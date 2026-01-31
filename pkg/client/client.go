// Package client provides a Go client for GibRAM with Protobuf protocol
package client

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gibram-io/gibram/pkg/codec"
	"github.com/gibram-io/gibram/pkg/types"
	pb "github.com/gibram-io/gibram/proto/gibrampb"
	"google.golang.org/protobuf/proto"
)

// =============================================================================
// Protocol Constants
// =============================================================================

const (
	ProtocolVersion = 2
	MaxFrameSize    = 64 * 1024 * 1024 // 64MB default, can be configured
)

// =============================================================================
// Connection Pool
// =============================================================================

const (
	DefaultPoolSize    = 20
	DefaultConnTimeout = 5 * time.Second
	DefaultIdleTimeout = 60 * time.Second
	DefaultMaxRetries  = 3
)

var (
	ErrPoolClosed    = errors.New("connection pool is closed")
	ErrPoolExhausted = errors.New("connection pool exhausted")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrRateLimited   = errors.New("rate limited")
	ErrNotFound      = errors.New("not found")
)

// PoolConfig configures the connection pool
type PoolConfig struct {
	MaxConnections int           // Max connections in pool (default: 20)
	ConnTimeout    time.Duration // Dial timeout (default: 5s)
	IdleTimeout    time.Duration // Idle connection timeout (default: 60s)
	MaxRetries     int           // Max retries on connection failure (default: 3)

	// TLS settings
	TLSEnabled    bool // Enable TLS
	TLSSkipVerify bool // Skip certificate verification (dev only)

	// Auth settings
	APIKey string // API key for authentication
}

// DefaultPoolConfig returns default pool configuration
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConnections: DefaultPoolSize,
		ConnTimeout:    DefaultConnTimeout,
		IdleTimeout:    DefaultIdleTimeout,
		MaxRetries:     DefaultMaxRetries,
	}
}

// pooledConn wraps a connection with metadata
type pooledConn struct {
	conn          net.Conn
	reader        *bufio.Reader
	lastUsed      atomic.Int64
	inUse         atomic.Bool
	authenticated bool
	requestID     atomic.Uint64
}

// ConnPool manages a pool of connections
type ConnPool struct {
	mu          sync.Mutex
	addr        string
	config      PoolConfig
	connections []*pooledConn
	available   chan *pooledConn
	closed      int32 // atomic
	activeCount int32 // atomic
}

// NewConnPool creates a new connection pool
func NewConnPool(addr string, config PoolConfig) (*ConnPool, error) {
	if config.MaxConnections <= 0 {
		config.MaxConnections = DefaultPoolSize
	}
	if config.ConnTimeout <= 0 {
		config.ConnTimeout = DefaultConnTimeout
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = DefaultIdleTimeout
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = DefaultMaxRetries
	}

	pool := &ConnPool{
		addr:        addr,
		config:      config,
		connections: make([]*pooledConn, 0, config.MaxConnections),
		available:   make(chan *pooledConn, config.MaxConnections),
	}

	// Pre-warm with one connection to verify connectivity
	conn, err := pool.createConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	pool.putConn(conn)

	// Start idle connection cleaner
	go pool.cleanIdleConnections()

	return pool, nil
}

// createConn creates a new connection
func (p *ConnPool) createConn() (*pooledConn, error) {
	var conn net.Conn
	var err error

	if p.config.TLSEnabled {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: p.config.TLSSkipVerify,
		}
		dialer := &net.Dialer{Timeout: p.config.ConnTimeout}
		conn, err = tls.DialWithDialer(dialer, "tcp", p.addr, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", p.addr, p.config.ConnTimeout)
	}

	if err != nil {
		return nil, err
	}

	pc := &pooledConn{
		conn:     conn,
		reader:   bufio.NewReader(conn),
	}
	pc.lastUsed.Store(time.Now().UnixNano())
	pc.inUse.Store(true)

	// Authenticate if API key is provided
	if p.config.APIKey != "" {
		if err := p.authenticateConn(pc); err != nil {
			conn.Close()
			return nil, fmt.Errorf("authentication failed: %w", err)
		}
		pc.authenticated = true
	}

	p.mu.Lock()
	p.connections = append(p.connections, pc)
	p.mu.Unlock()

	atomic.AddInt32(&p.activeCount, 1)
	return pc, nil
}

