// Package server provides the TCP server for GibRAM with Protobuf protocol
package server

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gibram-io/gibram/pkg/backup"
	"github.com/gibram-io/gibram/pkg/codec"
	"github.com/gibram-io/gibram/pkg/config"
	"github.com/gibram-io/gibram/pkg/engine"
	"github.com/gibram-io/gibram/pkg/graph"
	"github.com/gibram-io/gibram/pkg/logging"
	"github.com/gibram-io/gibram/pkg/types"
	pb "github.com/gibram-io/gibram/proto/gibrampb"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"
)

// =============================================================================
// RBAC Permission Mapping
// =============================================================================

// commandPermissions maps command types to required permissions
var commandPermissions = map[pb.CommandType]string{
	// Read operations
	pb.CommandType_CMD_PING:                config.PermRead,
	pb.CommandType_CMD_INFO:                config.PermRead,
	pb.CommandType_CMD_HEALTH:              config.PermRead,
	pb.CommandType_CMD_GET_DOCUMENT:        config.PermRead,
	pb.CommandType_CMD_GET_TEXTUNIT:        config.PermRead,
	pb.CommandType_CMD_GET_ENTITY:          config.PermRead,
	pb.CommandType_CMD_GET_ENTITY_BY_TITLE: config.PermRead,
	pb.CommandType_CMD_GET_RELATIONSHIP:    config.PermRead,
	pb.CommandType_CMD_GET_COMMUNITY:       config.PermRead,
	pb.CommandType_CMD_QUERY:               config.PermRead,
	pb.CommandType_CMD_EXPLAIN:             config.PermRead,
	pb.CommandType_CMD_MGET_ENTITIES:       config.PermRead,
	pb.CommandType_CMD_MGET_DOCUMENTS:      config.PermRead,
	pb.CommandType_CMD_MGET_TEXTUNITS:      config.PermRead,
	pb.CommandType_CMD_MGET_RELATIONSHIPS:  config.PermRead,
	pb.CommandType_CMD_LASTSAVE:            config.PermRead,
	pb.CommandType_CMD_BACKUP_STATUS:       config.PermRead,
	pb.CommandType_CMD_WAL_STATUS:          config.PermRead,
	pb.CommandType_CMD_LIST_SESSIONS:       config.PermRead,
	pb.CommandType_CMD_SESSION_INFO:        config.PermRead,

	// Write operations
	pb.CommandType_CMD_ADD_DOCUMENT:         config.PermWrite,
	pb.CommandType_CMD_DELETE_DOCUMENT:      config.PermWrite,
	pb.CommandType_CMD_ADD_TEXTUNIT:         config.PermWrite,
	pb.CommandType_CMD_DELETE_TEXTUNIT:      config.PermWrite,
	pb.CommandType_CMD_LINK_TEXTUNIT_ENTITY: config.PermWrite,
	pb.CommandType_CMD_ADD_ENTITY:           config.PermWrite,
	pb.CommandType_CMD_UPDATE_ENTITY_DESC:   config.PermWrite,
	pb.CommandType_CMD_DELETE_ENTITY:        config.PermWrite,
	pb.CommandType_CMD_ADD_RELATIONSHIP:     config.PermWrite,
	pb.CommandType_CMD_DELETE_RELATIONSHIP:  config.PermWrite,
	pb.CommandType_CMD_ADD_COMMUNITY:        config.PermWrite,
	pb.CommandType_CMD_DELETE_COMMUNITY:     config.PermWrite,
	pb.CommandType_CMD_COMPUTE_COMMUNITIES:  config.PermWrite,
	pb.CommandType_CMD_HIERARCHICAL_LEIDEN:  config.PermWrite,
	pb.CommandType_CMD_SET_SESSION_TTL:      config.PermWrite,
	pb.CommandType_CMD_TOUCH_SESSION:        config.PermWrite,
	pb.CommandType_CMD_MSET_ENTITIES:        config.PermWrite,
	pb.CommandType_CMD_MSET_DOCUMENTS:       config.PermWrite,
	pb.CommandType_CMD_MSET_TEXTUNITS:       config.PermWrite,
	pb.CommandType_CMD_MSET_RELATIONSHIPS:   config.PermWrite,
	pb.CommandType_CMD_PIPELINE:             config.PermWrite,

	// Admin operations
	pb.CommandType_CMD_SAVE:           config.PermAdmin,
	pb.CommandType_CMD_BGSAVE:         config.PermAdmin,
	pb.CommandType_CMD_BGRESTORE:      config.PermAdmin,
	pb.CommandType_CMD_REBUILD_INDEX:  config.PermAdmin,
	pb.CommandType_CMD_WAL_CHECKPOINT: config.PermAdmin,
	pb.CommandType_CMD_WAL_TRUNCATE:   config.PermAdmin,
	pb.CommandType_CMD_WAL_ROTATE:     config.PermAdmin,
	pb.CommandType_CMD_DELETE_SESSION: config.PermAdmin,
}

// =============================================================================
// Protocol Constants
// =============================================================================

const (
	ProtocolVersion      = 1
	DefaultMaxFrameSize  = 64 * 1024 * 1024 // 64MB
	DefaultVectorDim     = 1536
	DefaultIdleTimeout   = 300 * time.Second
	DefaultUnauthTimeout = 10 * time.Second
	DefaultRateLimit     = 1000
	DefaultRateBurst     = 100
)

// =============================================================================
// Server - Protobuf TCP Server
// =============================================================================

// Server handles Protobuf protocol connections
type Server struct {
	engine    *engine.Engine
	config    *config.Config
	listener  net.Listener
	wg        sync.WaitGroup
	stopCh    chan struct{}
	startTime time.Time
	requestID atomic.Uint64

	// Security
	apiKeyStore  *config.APIKeyStore
	rateLimiters sync.Map // map[keyID]*rate.Limiter

	// Backup state
	backupInProgress atomic.Bool
	backupType       string
	backupStartTime  int64
	lastSaveTime     int64
	lastSavePath     string

	// Snapshot callback (accepts path)
	snapshotFn func(path string) error
	restoreFn  func(path string) error

	// WAL reference for WAL commands
	wal *backup.WAL

	// Connection config (derived from config.Config)
	maxFrameSize  uint32
	idleTimeout   time.Duration
	unauthTimeout time.Duration
	rateLimit     int
	rateBurst     int
}