// authenticateConn authenticates a connection with API key using Protobuf
func (p *ConnPool) authenticateConn(pc *pooledConn) error {
	// Build auth request
	authReq := &pb.AuthRequest{ApiKey: p.config.APIKey}
	payload, _ := proto.Marshal(authReq)

	env := &pb.Envelope{
		Version:   ProtocolVersion,
		RequestId: pc.requestID.Add(1),
		CmdType:   pb.CommandType_CMD_AUTH,
		Payload:   payload,
	}

	// Send
	if err := writeEnvelope(pc.conn, env); err != nil {
		return err
	}

	// Read response
	respEnv, err := readEnvelope(pc.reader)
	if err != nil {
		return err
	}

	if respEnv.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(respEnv.Payload, &errResp)
		return fmt.Errorf("auth error: %s", errResp.Message)
	}

	if respEnv.CmdType != pb.CommandType_CMD_AUTH_RESPONSE {
		return fmt.Errorf("unexpected response type: %v", respEnv.CmdType)
	}

	var authResp pb.AuthResponse
	if err := proto.Unmarshal(respEnv.Payload, &authResp); err != nil {
		return err
	}

	if !authResp.Success {
		return fmt.Errorf("auth failed: %s", authResp.Message)
	}

	return nil
}

// getConn gets a connection from the pool
func (p *ConnPool) getConn() (*pooledConn, error) {
	if atomic.LoadInt32(&p.closed) == 1 {
		return nil, ErrPoolClosed
	}

	// Try to get from available pool (non-blocking)
	select {
	case pc := <-p.available:
		if pc != nil && time.Since(time.Unix(0, pc.lastUsed.Load())) < p.config.IdleTimeout {
			pc.inUse.Store(true)
			return pc, nil
		}
		if pc != nil {
			p.closeConn(pc)
		}
	default:
	}

	// Check if we can create new connection
	if atomic.LoadInt32(&p.activeCount) < int32(p.config.MaxConnections) {
		return p.createConn()
	}

	// Wait for available connection with timeout
	select {
	case pc := <-p.available:
		if pc != nil {
			pc.inUse.Store(true)
			return pc, nil
		}
	case <-time.After(p.config.ConnTimeout):
		return nil, ErrPoolExhausted
	}

	return nil, ErrPoolExhausted
}

// putConn returns a connection to the pool
func (p *ConnPool) putConn(pc *pooledConn) {
	if atomic.LoadInt32(&p.closed) == 1 {
		p.closeConn(pc)
		return
	}

	pc.inUse.Store(false)
	pc.lastUsed.Store(time.Now().UnixNano())

	select {
	case p.available <- pc:
	default:
		p.closeConn(pc)
	}
}

// closeConn closes a connection
func (p *ConnPool) closeConn(pc *pooledConn) {
	pc.conn.Close()
	atomic.AddInt32(&p.activeCount, -1)

	p.mu.Lock()
	for i, c := range p.connections {
		if c == pc {
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			break
		}
	}
	p.mu.Unlock()
}

// cleanIdleConnections periodically removes idle connections
func (p *ConnPool) cleanIdleConnections() {
	ticker := time.NewTicker(p.config.IdleTimeout / 2)
	defer ticker.Stop()

	for range ticker.C {
		if atomic.LoadInt32(&p.closed) == 1 {
			return
		}

		var toClose []*pooledConn
		var toReturn []*pooledConn

		for {
			select {
			case pc := <-p.available:
				if pc == nil {
					continue
				}
				if time.Since(time.Unix(0, pc.lastUsed.Load())) > p.config.IdleTimeout {
					toClose = append(toClose, pc)
				} else {
					toReturn = append(toReturn, pc)
				}
			default:
				goto done
			}
		}
	done:

		for _, pc := range toClose {
			p.closeConn(pc)
		}

		for _, pc := range toReturn {
			select {
			case p.available <- pc:
			default:
				p.closeConn(pc)
			}
		}
	}
}

// Close closes all connections in the pool
func (p *ConnPool) Close() {
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return
	}

	close(p.available)

	p.mu.Lock()
	for _, pc := range p.connections {
		pc.conn.Close()
	}
	p.connections = nil
	p.mu.Unlock()
}

// Stats returns pool statistics
func (p *ConnPool) Stats() (active, available int) {
	return int(atomic.LoadInt32(&p.activeCount)), len(p.available)
}

// =============================================================================
// Wire Protocol Helpers
// =============================================================================

func writeEnvelope(w io.Writer, env *pb.Envelope) error {
	data, err := proto.Marshal(env)
	if err != nil {
		return err
	}

	// Frame: [1 byte codec][4 bytes length][payload]
	frame := make([]byte, 1+4+len(data))
	frame[0] = byte(codec.CodecProtobuf)
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(data)))
	copy(frame[5:], data)

	_, err = w.Write(frame)
	return err
}

func readEnvelope(r *bufio.Reader) (*pb.Envelope, error) {
	// Read codec type (1 byte)
	codecByte, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	if codec.CodecType(codecByte) != codec.CodecProtobuf {
		return nil, fmt.Errorf("unsupported codec: %d", codecByte)
	}

	// Read length (4 bytes, big endian)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	if length > MaxFrameSize {
		return nil, fmt.Errorf("frame too large: %d", length)
	}

	// Read payload
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	// Decode envelope
	var env pb.Envelope
	if err := proto.Unmarshal(payload, &env); err != nil {
		return nil, err
	}

	return &env, nil
}

// =============================================================================
// Client
// =============================================================================