// NewServer creates a new Protobuf server
func NewServer(eng *engine.Engine) *Server {
	return NewServerWithConfig(eng, nil)
}

// NewServerWithConfig creates a new Protobuf server with config
func NewServerWithConfig(eng *engine.Engine, cfg *config.Config) *Server {
	s := &Server{
		engine:        eng,
		config:        cfg,
		stopCh:        make(chan struct{}),
		startTime:     time.Now(),
		maxFrameSize:  DefaultMaxFrameSize,
		idleTimeout:   DefaultIdleTimeout,
		unauthTimeout: DefaultUnauthTimeout,
		rateLimit:     DefaultRateLimit,
		rateBurst:     DefaultRateBurst,
	}

	// Apply config if provided
	if cfg != nil {
		if cfg.Security.MaxFrameSize > 0 {
			s.maxFrameSize = uint32(cfg.Security.MaxFrameSize)
		}
		if cfg.Security.IdleTimeout > 0 {
			s.idleTimeout = cfg.Security.IdleTimeout
		}
		if cfg.Security.UnauthTimeout > 0 {
			s.unauthTimeout = cfg.Security.UnauthTimeout
		}
		if cfg.Security.RateLimit > 0 {
			s.rateLimit = cfg.Security.RateLimit
		}
		if cfg.Security.RateBurst > 0 {
			s.rateBurst = cfg.Security.RateBurst
		}

		// Setup API key store
		if cfg.HasAuth() {
			store, err := config.NewAPIKeyStore(&cfg.Auth)
			if err != nil {
				logging.Warn(" failed to create API key store: %v", err)
			} else {
				s.apiKeyStore = store
			}
		}
	}

	return s
}

// SetSnapshotCallback sets the snapshot function with path
func (s *Server) SetSnapshotCallback(fn func(path string) error) {
	s.snapshotFn = fn
}

// SetRestoreCallback sets the restore function
func (s *Server) SetRestoreCallback(fn func(path string) error) {
	s.restoreFn = fn
}

// SetWAL sets the WAL instance for WAL commands
func (s *Server) SetWAL(wal *backup.WAL) {
	s.wal = wal
}

// GetWAL returns the WAL instance
func (s *Server) GetWAL() *backup.WAL {
	return s.wal
}

// Start starts the server
func (s *Server) Start(addr string) error {
	var ln net.Listener
	var err error

	// Check for TLS configuration (supports auto-cert)
	if s.config != nil && s.config.HasTLS() {
		dataDir := s.config.Server.DataDir
		if dataDir == "" {
			dataDir = "./data"
		}

		tlsConfig, tlsEnabled, err := s.config.TLS.LoadOrGenerateTLSConfig(dataDir)
		if err != nil {
			return fmt.Errorf("failed to configure TLS: %w", err)
		}

		if tlsEnabled {
			ln, err = tls.Listen("tcp", addr, tlsConfig)
			if err != nil {
				return err
			}
			if s.config.TLS.AutoCert && s.config.TLS.CertFile == "" {
				logging.Info("GibRAM Protobuf Server listening on %s (TLS auto-cert)", addr)
			} else {
				logging.Info("GibRAM Protobuf Server listening on %s (TLS enabled)", addr)
			}
		} else {
			ln, err = net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			logging.Info("GibRAM Protobuf Server listening on %s", addr)
		}
	} else {
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		logging.Info("GibRAM Protobuf Server listening on %s", addr)
	}

	s.listener = ln

	// Log security info
	if s.apiKeyStore != nil {
		logging.Info("  Authentication: enabled")
	} else {
		logging.Info("  Authentication: disabled (insecure)")
	}
	logging.Info("  Max frame size: %d bytes", s.maxFrameSize)
	logging.Info("  Rate limit: %d req/s (burst: %d)", s.rateLimit, s.rateBurst)

	go s.acceptLoop()
	return nil
}

// Stop stops the server
func (s *Server) Stop() {
	close(s.stopCh)
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				logging.Error("Accept error: %v", err)
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// connState tracks per-connection state
type connState struct {
	authenticated bool
	apiKey        *config.APIKey
	limiter       *rate.Limiter
}

func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	reader := bufio.NewReader(conn)
	state := &connState{}

	// If auth is required, set short timeout for unauthenticated connections
	if s.apiKeyStore != nil {
		conn.SetDeadline(time.Now().Add(s.unauthTimeout))
	}

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		// Read envelope
		env, err := s.readEnvelope(reader)
		if err != nil {
			if err != io.EOF {
				logging.Error("Read envelope error: %v", err)
			}
			return
		}

		// Authentication check
		if s.apiKeyStore != nil && !state.authenticated {
			// First command must be AUTH
			if env.CmdType != pb.CommandType_CMD_AUTH {
				response := &pb.Envelope{
					Version:   ProtocolVersion,
					RequestId: env.RequestId,
					CmdType:   pb.CommandType_CMD_ERROR,
					Payload:   s.errorPayload("authentication required"),
				}
				s.writeEnvelope(conn, response)
				return
			}

			// Handle auth
			response := s.handleAuth(env.Payload, state)
			if err := s.writeEnvelope(conn, response); err != nil {
				logging.Error("Write auth response error: %v", err)
				return
			}

			if !state.authenticated {
				// Auth failed
				return
			}

			// Auth succeeded, extend deadline
			conn.SetDeadline(time.Now().Add(s.idleTimeout))
			continue
		}

		// Rate limiting (per API key)
		if state.limiter != nil && !state.limiter.Allow() {
			response := &pb.Envelope{
				Version:   ProtocolVersion,
				RequestId: env.RequestId,
				CmdType:   pb.CommandType_CMD_ERROR,
				Payload:   s.errorPayload("rate limit exceeded"),
			}
			s.writeEnvelope(conn, response)
			continue
		}

		// Reset idle timeout
		if state.authenticated {
			conn.SetDeadline(time.Now().Add(s.idleTimeout))
		}

		// Process and send response
		response := s.processEnvelope(env, state)
		if err := s.writeEnvelope(conn, response); err != nil {
			logging.Error("Write response error: %v", err)
			return
		}
	}
}

// handleAuth processes authentication request
func (s *Server) handleAuth(payload []byte, state *connState) *pb.Envelope {
	response := &pb.Envelope{
		Version: ProtocolVersion,
		CmdType: pb.CommandType_CMD_AUTH_RESPONSE,
	}

	var req pb.AuthRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		resp := &pb.AuthResponse{Success: false, Message: "invalid auth request"}
		response.Payload, _ = proto.Marshal(resp)
		return response
	}

	apiKey, err := s.apiKeyStore.Validate(req.ApiKey)
	if err != nil {
		resp := &pb.AuthResponse{Success: false, Message: err.Error()}
		response.Payload, _ = proto.Marshal(resp)
		return response
	}

	// Auth succeeded
	state.authenticated = true
	state.apiKey = apiKey

	// Get or create rate limiter for this API key
	if limiter, ok := s.rateLimiters.Load(apiKey.ID); ok {
		state.limiter = limiter.(*rate.Limiter)
	} else {
		limiter := rate.NewLimiter(rate.Limit(s.rateLimit), s.rateBurst)
		s.rateLimiters.Store(apiKey.ID, limiter)
		state.limiter = limiter
	}

	// Build permissions list
	var perms []string
	for perm := range apiKey.Permissions {
		perms = append(perms, perm)
	}

	resp := &pb.AuthResponse{
		Success:     true,
		Message:     "authenticated",
		KeyId:       apiKey.ID,
		Permissions: perms,
	}
	response.Payload, _ = proto.Marshal(resp)
	return response
}