type Client struct {
	pool      *ConnPool
	sessionID string // Required session ID for all operations
}

// NewClient creates a new client with default pool config
// sessionID is required for all operations (like database selection)
func NewClient(addr, sessionID string) (*Client, error) {
	return NewClientWithConfig(addr, sessionID, DefaultPoolConfig())
}

// NewClientWithConfig creates a new client with custom pool config
// sessionID is required for all operations (like database selection)
func NewClientWithConfig(addr, sessionID string, config PoolConfig) (*Client, error) {
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	pool, err := NewConnPool(addr, config)
	if err != nil {
		return nil, err
	}

	return &Client{pool: pool, sessionID: sessionID}, nil
}

func (c *Client) Close() error {
	c.pool.Close()
	return nil
}

// PoolStats returns connection pool statistics
func (c *Client) PoolStats() (active, available int) {
	return c.pool.Stats()
}

// send sends a command and returns the response
func (c *Client) send(cmdType pb.CommandType, payload proto.Message) (*pb.Envelope, error) {
	var lastErr error

	for retry := 0; retry < c.pool.config.MaxRetries; retry++ {
		pc, err := c.pool.getConn()
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := c.doSend(pc, cmdType, payload)
		if err != nil {
			c.pool.closeConn(pc)
			lastErr = err
			continue
		}

		c.pool.putConn(pc)
		return resp, nil
	}

	return nil, fmt.Errorf("after %d retries: %w", c.pool.config.MaxRetries, lastErr)
}

func (c *Client) doSend(pc *pooledConn, cmdType pb.CommandType, payload proto.Message) (*pb.Envelope, error) {
	var payloadBytes []byte
	if payload != nil {
		var err error
		payloadBytes, err = proto.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	env := &pb.Envelope{
		Version:   ProtocolVersion,
		RequestId: pc.requestID.Add(1),
		CmdType:   cmdType,
		Payload:   payloadBytes,
		SessionId: c.sessionID,
	}

	// Set write deadline
	pc.conn.SetWriteDeadline(time.Now().Add(c.pool.config.ConnTimeout))

	if err := writeEnvelope(pc.conn, env); err != nil {
		return nil, err
	}

	// Set read deadline
	pc.conn.SetReadDeadline(time.Now().Add(c.pool.config.ConnTimeout * 2))

	resp, err := readEnvelope(pc.reader)
	if err != nil {
		return nil, err
	}

	// Clear deadlines
	pc.conn.SetDeadline(time.Time{})

	// Check for error response
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		return nil, fmt.Errorf("server error: %s", errResp.Message)
	}

	return resp, nil
}

// =============================================================================
// Basic Commands
// =============================================================================

func (c *Client) Ping() error {
	resp, err := c.send(pb.CommandType_CMD_PING, nil)
	if err != nil {
		return err
	}
	if resp.CmdType != pb.CommandType_CMD_PONG {
		return fmt.Errorf("unexpected response: %v", resp.CmdType)
	}
	return nil
}

// =============================================================================
// Session Management Commands
// =============================================================================

// ListSessions returns all active sessions on the server
func (c *Client) ListSessions() ([]types.SessionInfo, error) {
	resp, err := c.send(pb.CommandType_CMD_LIST_SESSIONS, nil)
	if err != nil {
		return nil, err
	}

	var listResp pb.ListSessionsResponse
	if err := proto.Unmarshal(resp.Payload, &listResp); err != nil {
		return nil, err
	}

	sessions := make([]types.SessionInfo, len(listResp.Sessions))
	for i, s := range listResp.Sessions {
		sessions[i] = types.SessionInfo{
			ID:                s.SessionId,
			CreatedAt:         s.CreatedAt,
			LastAccess:        s.LastAccess,
			TTL:               s.Ttl,
			IdleTTL:           s.IdleTtl,
			DocumentCount:     int(s.DocumentCount),
			TextUnitCount:     int(s.TextunitCount),
			EntityCount:       int(s.EntityCount),
			RelationshipCount: int(s.RelationshipCount),
			CommunityCount:    int(s.CommunityCount),
		}
	}

	return sessions, nil
}

// DeleteSession deletes a specific session (requires admin permission)
func (c *Client) DeleteSession(sessionID string) error {
	// Override client's sessionID temporarily for this admin operation
	oldSessionID := c.sessionID
	c.sessionID = sessionID
	defer func() { c.sessionID = oldSessionID }()

	_, err := c.send(pb.CommandType_CMD_DELETE_SESSION, nil)
	return err
}

// SetSessionTTL sets TTL for current session
func (c *Client) SetSessionTTL(ttl, idleTTL int64) error {
	req := &pb.SetSessionTTLRequest{
		Ttl:     ttl,
		IdleTtl: idleTTL,
	}
	_, err := c.send(pb.CommandType_CMD_SET_SESSION_TTL, req)
	return err
}

// TouchSession updates last access time for current session
func (c *Client) TouchSession() error {
	_, err := c.send(pb.CommandType_CMD_TOUCH_SESSION, nil)
	return err
}

// =============================================================================
// Info & Health Commands
// =============================================================================

func (c *Client) Info() (*types.ServerInfo, error) {
	resp, err := c.send(pb.CommandType_CMD_INFO, nil)
	if err != nil {
		return nil, err
	}

	var infoResp pb.InfoResponse
	if err := proto.Unmarshal(resp.Payload, &infoResp); err != nil {
		return nil, err
	}

	return &types.ServerInfo{
		Version:           infoResp.Version,
		DocumentCount:     int(infoResp.DocumentCount),
		TextUnitCount:     int(infoResp.TextunitCount),
		EntityCount:       int(infoResp.EntityCount),
		RelationshipCount: int(infoResp.RelationshipCount),
		CommunityCount:    int(infoResp.CommunityCount),
		VectorDim:         int(infoResp.VectorDim),
	}, nil
}

// HealthStatus represents server health information
type HealthStatus struct {
	Status     string            `json:"status"`
	Components map[string]string `json:"components"`
}

func (c *Client) Health() (*HealthStatus, error) {
	resp, err := c.send(pb.CommandType_CMD_HEALTH, nil)
	if err != nil {
		return nil, err
	}

	var healthResp pb.HealthResponse
	if err := proto.Unmarshal(resp.Payload, &healthResp); err != nil {
		return nil, err
	}

	return &HealthStatus{
		Status:     healthResp.Status,
		Components: healthResp.Components,
	}, nil
}

// =============================================================================
// Document Commands
// =============================================================================

func (c *Client) AddDocument(extID, filename string) (uint64, error) {
	req := &pb.AddDocumentRequest{
		ExternalId: extID,
		Filename:   filename,
	}

	resp, err := c.send(pb.CommandType_CMD_ADD_DOCUMENT, req)
	if err != nil {
		return 0, err
	}

	var okResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &okResp); err != nil {
		return 0, err
	}

	return okResp.Id, nil
}

func (c *Client) GetDocument(id uint64) (*types.Document, error) {
	req := &pb.GetByIDRequest{Id: id}

	resp, err := c.send(pb.CommandType_CMD_GET_DOCUMENT, req)
	if err != nil {
		return nil, err
	}

	var docResp pb.Document
	if err := proto.Unmarshal(resp.Payload, &docResp); err != nil {
		return nil, err
	}

	return codec.ProtoToDocument(&docResp), nil
}

func (c *Client) DeleteDocument(id uint64) error {
	req := &pb.DeleteByIDRequest{Id: id}
	_, err := c.send(pb.CommandType_CMD_DELETE_DOCUMENT, req)
	return err
}

// =============================================================================
// TextUnit Commands
// =============================================================================

func (c *Client) AddTextUnit(extID string, docID uint64, content string, embedding []float32, tokenCount int) (uint64, error) {
	req := &pb.AddTextUnitRequest{
		ExternalId: extID,
		DocumentId: docID,
		Content:    content,
		Embedding:  embedding,
		TokenCount: int32(tokenCount),
	}

	resp, err := c.send(pb.CommandType_CMD_ADD_TEXTUNIT, req)
	if err != nil {
		return 0, err
	}

	var okResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &okResp); err != nil {
		return 0, err
	}

	return okResp.Id, nil
}

func (c *Client) GetTextUnit(id uint64) (*types.TextUnit, error) {
	req := &pb.GetByIDRequest{Id: id}

	resp, err := c.send(pb.CommandType_CMD_GET_TEXTUNIT, req)
	if err != nil {
		return nil, err
	}

	var tuResp pb.TextUnit
	if err := proto.Unmarshal(resp.Payload, &tuResp); err != nil {
		return nil, err
	}

	return codec.ProtoToTextUnit(&tuResp), nil
}

func (c *Client) DeleteTextUnit(id uint64) error {
	req := &pb.DeleteByIDRequest{Id: id}
	_, err := c.send(pb.CommandType_CMD_DELETE_TEXTUNIT, req)
	return err
}

func (c *Client) LinkTextUnitToEntity(tuID, entityID uint64) error {
	req := &pb.LinkTextUnitEntityRequest{
		TextunitId: tuID,
		EntityId:   entityID,
	}
	_, err := c.send(pb.CommandType_CMD_LINK_TEXTUNIT_ENTITY, req)
	return err
}

// =============================================================================
// Entity Commands
// =============================================================================

func (c *Client) AddEntity(extID, title, entType, description string, embedding []float32) (uint64, error) {
	req := &pb.AddEntityRequest{
		ExternalId:  extID,
		Title:       title,
		Type:        entType,
		Description: description,
		Embedding:   embedding,
	}

	resp, err := c.send(pb.CommandType_CMD_ADD_ENTITY, req)
	if err != nil {
		return 0, err
	}

	var okResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &okResp); err != nil {
		return 0, err
	}

	return okResp.Id, nil
}