func (s *Server) readEnvelope(r io.Reader) (*pb.Envelope, error) {
	// Read codec type (1 byte)
	var codecByte [1]byte
	if _, err := io.ReadFull(r, codecByte[:]); err != nil {
		return nil, err
	}

	if codec.CodecType(codecByte[0]) != codec.CodecProtobuf {
		return nil, fmt.Errorf("unsupported codec: %d", codecByte[0])
	}

	// Read length (4 bytes, big endian)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	if length > s.maxFrameSize {
		return nil, fmt.Errorf("frame too large: %d (max: %d)", length, s.maxFrameSize)
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

func (s *Server) writeEnvelope(w io.Writer, env *pb.Envelope) error {
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

// =============================================================================
// Helper Methods
// =============================================================================

func (s *Server) errorPayload(msg string) []byte {
	data, _ := proto.Marshal(&pb.Error{Message: msg, Code: -1})
	return data
}

func (s *Server) okPayload(id uint64) []byte {
	data, _ := proto.Marshal(&pb.OkWithID{Id: id})
	return data
}

// getSessionID extracts session_id from envelope (MANDATORY)
func (s *Server) getSessionID(env *pb.Envelope) (string, error) {
	if env.SessionId == "" {
		return "", engine.ErrSessionRequired
	}
	return env.SessionId, nil
}

// =============================================================================
// Command Router
// =============================================================================

func (s *Server) processEnvelope(env *pb.Envelope, state *connState) *pb.Envelope {
	reqID := env.RequestId
	if reqID == 0 {
		reqID = s.requestID.Add(1)
	}

	response := &pb.Envelope{
		Version:   ProtocolVersion,
		RequestId: reqID,
	}

	// RBAC: Check permission for this command
	if state.apiKey != nil {
		requiredPerm, hasMapping := commandPermissions[env.CmdType]
		if hasMapping && !state.apiKey.HasPermission(requiredPerm) {
			response.CmdType = pb.CommandType_CMD_ERROR
			response.Payload = s.errorPayload(fmt.Sprintf("permission denied: requires '%s' permission", requiredPerm))
			return response
		}
	}

	switch env.CmdType {
	// Basic commands (no session required)
	case pb.CommandType_CMD_PING:
		response.CmdType = pb.CommandType_CMD_PONG
		response.Payload = nil

	case pb.CommandType_CMD_INFO:
		response.CmdType = pb.CommandType_CMD_INFO_RESPONSE
		response.Payload = s.handleInfo(env)

	case pb.CommandType_CMD_HEALTH:
		response.CmdType = pb.CommandType_CMD_HEALTH_RESPONSE
		response.Payload = s.handleHealth()

	// Session management commands
	case pb.CommandType_CMD_LIST_SESSIONS:
		response.CmdType, response.Payload = s.handleListSessions()

	case pb.CommandType_CMD_SESSION_INFO:
		response.CmdType, response.Payload = s.handleSessionInfo(env)

	case pb.CommandType_CMD_DELETE_SESSION:
		response.CmdType, response.Payload = s.handleDeleteSession(env)

	case pb.CommandType_CMD_SET_SESSION_TTL:
		response.CmdType, response.Payload = s.handleSetSessionTTL(env)

	case pb.CommandType_CMD_TOUCH_SESSION:
		response.CmdType, response.Payload = s.handleTouchSession(env)

	// Document operations (require session)
	case pb.CommandType_CMD_ADD_DOCUMENT:
		response.CmdType, response.Payload = s.handleAddDocument(env)

	case pb.CommandType_CMD_GET_DOCUMENT:
		response.CmdType, response.Payload = s.handleGetDocument(env)

	case pb.CommandType_CMD_DELETE_DOCUMENT:
		response.CmdType, response.Payload = s.handleDeleteDocument(env)

	// TextUnit operations (require session)
	case pb.CommandType_CMD_ADD_TEXTUNIT:
		response.CmdType, response.Payload = s.handleAddTextUnit(env)

	case pb.CommandType_CMD_GET_TEXTUNIT:
		response.CmdType, response.Payload = s.handleGetTextUnit(env)

	case pb.CommandType_CMD_DELETE_TEXTUNIT:
		response.CmdType, response.Payload = s.handleDeleteTextUnit(env)

	case pb.CommandType_CMD_LINK_TEXTUNIT_ENTITY:
		response.CmdType, response.Payload = s.handleLinkTextUnitEntity(env)

	// Entity operations (require session)
	case pb.CommandType_CMD_ADD_ENTITY:
		response.CmdType, response.Payload = s.handleAddEntity(env)

	case pb.CommandType_CMD_GET_ENTITY:
		response.CmdType, response.Payload = s.handleGetEntity(env)

	case pb.CommandType_CMD_GET_ENTITY_BY_TITLE:
		response.CmdType, response.Payload = s.handleGetEntityByTitle(env)

	case pb.CommandType_CMD_UPDATE_ENTITY_DESC:
		response.CmdType, response.Payload = s.handleUpdateEntityDesc(env)

	case pb.CommandType_CMD_DELETE_ENTITY:
		response.CmdType, response.Payload = s.handleDeleteEntity(env)

	// Relationship operations (require session)
	case pb.CommandType_CMD_ADD_RELATIONSHIP:
		response.CmdType, response.Payload = s.handleAddRelationship(env)

	case pb.CommandType_CMD_GET_RELATIONSHIP:
		response.CmdType, response.Payload = s.handleGetRelationship(env)

	case pb.CommandType_CMD_DELETE_RELATIONSHIP:
		response.CmdType, response.Payload = s.handleDeleteRelationship(env)

	// Community operations (require session)
	case pb.CommandType_CMD_ADD_COMMUNITY:
		response.CmdType, response.Payload = s.handleAddCommunity(env)

	case pb.CommandType_CMD_GET_COMMUNITY:
		response.CmdType, response.Payload = s.handleGetCommunity(env)

	case pb.CommandType_CMD_DELETE_COMMUNITY:
		response.CmdType, response.Payload = s.handleDeleteCommunity(env)

	case pb.CommandType_CMD_COMPUTE_COMMUNITIES:
		response.CmdType, response.Payload = s.handleComputeCommunities(env)

	case pb.CommandType_CMD_HIERARCHICAL_LEIDEN:
		response.CmdType, response.Payload = s.handleHierarchicalLeiden(env)

	// Query operations (require session)
	case pb.CommandType_CMD_QUERY:
		response.CmdType, response.Payload = s.handleQuery(env)

	case pb.CommandType_CMD_EXPLAIN:
		response.CmdType, response.Payload = s.handleExplain(env)

	// Bulk operations (require session)
	case pb.CommandType_CMD_MSET_ENTITIES:
		response.CmdType, response.Payload = s.handleMSetEntities(env)

	case pb.CommandType_CMD_MGET_ENTITIES:
		response.CmdType, response.Payload = s.handleMGetEntities(env)

	case pb.CommandType_CMD_MSET_DOCUMENTS:
		response.CmdType, response.Payload = s.handleMSetDocuments(env)

	case pb.CommandType_CMD_MGET_DOCUMENTS:
		response.CmdType, response.Payload = s.handleMGetDocuments(env)

	case pb.CommandType_CMD_MSET_TEXTUNITS:
		response.CmdType, response.Payload = s.handleMSetTextUnits(env)

	case pb.CommandType_CMD_MGET_TEXTUNITS:
		response.CmdType, response.Payload = s.handleMGetTextUnits(env)

	case pb.CommandType_CMD_MSET_RELATIONSHIPS:
		response.CmdType, response.Payload = s.handleMSetRelationships(env)

	case pb.CommandType_CMD_MGET_RELATIONSHIPS:
		response.CmdType, response.Payload = s.handleMGetRelationships(env)

	case pb.CommandType_CMD_LIST_ENTITIES:
		response.CmdType, response.Payload = s.handleListEntities(env)

	case pb.CommandType_CMD_LIST_RELATIONSHIPS:
		response.CmdType, response.Payload = s.handleListRelationships(env)

	// Pipeline (require session)
	case pb.CommandType_CMD_PIPELINE:
		response.CmdType, response.Payload = s.handlePipeline(env, state)

	// Backup operations (no session)
	case pb.CommandType_CMD_BGSAVE:
		response.CmdType, response.Payload = s.handleBGSave(env.Payload)

	case pb.CommandType_CMD_SAVE:
		response.CmdType, response.Payload = s.handleSave(env.Payload)

	case pb.CommandType_CMD_LASTSAVE:
		response.CmdType, response.Payload = s.handleLastSave()

	case pb.CommandType_CMD_BGRESTORE:
		response.CmdType, response.Payload = s.handleBGRestore(env.Payload)

	case pb.CommandType_CMD_BACKUP_STATUS:
		response.CmdType, response.Payload = s.handleBackupStatus()

	case pb.CommandType_CMD_WAL_STATUS:
		response.CmdType, response.Payload = s.handleWALStatus()

	// Index management (require session)
	case pb.CommandType_CMD_REBUILD_INDEX:
		response.CmdType, response.Payload = s.handleRebuildIndex(env)

	// WAL operations (no session)
	case pb.CommandType_CMD_WAL_CHECKPOINT:
		response.CmdType, response.Payload = s.handleWALCheckpoint()

	case pb.CommandType_CMD_WAL_TRUNCATE:
		response.CmdType, response.Payload = s.handleWALTruncate(env.Payload)

	case pb.CommandType_CMD_WAL_ROTATE:
		response.CmdType, response.Payload = s.handleWALRotate()

	default:
		response.CmdType = pb.CommandType_CMD_ERROR
		response.Payload = s.errorPayload(fmt.Sprintf("unknown command: %d", env.CmdType))
	}

	return response
}

// =============================================================================
// Info & Health Handlers (no session required)
// =============================================================================

func (s *Server) handleInfo(env *pb.Envelope) []byte {
	// If session_id provided, get session-specific info
	if env.SessionId != "" {
		info, err := s.engine.InfoForSession(env.SessionId)
		if err != nil {
			// Fallback to global info for missing/expired sessions to avoid error payloads
			if err == engine.ErrSessionNotFound || err == engine.ErrSessionExpired {
				info = s.engine.Info()
			} else {
				return s.errorPayload(err.Error())
			}
		}
		resp := &pb.InfoResponse{
			Version:           info.Version,
			DocumentCount:     uint64(info.DocumentCount),
			TextunitCount:     uint64(info.TextUnitCount),
			EntityCount:       uint64(info.EntityCount),
			RelationshipCount: uint64(info.RelationshipCount),
			CommunityCount:    uint64(info.CommunityCount),
			VectorDim:         int32(info.VectorDim),
			SessionCount:      int32(info.SessionCount),
		}
		data, _ := proto.Marshal(resp)
		return data
	}

	// Global info across all sessions
	info := s.engine.Info()
	resp := &pb.InfoResponse{
		Version:           info.Version,
		DocumentCount:     uint64(info.DocumentCount),
		TextunitCount:     uint64(info.TextUnitCount),
		EntityCount:       uint64(info.EntityCount),
		RelationshipCount: uint64(info.RelationshipCount),
		CommunityCount:    uint64(info.CommunityCount),
		VectorDim:         int32(info.VectorDim),
		SessionCount:      int32(info.SessionCount),
	}
	data, _ := proto.Marshal(resp)
	return data
}

func (s *Server) handleHealth() []byte {
	backupStatus := "not_configured"
	if s.snapshotFn != nil {
		if s.backupInProgress.Load() {
			backupStatus = "in_progress"
		} else {
			backupStatus = "ok"
		}
	}

	resp := &pb.HealthResponse{
		Status: "ok",
		Components: map[string]string{
			"engine": "ok",
			"backup": backupStatus,
		},
	}
	data, _ := proto.Marshal(resp)
	return data
}

// =============================================================================
// Session Management Handlers
// =============================================================================

func (s *Server) handleListSessions() (pb.CommandType, []byte) {
	sessions := s.engine.ListSessions()

	resp := &pb.ListSessionsResponse{
		Sessions: make([]*pb.SessionInfo, len(sessions)),
	}

	for i, sess := range sessions {
		resp.Sessions[i] = &pb.SessionInfo{
			SessionId:         sess.ID,
			CreatedAt:         sess.CreatedAt,
			LastAccess:        sess.LastAccess,
			Ttl:               sess.TTL,
			IdleTtl:           sess.IdleTTL,
			DocumentCount:     uint64(sess.DocumentCount),
			TextunitCount:     uint64(sess.TextUnitCount),
			EntityCount:       uint64(sess.EntityCount),
			RelationshipCount: uint64(sess.RelationshipCount),
			CommunityCount:    uint64(sess.CommunityCount),
		}
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_OK, data
}

func (s *Server) handleSessionInfo(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	info, err := s.engine.GetSessionInfo(sessionID)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	resp := &pb.SessionInfo{
		SessionId:         info.ID,
		CreatedAt:         info.CreatedAt,
		LastAccess:        info.LastAccess,
		Ttl:               info.TTL,
		IdleTtl:           info.IdleTTL,
		DocumentCount:     uint64(info.DocumentCount),
		TextunitCount:     uint64(info.TextUnitCount),
		EntityCount:       uint64(info.EntityCount),
		RelationshipCount: uint64(info.RelationshipCount),
		CommunityCount:    uint64(info.CommunityCount),
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_OK, data
}

func (s *Server) handleDeleteSession(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	deleted := s.engine.DeleteSession(sessionID)
	if !deleted {
		return pb.CommandType_CMD_ERROR, s.errorPayload("session not found")
	}

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

func (s *Server) handleSetSessionTTL(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.SetSessionTTLRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if err := s.engine.SetSessionTTL(sessionID, req.Ttl, req.IdleTtl); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

func (s *Server) handleTouchSession(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if err := s.engine.TouchSession(sessionID); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

// =============================================================================
// Document Handlers
// =============================================================================

func (s *Server) handleAddDocument(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.AddDocumentRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	doc, err := s.engine.AddDocument(sessionID, req.ExternalId, req.Filename)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(doc.ID)
}

func (s *Server) handleGetDocument(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.GetByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	doc, ok := s.engine.GetDocument(sessionID, req.Id)
	if !ok {
		return pb.CommandType_CMD_ERROR, s.errorPayload("document not found")
	}

	data, _ := proto.Marshal(codec.DocumentToProto(doc))
	return pb.CommandType_CMD_DOCUMENT_RESPONSE, data
}

func (s *Server) handleDeleteDocument(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.DeleteByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if !s.engine.DeleteDocument(sessionID, req.Id) {
		return pb.CommandType_CMD_ERROR, s.errorPayload("document not found")
	}

	return pb.CommandType_CMD_OK, s.okPayload(req.Id)
}

// =============================================================================
// TextUnit Handlers
// =============================================================================

func (s *Server) handleAddTextUnit(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.AddTextUnitRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	tu, err := s.engine.AddTextUnit(
		sessionID, req.ExternalId, req.DocumentId, req.Content,
		req.Embedding, int(req.TokenCount),
	)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(tu.ID)
}

func (s *Server) handleGetTextUnit(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.GetByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	tu, ok := s.engine.GetTextUnit(sessionID, req.Id)
	if !ok {
		return pb.CommandType_CMD_ERROR, s.errorPayload("textunit not found")
	}

	data, _ := proto.Marshal(codec.TextUnitToProto(tu))
	return pb.CommandType_CMD_TEXTUNIT_RESPONSE, data
}

func (s *Server) handleDeleteTextUnit(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.DeleteByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if !s.engine.DeleteTextUnit(sessionID, req.Id) {
		return pb.CommandType_CMD_ERROR, s.errorPayload("textunit not found")
	}

	return pb.CommandType_CMD_OK, s.okPayload(req.Id)
}

func (s *Server) handleLinkTextUnitEntity(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.LinkTextUnitEntityRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if !s.engine.LinkTextUnitToEntity(sessionID, req.TextunitId, req.EntityId) {
		return pb.CommandType_CMD_ERROR, s.errorPayload("link failed")
	}

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

// =============================================================================
// Entity Handlers
// =============================================================================

func (s *Server) handleAddEntity(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.AddEntityRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	ent, err := s.engine.AddEntity(
		sessionID, req.ExternalId, req.Title, req.Type, req.Description, req.Embedding,
	)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(ent.ID)
}

func (s *Server) handleGetEntity(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.GetByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	ent, ok := s.engine.GetEntity(sessionID, req.Id)
	if !ok {
		return pb.CommandType_CMD_ERROR, s.errorPayload("entity not found")
	}

	data, _ := proto.Marshal(codec.EntityToProto(ent))
	return pb.CommandType_CMD_ENTITY_RESPONSE, data
}

func (s *Server) handleGetEntityByTitle(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.GetEntityByTitleRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	ent, ok := s.engine.GetEntityByTitle(sessionID, req.Title)
	if !ok {
		return pb.CommandType_CMD_ERROR, s.errorPayload("entity not found")
	}

	data, _ := proto.Marshal(codec.EntityToProto(ent))
	return pb.CommandType_CMD_ENTITY_RESPONSE, data
}

func (s *Server) handleUpdateEntityDesc(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.UpdateEntityDescRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if !s.engine.UpdateEntityDescription(sessionID, req.Id, req.Description, req.Embedding) {
		return pb.CommandType_CMD_ERROR, s.errorPayload("update failed")
	}

	return pb.CommandType_CMD_OK, s.okPayload(req.Id)
}

func (s *Server) handleDeleteEntity(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.DeleteByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if !s.engine.DeleteEntity(sessionID, req.Id) {
		return pb.CommandType_CMD_ERROR, s.errorPayload("entity not found")
	}

	return pb.CommandType_CMD_OK, s.okPayload(req.Id)
}

// =============================================================================
// Relationship Handlers
// =============================================================================

func (s *Server) handleAddRelationship(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.AddRelationshipRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	rel, err := s.engine.AddRelationship(
		sessionID, req.ExternalId, req.SourceId, req.TargetId,
		req.Type, req.Description, req.Weight,
	)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(rel.ID)
}

func (s *Server) handleGetRelationship(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.GetByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	rel, ok := s.engine.GetRelationship(sessionID, req.Id)
	if !ok {
		return pb.CommandType_CMD_ERROR, s.errorPayload("relationship not found")
	}

	data, _ := proto.Marshal(codec.RelationshipToProto(rel))
	return pb.CommandType_CMD_RELATIONSHIP_RESPONSE, data
}

func (s *Server) handleDeleteRelationship(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.DeleteByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if !s.engine.DeleteRelationship(sessionID, req.Id) {
		return pb.CommandType_CMD_ERROR, s.errorPayload("relationship not found")
	}

	return pb.CommandType_CMD_OK, s.okPayload(req.Id)
}

// =============================================================================
// Community Handlers
// =============================================================================

func (s *Server) handleAddCommunity(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.AddCommunityRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	comm, err := s.engine.AddCommunity(
		sessionID, req.ExternalId, req.Title, req.Summary, req.FullContent,
		int(req.Level), req.EntityIds, req.RelationshipIds, req.Embedding,
	)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(comm.ID)
}

func (s *Server) handleGetCommunity(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.GetByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	comm, ok := s.engine.GetCommunity(sessionID, req.Id)
	if !ok {
		return pb.CommandType_CMD_ERROR, s.errorPayload("community not found")
	}

	data, _ := proto.Marshal(codec.CommunityToProto(comm))
	return pb.CommandType_CMD_COMMUNITY_RESPONSE, data
}

func (s *Server) handleDeleteCommunity(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.DeleteByIDRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if !s.engine.DeleteCommunity(sessionID, req.Id) {
		return pb.CommandType_CMD_ERROR, s.errorPayload("community not found")
	}

	return pb.CommandType_CMD_OK, s.okPayload(req.Id)
}

// =============================================================================
// Community Computation Handlers
// =============================================================================

func (s *Server) handleComputeCommunities(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.ComputeCommunitiesRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	config := graph.LeidenConfig{
		Resolution: req.Resolution,
		Iterations: int(req.Iterations),
		MinDelta:   0.0001,
		RandomSeed: 42,
	}

	communities, err := s.engine.ComputeCommunities(sessionID, config)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	resp := &pb.ComputeCommunitiesResponse{
		Count:       int32(len(communities)),
		Communities: make([]*pb.Community, len(communities)),
	}
	for i, c := range communities {
		resp.Communities[i] = codec.CommunityToProto(c)
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_COMMUNITIES_RESPONSE, data
}

func (s *Server) handleHierarchicalLeiden(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.HierarchicalLeidenRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	maxLevels := int(req.MaxLevels)
	if maxLevels < 1 {
		maxLevels = 5
	}
	if maxLevels > 5 {
		maxLevels = 5
	}

	config := graph.LeidenConfig{
		Resolution:       req.Resolution,
		Iterations:       10,
		MinDelta:         0.0001,
		RandomSeed:       42,
		MaxLevels:        maxLevels,
		MinCommunitySize: 3,
		LevelResolution:  0.7,
	}

	communities, err := s.engine.ComputeHierarchicalCommunities(sessionID, config)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	levelCounts := make(map[int32]int32)
	for _, c := range communities {
		levelCounts[int32(c.Level)]++
	}

	resp := &pb.HierarchicalLeidenResponse{
		LevelCounts:      levelCounts,
		TotalCommunities: int32(len(communities)),
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_COMMUNITIES_RESPONSE, data
}

// =============================================================================
// Query Handlers
// =============================================================================

func (s *Server) handleQuery(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.QueryRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	// Convert to types.QuerySpec
	spec := types.QuerySpec{
		QueryVector:    req.QueryVector,
		TopK:           int(req.TopK),
		KHops:          int(req.KHops),
		MaxEntities:    int(req.MaxEntities),
		MaxTextUnits:   int(req.MaxTextunits),
		MaxCommunities: int(req.MaxCommunities),
	}

	// Convert search types
	for _, st := range req.SearchTypes {
		spec.SearchTypes = append(spec.SearchTypes, types.SearchType(st))
	}

	// Apply defaults
	if spec.TopK == 0 {
		spec.TopK = 10
	}
	if spec.KHops == 0 {
		spec.KHops = 2
	}
	if spec.MaxEntities == 0 {
		spec.MaxEntities = 50
	}
	if spec.MaxTextUnits == 0 {
		spec.MaxTextUnits = 10
	}
	if spec.MaxCommunities == 0 {
		spec.MaxCommunities = 5
	}
	if len(spec.SearchTypes) == 0 {
		spec.SearchTypes = []types.SearchType{
			types.SearchTypeTextUnit,
			types.SearchTypeEntity,
			types.SearchTypeCommunity,
		}
	}

	result, err := s.engine.Query(sessionID, spec)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	// Convert to protobuf response
	resp := &pb.QueryResponse{
		QueryId: result.QueryID,
		Stats: &pb.QueryStats{
			DurationMicros:  result.Stats.DurationMicros,
			VectorSearches:  int32(result.Stats.TextUnitsSearched + result.Stats.EntitiesSearched + result.Stats.CommunitiesSearched),
			GraphTraversals: int32(result.Stats.EdgesScanned),
		},
	}

	for _, tu := range result.TextUnits {
		resp.Textunits = append(resp.Textunits, &pb.TextUnitResult{
			Textunit:   codec.TextUnitToProto(tu.TextUnit),
			Similarity: tu.Similarity,
			Hop:        int32(tu.Hop),
		})
	}

	for _, ent := range result.Entities {
		resp.Entities = append(resp.Entities, &pb.EntityResult{
			Entity:     codec.EntityToProto(ent.Entity),
			Similarity: ent.Similarity,
			Hop:        int32(ent.Hop),
		})
	}

	for _, comm := range result.Communities {
		resp.Communities = append(resp.Communities, &pb.CommunityResult{
			Community:  codec.CommunityToProto(comm.Community),
			Similarity: comm.Similarity,
		})
	}

	for _, rel := range result.Relationships {
		resp.Relationships = append(resp.Relationships, &pb.RelationshipResult{
			Relationship: codec.RelationshipToProto(rel.Relationship),
			SourceTitle:  rel.SourceTitle,
			TargetTitle:  rel.TargetTitle,
		})
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_QUERY_RESPONSE, data
}

func (s *Server) handleExplain(env *pb.Envelope) (pb.CommandType, []byte) {
	var req pb.ExplainRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	explain, ok := s.engine.Explain(req.QueryId)
	if !ok {
		return pb.CommandType_CMD_ERROR, s.errorPayload("query not found")
	}

	resp := &pb.ExplainResponse{
		QueryId: explain.QueryID,
	}

	for _, seed := range explain.Seeds {
		resp.Seeds = append(resp.Seeds, &pb.SeedInfo{
			Type:       string(seed.Type),
			Id:         seed.ID,
			ExternalId: seed.ExternalID,
			Similarity: seed.Similarity,
		})
	}

	for _, step := range explain.Traversal {
		resp.Traversal = append(resp.Traversal, &pb.TraversalStep{
			FromEntityId:   step.FromEntityID,
			ToEntityId:     step.ToEntityID,
			RelationshipId: step.RelationshipID,
			RelType:        step.RelType,
			Weight:         step.Weight,
			Hop:            int32(step.Hop),
		})
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_EXPLAIN_RESPONSE, data
}

// =============================================================================
// Bulk Operation Handlers
// =============================================================================

func (s *Server) handleMSetEntities(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MSetEntitiesRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	inputs := make([]types.BulkEntityInput, len(req.Entities))
	for i, e := range req.Entities {
		inputs[i] = types.BulkEntityInput{
			ExternalID:  e.ExternalId,
			Title:       e.Title,
			Type:        e.Type,
			Description: e.Description,
			Embedding:   e.Embedding,
		}
	}

	ids, err := s.engine.MSetEntities(sessionID, inputs)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	resp := &pb.EntitiesResponse{CreatedIds: ids}
	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_ENTITIES_RESPONSE, data
}

func (s *Server) handleMGetEntities(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MGetEntitiesRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	entities := s.engine.MGetEntities(sessionID, req.Ids)
	resp := &pb.EntitiesResponse{
		Entities: make([]*pb.Entity, len(entities)),
	}
	for i, ent := range entities {
		resp.Entities[i] = codec.EntityToProto(ent)
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_ENTITIES_RESPONSE, data
}

func (s *Server) handleListEntities(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.ListEntitiesRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}

	entities, nextCursor := s.engine.ListEntities(sessionID, req.Cursor, limit)
	resp := &pb.EntitiesResponse{
		Entities:   make([]*pb.Entity, len(entities)),
		NextCursor: nextCursor,
	}
	for i, ent := range entities {
		resp.Entities[i] = codec.EntityToProto(ent)
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_ENTITIES_RESPONSE, data
}

func (s *Server) handleMSetDocuments(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MSetDocumentsRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	inputs := make([]types.BulkDocumentInput, len(req.Documents))
	for i, d := range req.Documents {
		inputs[i] = types.BulkDocumentInput{
			ExternalID: d.ExternalId,
			Filename:   d.Filename,
		}
	}

	ids, err := s.engine.MSetDocuments(sessionID, inputs)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	resp := &pb.DocumentsResponse{CreatedIds: ids}
	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_DOCUMENTS_RESPONSE, data
}

func (s *Server) handleMGetDocuments(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MGetDocumentsRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	docs := s.engine.MGetDocuments(sessionID, req.Ids)
	resp := &pb.DocumentsResponse{
		Documents: make([]*pb.Document, len(docs)),
	}
	for i, doc := range docs {
		resp.Documents[i] = codec.DocumentToProto(doc)
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_DOCUMENTS_RESPONSE, data
}

func (s *Server) handleMSetTextUnits(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MSetTextUnitsRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	inputs := make([]types.BulkTextUnitInput, len(req.Textunits))
	for i, t := range req.Textunits {
		inputs[i] = types.BulkTextUnitInput{
			ExternalID: t.ExternalId,
			DocumentID: t.DocumentId,
			Content:    t.Content,
			Embedding:  t.Embedding,
			TokenCount: int(t.TokenCount),
		}
	}

	ids, err := s.engine.MSetTextUnits(sessionID, inputs)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	resp := &pb.TextUnitsResponse{CreatedIds: ids}
	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_TEXTUNITS_RESPONSE, data
}

func (s *Server) handleMGetTextUnits(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MGetTextUnitsRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	textunits := s.engine.MGetTextUnits(sessionID, req.Ids)
	resp := &pb.TextUnitsResponse{
		Textunits: make([]*pb.TextUnit, len(textunits)),
	}
	for i, tu := range textunits {
		resp.Textunits[i] = codec.TextUnitToProto(tu)
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_TEXTUNITS_RESPONSE, data
}

func (s *Server) handleMSetRelationships(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MSetRelationshipsRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	inputs := make([]types.BulkRelationshipInput, len(req.Relationships))
	for i, r := range req.Relationships {
		inputs[i] = types.BulkRelationshipInput{
			ExternalID:  r.ExternalId,
			SourceID:    r.SourceId,
			TargetID:    r.TargetId,
			Type:        r.Type,
			Description: r.Description,
			Weight:      r.Weight,
		}
	}

	ids, err := s.engine.MSetRelationships(sessionID, inputs)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	resp := &pb.RelationshipsResponse{CreatedIds: ids}
	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_RELATIONSHIPS_RESPONSE, data
}

func (s *Server) handleMGetRelationships(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.MGetRelationshipsRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	rels := s.engine.MGetRelationships(sessionID, req.Ids)
	resp := &pb.RelationshipsResponse{
		Relationships: make([]*pb.Relationship, len(rels)),
	}
	for i, rel := range rels {
		resp.Relationships[i] = codec.RelationshipToProto(rel)
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_RELATIONSHIPS_RESPONSE, data
}

func (s *Server) handleListRelationships(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	var req pb.ListRelationshipsRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}

	rels, nextCursor := s.engine.ListRelationships(sessionID, req.Cursor, limit)
	resp := &pb.RelationshipsResponse{
		Relationships: make([]*pb.Relationship, len(rels)),
		NextCursor:    nextCursor,
	}
	for i, rel := range rels {
		resp.Relationships[i] = codec.RelationshipToProto(rel)
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_RELATIONSHIPS_RESPONSE, data
}

// =============================================================================
// Pipeline Handler
// =============================================================================

func (s *Server) handlePipeline(env *pb.Envelope, state *connState) (pb.CommandType, []byte) {
	var req pb.PipelineRequest
	if err := proto.Unmarshal(env.Payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	responses := make([]*pb.Envelope, 0, len(req.Commands))
	for _, cmd := range req.Commands {
		resp := s.processEnvelope(cmd, state)
		responses = append(responses, resp)
	}

	resp := &pb.PipelineResponse{Responses: responses}
	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_PIPELINE_RESPONSE, data
}

// =============================================================================
// Backup Handlers
// =============================================================================

func (s *Server) handleBGSave(payload []byte) (pb.CommandType, []byte) {
	if s.snapshotFn == nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload("backup not configured")
	}

	if s.backupInProgress.Load() {
		return pb.CommandType_CMD_ERROR, s.errorPayload("backup already in progress")
	}

	var req pb.SaveRequest
	if len(payload) > 0 {
		proto.Unmarshal(payload, &req)
	}

	// Default path if not specified
	savePath := req.Path
	if savePath == "" && s.config != nil {
		savePath = s.config.Server.DataDir + "/snapshot.gibram"
	}

	s.backupInProgress.Store(true)
	s.backupType = "save"
	s.backupStartTime = time.Now().Unix()

	go func() {
		defer s.backupInProgress.Store(false)

		if err := s.snapshotFn(savePath); err != nil {
			logging.Error("Background save failed: %v", err)
			return
		}

		s.lastSaveTime = time.Now().Unix()
		s.lastSavePath = savePath
		logging.Info("Background save completed to %s", savePath)
	}()

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

func (s *Server) handleSave(payload []byte) (pb.CommandType, []byte) {
	if s.snapshotFn == nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload("backup not configured")
	}

	var req pb.SaveRequest
	if len(payload) > 0 {
		proto.Unmarshal(payload, &req)
	}

	// Default path if not specified
	savePath := req.Path
	if savePath == "" && s.config != nil {
		savePath = s.config.Server.DataDir + "/snapshot.gibram"
	}

	if err := s.snapshotFn(savePath); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	s.lastSaveTime = time.Now().Unix()
	s.lastSavePath = savePath

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

func (s *Server) handleLastSave() (pb.CommandType, []byte) {
	resp := &pb.LastSaveResponse{
		Timestamp: s.lastSaveTime,
		Path:      s.lastSavePath,
	}
	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_BACKUP_RESPONSE, data
}

func (s *Server) handleBGRestore(payload []byte) (pb.CommandType, []byte) {
	if s.restoreFn == nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload("restore not configured")
	}

	if s.backupInProgress.Load() {
		return pb.CommandType_CMD_ERROR, s.errorPayload("operation already in progress")
	}

	var req pb.RestoreRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	s.backupInProgress.Store(true)
	s.backupType = "restore"
	s.backupStartTime = time.Now().Unix()

	go func() {
		defer s.backupInProgress.Store(false)

		if err := s.restoreFn(req.Path); err != nil {
			logging.Error("Background restore failed: %v", err)
			return
		}

		logging.Info("Background restore completed from %s", req.Path)
	}()

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

func (s *Server) handleBackupStatus() (pb.CommandType, []byte) {
	resp := &pb.BackupStatusResponse{
		InProgress:   s.backupInProgress.Load(),
		Type:         s.backupType,
		StartTime:    s.backupStartTime,
		LastSaveTime: s.lastSaveTime,
		LastSavePath: s.lastSavePath,
	}
	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_BACKUP_RESPONSE, data
}

func (s *Server) handleWALStatus() (pb.CommandType, []byte) {
	resp := &pb.WALStatusResponse{}

	// Get real WAL status if available
	if s.wal != nil {
		resp.CurrentLsn = s.wal.CurrentLSN()
		resp.FlushedLsn = s.wal.FlushedLSN()
		resp.SegmentCount = int32(s.wal.SegmentCount())
		resp.TotalSizeBytes = s.wal.TotalSize()
	}

	data, _ := proto.Marshal(resp)
	return pb.CommandType_CMD_BACKUP_RESPONSE, data
}

// =============================================================================
// Index Management Handlers
// =============================================================================

func (s *Server) handleRebuildIndex(env *pb.Envelope) (pb.CommandType, []byte) {
	sessionID, err := s.getSessionID(env)
	if err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	if err := s.engine.RebuildVectorIndices(sessionID); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(err.Error())
	}

	return pb.CommandType_CMD_OK, s.okPayload(0)
}

// =============================================================================
// WAL Operation Handlers
// =============================================================================

func (s *Server) handleWALCheckpoint() (pb.CommandType, []byte) {
	if s.wal == nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload("WAL not configured")
	}

	// Checkpoint: sync WAL to disk
	if err := s.wal.Sync(); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(fmt.Sprintf("checkpoint failed: %v", err))
	}

	// Also trigger a snapshot if configured
	if s.snapshotFn != nil {
		go func() {
			if err := s.snapshotFn(""); err != nil {
				logging.Error("Checkpoint snapshot failed: %v", err)
			}
		}()
	}

	return pb.CommandType_CMD_OK, s.okPayload(s.wal.FlushedLSN())
}

func (s *Server) handleWALTruncate(payload []byte) (pb.CommandType, []byte) {
	if s.wal == nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload("WAL not configured")
	}

	// Parse the truncate request to get target LSN
	// If payload provided, try to parse as uint64 (simple encoding)
	var targetLSN uint64
	if len(payload) >= 8 {
		targetLSN = binary.BigEndian.Uint64(payload[:8])
	}

	// If no target specified, truncate up to flushed LSN
	if targetLSN == 0 {
		targetLSN = s.wal.FlushedLSN()
	}

	// Truncate old WAL segments
	if err := s.wal.TruncateBefore(targetLSN); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(fmt.Sprintf("truncate failed: %v", err))
	}

	return pb.CommandType_CMD_OK, s.okPayload(targetLSN)
}

func (s *Server) handleWALRotate() (pb.CommandType, []byte) {
	if s.wal == nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload("WAL not configured")
	}

	// Force rotate to new segment
	if err := s.wal.Rotate(); err != nil {
		return pb.CommandType_CMD_ERROR, s.errorPayload(fmt.Sprintf("rotate failed: %v", err))
	}

	return pb.CommandType_CMD_OK, s.okPayload(uint64(s.wal.SegmentCount()))
}