func (c *Client) GetEntity(id uint64) (*types.Entity, error) {
	req := &pb.GetByIDRequest{Id: id}

	resp, err := c.send(pb.CommandType_CMD_GET_ENTITY, req)
	if err != nil {
		return nil, err
	}

	var entResp pb.Entity
	if err := proto.Unmarshal(resp.Payload, &entResp); err != nil {
		return nil, err
	}

	return codec.ProtoToEntity(&entResp), nil
}

func (c *Client) GetEntityByTitle(title string) (*types.Entity, error) {
	req := &pb.GetEntityByTitleRequest{Title: title}

	resp, err := c.send(pb.CommandType_CMD_GET_ENTITY_BY_TITLE, req)
	if err != nil {
		return nil, err
	}

	var entResp pb.Entity
	if err := proto.Unmarshal(resp.Payload, &entResp); err != nil {
		return nil, err
	}

	return codec.ProtoToEntity(&entResp), nil
}

func (c *Client) UpdateEntityDescription(id uint64, description string, embedding []float32) error {
	req := &pb.UpdateEntityDescRequest{
		Id:          id,
		Description: description,
		Embedding:   embedding,
	}
	_, err := c.send(pb.CommandType_CMD_UPDATE_ENTITY_DESC, req)
	return err
}

func (c *Client) DeleteEntity(id uint64) error {
	req := &pb.DeleteByIDRequest{Id: id}
	_, err := c.send(pb.CommandType_CMD_DELETE_ENTITY, req)
	return err
}

// =============================================================================
// Relationship Commands
// =============================================================================

func (c *Client) AddRelationship(extID string, sourceID, targetID uint64, relType, description string, weight float32) (uint64, error) {
	req := &pb.AddRelationshipRequest{
		ExternalId:  extID,
		SourceId:    sourceID,
		TargetId:    targetID,
		Type:        relType,
		Description: description,
		Weight:      weight,
	}

	resp, err := c.send(pb.CommandType_CMD_ADD_RELATIONSHIP, req)
	if err != nil {
		return 0, err
	}

	var okResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &okResp); err != nil {
		return 0, err
	}

	return okResp.Id, nil
}

func (c *Client) GetRelationship(id uint64) (*types.Relationship, error) {
	req := &pb.GetByIDRequest{Id: id}

	resp, err := c.send(pb.CommandType_CMD_GET_RELATIONSHIP, req)
	if err != nil {
		return nil, err
	}

	var relResp pb.Relationship
	if err := proto.Unmarshal(resp.Payload, &relResp); err != nil {
		return nil, err
	}

	return codec.ProtoToRelationship(&relResp), nil
}

func (c *Client) DeleteRelationship(id uint64) error {
	req := &pb.DeleteByIDRequest{Id: id}
	_, err := c.send(pb.CommandType_CMD_DELETE_RELATIONSHIP, req)
	return err
}

// =============================================================================
// Community Commands
// =============================================================================

func (c *Client) AddCommunity(extID, title, summary, fullContent string, level int, entityIDs, relIDs []uint64, embedding []float32) (uint64, error) {
	req := &pb.AddCommunityRequest{
		ExternalId:      extID,
		Title:           title,
		Summary:         summary,
		FullContent:     fullContent,
		Level:           int32(level),
		EntityIds:       entityIDs,
		RelationshipIds: relIDs,
		Embedding:       embedding,
	}

	resp, err := c.send(pb.CommandType_CMD_ADD_COMMUNITY, req)
	if err != nil {
		return 0, err
	}

	var okResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &okResp); err != nil {
		return 0, err
	}

	return okResp.Id, nil
}

func (c *Client) GetCommunity(id uint64) (*types.Community, error) {
	req := &pb.GetByIDRequest{Id: id}

	resp, err := c.send(pb.CommandType_CMD_GET_COMMUNITY, req)
	if err != nil {
		return nil, err
	}

	var commResp pb.Community
	if err := proto.Unmarshal(resp.Payload, &commResp); err != nil {
		return nil, err
	}

	return codec.ProtoToCommunity(&commResp), nil
}

func (c *Client) DeleteCommunity(id uint64) error {
	req := &pb.DeleteByIDRequest{Id: id}
	_, err := c.send(pb.CommandType_CMD_DELETE_COMMUNITY, req)
	return err
}

type ComputeCommunitiesResult struct {
	Count       int
	Communities []*types.Community
}

func (c *Client) ComputeCommunities(resolution float64, iterations int) (*ComputeCommunitiesResult, error) {
	req := &pb.ComputeCommunitiesRequest{
		Resolution: resolution,
		Iterations: int32(iterations),
	}

	resp, err := c.send(pb.CommandType_CMD_COMPUTE_COMMUNITIES, req)
	if err != nil {
		return nil, err
	}

	var commResp pb.ComputeCommunitiesResponse
	if err := proto.Unmarshal(resp.Payload, &commResp); err != nil {
		return nil, err
	}

	result := &ComputeCommunitiesResult{
		Count:       int(commResp.Count),
		Communities: make([]*types.Community, len(commResp.Communities)),
	}
	for i, c := range commResp.Communities {
		result.Communities[i] = codec.ProtoToCommunity(c)
	}

	return result, nil
}

type HierarchicalLeidenResult struct {
	TotalCommunities int
	LevelCounts      map[int]int
}

func (c *Client) HierarchicalLeiden(maxLevels int, resolution float64) (*HierarchicalLeidenResult, error) {
	if maxLevels > 5 {
		maxLevels = 5
	}

	req := &pb.HierarchicalLeidenRequest{
		MaxLevels:  int32(maxLevels),
		Resolution: resolution,
	}

	resp, err := c.send(pb.CommandType_CMD_HIERARCHICAL_LEIDEN, req)
	if err != nil {
		return nil, err
	}

	var hlResp pb.HierarchicalLeidenResponse
	if err := proto.Unmarshal(resp.Payload, &hlResp); err != nil {
		return nil, err
	}

	result := &HierarchicalLeidenResult{
		TotalCommunities: int(hlResp.TotalCommunities),
		LevelCounts:      make(map[int]int),
	}
	for k, v := range hlResp.LevelCounts {
		result.LevelCounts[int(k)] = int(v)
	}

	return result, nil
}

func (c *Client) HierarchicalLeidenDefault() (*HierarchicalLeidenResult, error) {
	return c.HierarchicalLeiden(5, 1.0)
}

// =============================================================================
// Query Commands
// =============================================================================

func (c *Client) Query(spec types.QuerySpec) (*types.ContextPack, error) {
	// Convert search types to strings (proto uses repeated string)
	var searchTypes []string
	for _, st := range spec.SearchTypes {
		searchTypes = append(searchTypes, string(st))
	}

	req := &pb.QueryRequest{
		QueryVector:    spec.QueryVector,
		TopK:           int32(spec.TopK),
		KHops:          int32(spec.KHops),
		MaxEntities:    int32(spec.MaxEntities),
		MaxTextunits:   int32(spec.MaxTextUnits),
		MaxCommunities: int32(spec.MaxCommunities),
		SearchTypes:    searchTypes,
	}

	resp, err := c.send(pb.CommandType_CMD_QUERY, req)
	if err != nil {
		return nil, err
	}

	var queryResp pb.QueryResponse
	if err := proto.Unmarshal(resp.Payload, &queryResp); err != nil {
		return nil, err
	}

	result := &types.ContextPack{
		QueryID: queryResp.QueryId,
		Stats: types.QueryStats{
			DurationMicros: queryResp.Stats.DurationMicros,
		},
	}

	for _, tu := range queryResp.Textunits {
		result.TextUnits = append(result.TextUnits, types.TextUnitResult{
			TextUnit:   codec.ProtoToTextUnit(tu.Textunit),
			Similarity: tu.Similarity,
			Hop:        int(tu.Hop),
		})
	}

	for _, ent := range queryResp.Entities {
		result.Entities = append(result.Entities, types.EntityResult{
			Entity:     codec.ProtoToEntity(ent.Entity),
			Similarity: ent.Similarity,
			Hop:        int(ent.Hop),
		})
	}

	for _, comm := range queryResp.Communities {
		result.Communities = append(result.Communities, types.CommunityResult{
			Community:  codec.ProtoToCommunity(comm.Community),
			Similarity: comm.Similarity,
		})
	}

	for _, rel := range queryResp.Relationships {
		result.Relationships = append(result.Relationships, types.RelationshipResult{
			Relationship: codec.ProtoToRelationship(rel.Relationship),
			SourceTitle:  rel.SourceTitle,
			TargetTitle:  rel.TargetTitle,
		})
	}

	return result, nil
}

func (c *Client) Explain(queryID uint64) (*types.ExplainPack, error) {
	req := &pb.ExplainRequest{QueryId: queryID}

	resp, err := c.send(pb.CommandType_CMD_EXPLAIN, req)
	if err != nil {
		return nil, err
	}

	var explainResp pb.ExplainResponse
	if err := proto.Unmarshal(resp.Payload, &explainResp); err != nil {
		return nil, err
	}

	result := &types.ExplainPack{
		QueryID: explainResp.QueryId,
	}

	for _, seed := range explainResp.Seeds {
		result.Seeds = append(result.Seeds, types.SeedInfo{
			Type:       types.SearchType(seed.Type),
			ID:         seed.Id,
			ExternalID: seed.ExternalId,
			Similarity: seed.Similarity,
		})
	}

	for _, step := range explainResp.Traversal {
		result.Traversal = append(result.Traversal, types.TraversalStep{
			FromEntityID:   step.FromEntityId,
			ToEntityID:     step.ToEntityId,
			RelationshipID: step.RelationshipId,
			RelType:        step.RelType,
			Weight:         step.Weight,
			Hop:            int(step.Hop),
		})
	}

	return result, nil
}

// =============================================================================
// TTL Commands
// =============================================================================
// DEPRECATED v0.1.0: TTL management moved to session-level

/*
func (c *Client) SetTTL(itemType types.ItemType, id uint64, ttl int64) error {
	req := &pb.SetTTLRequest{
		ItemType:   string(itemType),
		Id:         id,
		TtlSeconds: ttl,
	}
	_, err := c.send(pb.CommandType_CMD_SET_TTL, req)
	return err
}

func (c *Client) SetIdleTTL(itemType types.ItemType, id uint64, idleTTL int64) error {
	req := &pb.SetIdleTTLRequest{
		ItemType:       string(itemType),
		Id:             id,
		IdleTtlSeconds: idleTTL,
	}
	_, err := c.send(pb.CommandType_CMD_SET_IDLE_TTL, req)
	return err
}

func (c *Client) GetTTL(itemType types.ItemType, id uint64) (int64, error) {
	req := &pb.GetTTLRequest{
		ItemType: string(itemType),
		Id:       id,
	}

	resp, err := c.send(pb.CommandType_CMD_GET_TTL, req)
	if err != nil {
		return 0, err
	}

	var ttlResp pb.TTLResponse
	if err := proto.Unmarshal(resp.Payload, &ttlResp); err != nil {
		return 0, err
	}

	return ttlResp.TtlRemaining, nil
}
*/

// =============================================================================
// Bulk Commands
// =============================================================================

func (c *Client) MSetEntities(entities []types.BulkEntityInput) ([]uint64, error) {
	var pbEntities []*pb.AddEntityRequest
	for _, e := range entities {
		pbEntities = append(pbEntities, &pb.AddEntityRequest{
			ExternalId:  e.ExternalID,
			Title:       e.Title,
			Type:        e.Type,
			Description: e.Description,
			Embedding:   e.Embedding,
		})
	}

	req := &pb.MSetEntitiesRequest{Entities: pbEntities}
	resp, err := c.send(pb.CommandType_CMD_MSET_ENTITIES, req)
	if err != nil {
		return nil, err
	}

	var result pb.EntitiesResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	return result.CreatedIds, nil
}

func (c *Client) MGetEntities(ids []uint64) ([]*types.Entity, error) {
	req := &pb.MGetEntitiesRequest{Ids: ids}
	resp, err := c.send(pb.CommandType_CMD_MGET_ENTITIES, req)
	if err != nil {
		return nil, err
	}

	var result pb.EntitiesResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	var entities []*types.Entity
	for _, e := range result.Entities {
		entities = append(entities, codec.ProtoToEntity(e))
	}

	return entities, nil
}

// ListEntities returns entities after the given cursor, up to limit, in ID order.
func (c *Client) ListEntities(cursor uint64, limit int) ([]*types.Entity, uint64, error) {
	req := &pb.ListEntitiesRequest{
		Cursor: cursor,
		Limit:  int32(limit),
	}
	resp, err := c.send(pb.CommandType_CMD_LIST_ENTITIES, req)
	if err != nil {
		return nil, 0, err
	}

	var result pb.EntitiesResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, 0, err
	}

	var entities []*types.Entity
	for _, e := range result.Entities {
		entities = append(entities, codec.ProtoToEntity(e))
	}

	return entities, result.NextCursor, nil
}

func (c *Client) MSetDocuments(docs []types.BulkDocumentInput) ([]uint64, error) {
	var pbDocs []*pb.AddDocumentRequest
	for _, d := range docs {
		pbDocs = append(pbDocs, &pb.AddDocumentRequest{
			ExternalId: d.ExternalID,
			Filename:   d.Filename,
		})
	}

	req := &pb.MSetDocumentsRequest{Documents: pbDocs}
	resp, err := c.send(pb.CommandType_CMD_MSET_DOCUMENTS, req)
	if err != nil {
		return nil, err
	}

	var result pb.DocumentsResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	return result.CreatedIds, nil
}

func (c *Client) MGetDocuments(ids []uint64) ([]*types.Document, error) {
	req := &pb.MGetDocumentsRequest{Ids: ids}
	resp, err := c.send(pb.CommandType_CMD_MGET_DOCUMENTS, req)
	if err != nil {
		return nil, err
	}

	var result pb.DocumentsResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	var docs []*types.Document
	for _, d := range result.Documents {
		docs = append(docs, codec.ProtoToDocument(d))
	}

	return docs, nil
}

func (c *Client) MSetTextUnits(tus []types.BulkTextUnitInput) ([]uint64, error) {
	var pbTUs []*pb.AddTextUnitRequest
	for _, t := range tus {
		pbTUs = append(pbTUs, &pb.AddTextUnitRequest{
			ExternalId: t.ExternalID,
			DocumentId: t.DocumentID,
			Content:    t.Content,
			Embedding:  t.Embedding,
			TokenCount: int32(t.TokenCount),
		})
	}

	req := &pb.MSetTextUnitsRequest{Textunits: pbTUs}
	resp, err := c.send(pb.CommandType_CMD_MSET_TEXTUNITS, req)
	if err != nil {
		return nil, err
	}

	var result pb.TextUnitsResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	return result.CreatedIds, nil
}

func (c *Client) MGetTextUnits(ids []uint64) ([]*types.TextUnit, error) {
	req := &pb.MGetTextUnitsRequest{Ids: ids}
	resp, err := c.send(pb.CommandType_CMD_MGET_TEXTUNITS, req)
	if err != nil {
		return nil, err
	}

	var result pb.TextUnitsResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	var tus []*types.TextUnit
	for _, t := range result.Textunits {
		tus = append(tus, codec.ProtoToTextUnit(t))
	}

	return tus, nil
}

func (c *Client) MSetRelationships(rels []types.BulkRelationshipInput) ([]uint64, error) {
	var pbRels []*pb.AddRelationshipRequest
	for _, r := range rels {
		pbRels = append(pbRels, &pb.AddRelationshipRequest{
			ExternalId:  r.ExternalID,
			SourceId:    r.SourceID,
			TargetId:    r.TargetID,
			Type:        r.Type,
			Description: r.Description,
			Weight:      r.Weight,
		})
	}

	req := &pb.MSetRelationshipsRequest{Relationships: pbRels}
	resp, err := c.send(pb.CommandType_CMD_MSET_RELATIONSHIPS, req)
	if err != nil {
		return nil, err
	}

	var result pb.RelationshipsResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	return result.CreatedIds, nil
}

func (c *Client) MGetRelationships(ids []uint64) ([]*types.Relationship, error) {
	req := &pb.MGetRelationshipsRequest{Ids: ids}
	resp, err := c.send(pb.CommandType_CMD_MGET_RELATIONSHIPS, req)
	if err != nil {
		return nil, err
	}

	var result pb.RelationshipsResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, err
	}

	var rels []*types.Relationship
	for _, r := range result.Relationships {
		rels = append(rels, codec.ProtoToRelationship(r))
	}

	return rels, nil
}

// ListRelationships returns relationships after the given cursor, up to limit, in ID order.
func (c *Client) ListRelationships(cursor uint64, limit int) ([]*types.Relationship, uint64, error) {
	req := &pb.ListRelationshipsRequest{
		Cursor: cursor,
		Limit:  int32(limit),
	}
	resp, err := c.send(pb.CommandType_CMD_LIST_RELATIONSHIPS, req)
	if err != nil {
		return nil, 0, err
	}

	var result pb.RelationshipsResponse
	if err := proto.Unmarshal(resp.Payload, &result); err != nil {
		return nil, 0, err
	}

	var rels []*types.Relationship
	for _, r := range result.Relationships {
		rels = append(rels, codec.ProtoToRelationship(r))
	}

	return rels, result.NextCursor, nil
}

// =============================================================================
// Backup Commands
// =============================================================================

func (c *Client) BGSave(path string) error {
	req := &pb.SaveRequest{Path: path}
	_, err := c.send(pb.CommandType_CMD_BGSAVE, req)
	return err
}

func (c *Client) Save(path string) error {
	req := &pb.SaveRequest{Path: path}
	_, err := c.send(pb.CommandType_CMD_SAVE, req)
	return err
}

type LastSaveInfo struct {
	Timestamp int64
	Path      string
}

func (c *Client) LastSave() (*LastSaveInfo, error) {
	resp, err := c.send(pb.CommandType_CMD_LASTSAVE, nil)
	if err != nil {
		return nil, err
	}

	var lsResp pb.LastSaveResponse
	if err := proto.Unmarshal(resp.Payload, &lsResp); err != nil {
		return nil, err
	}

	return &LastSaveInfo{
		Timestamp: lsResp.Timestamp,
		Path:      lsResp.Path,
	}, nil
}

func (c *Client) BGRestore(path string) error {
	req := &pb.RestoreRequest{Path: path}
	_, err := c.send(pb.CommandType_CMD_BGRESTORE, req)
	return err
}

type BackupStatus struct {
	InProgress   bool
	Type         string
	StartTime    int64
	LastSaveTime int64
	LastSavePath string
}

func (c *Client) BackupStatus() (*BackupStatus, error) {
	resp, err := c.send(pb.CommandType_CMD_BACKUP_STATUS, nil)
	if err != nil {
		return nil, err
	}

	var bsResp pb.BackupStatusResponse
	if err := proto.Unmarshal(resp.Payload, &bsResp); err != nil {
		return nil, err
	}

	return &BackupStatus{
		InProgress:   bsResp.InProgress,
		Type:         bsResp.Type,
		StartTime:    bsResp.StartTime,
		LastSaveTime: bsResp.LastSaveTime,
		LastSavePath: bsResp.LastSavePath,
	}, nil
}
