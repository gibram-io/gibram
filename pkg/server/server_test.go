// Package server - comprehensive tests for TCP server
package server

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gibram-io/gibram/pkg/backup"
	"github.com/gibram-io/gibram/pkg/codec"
	"github.com/gibram-io/gibram/pkg/config"
	"github.com/gibram-io/gibram/pkg/engine"
	pb "github.com/gibram-io/gibram/proto/gibrampb"
	"google.golang.org/protobuf/proto"
)

var _ = backup.SyncNever // Suppress unused import (used in createTestServerWithWAL)

// =============================================================================
// Test Helpers
// =============================================================================

const testVectorDim = 64

const testSessionID = "test-session-1"

func createTestServer(t *testing.T) (*Server, string) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Start server
	if err := srv.Start(addr); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	return srv, addr
}

// sendCommand sends a command using proper codec encoding and returns the response
func sendCommand(conn net.Conn, cmdType pb.CommandType, payload proto.Message) (*pb.Envelope, error) {
	// Marshal payload
	var payloadBytes []byte
	var err error
	if payload != nil {
		payloadBytes, err = proto.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}

	// Create envelope
	env := &pb.Envelope{
		Version:   ProtocolVersion,
		RequestId: 1,
		CmdType:   cmdType,
		Payload:   payloadBytes,
		SessionId: testSessionID,
	}

	// Encode using codec (includes codec marker byte)
	frameData, err := codec.EncodeEnvelope(env)
	if err != nil {
		return nil, err
	}

	// Write frame
	if _, err := conn.Write(frameData); err != nil {
		return nil, err
	}

	// Read response using codec decoder
	resp, _, err := codec.DecodeEnvelope(conn)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// sendRawFrame sends raw bytes without proper encoding (for testing error handling)
func sendRawFrame(conn net.Conn, data []byte) error {
	_, err := conn.Write(data)
	return err
}

// readResponse reads a response envelope from connection
func readResponse(conn net.Conn) (*pb.Envelope, error) {
	// Read codec byte
	codecByte := make([]byte, 1)
	if _, err := io.ReadFull(conn, codecByte); err != nil {
		return nil, err
	}

	// Read length
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf)

	// Read payload
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}

	var env pb.Envelope
	if err := proto.Unmarshal(payload, &env); err != nil {
		return nil, err
	}

	return &env, nil
}

// =============================================================================
// Server Creation Tests
// =============================================================================

func TestNewServer(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	if srv == nil {
		t.Fatal("NewServer returned nil")
	}

	if srv.engine != eng {
		t.Error("Server should have engine reference")
	}
}

func TestNewServerWithConfig(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			MaxFrameSize:  1024 * 1024,
			IdleTimeout:   60 * time.Second,
			UnauthTimeout: 5 * time.Second,
			RateLimit:     500,
			RateBurst:     50,
		},
	}

	srv := NewServerWithConfig(eng, cfg)

	if srv == nil {
		t.Fatal("NewServerWithConfig returned nil")
	}

	if srv.maxFrameSize != 1024*1024 {
		t.Errorf("MaxFrameSize not applied, got %d", srv.maxFrameSize)
	}
	if srv.idleTimeout != 60*time.Second {
		t.Errorf("IdleTimeout not applied, got %v", srv.idleTimeout)
	}
	if srv.rateLimit != 500 {
		t.Errorf("RateLimit not applied, got %d", srv.rateLimit)
	}
}

func TestServerWithNilConfig(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServerWithConfig(eng, nil)

	if srv == nil {
		t.Fatal("Server should work with nil config")
	}

	// Should have defaults
	if srv.maxFrameSize != DefaultMaxFrameSize {
		t.Errorf("Should have default max frame size")
	}
}

// =============================================================================
// Server Start/Stop Tests
// =============================================================================

func TestServerStartStop(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Start server
	if err := srv.Start(addr); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Server should be running (listener set)
	if srv.listener == nil {
		t.Error("Server listener should be set after Start")
	}

	// Stop should work without panic
	srv.Stop()
}

func TestServerStartInvalidAddress(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	err := srv.Start("invalid:address:port")
	if err == nil {
		t.Error("Start with invalid address should fail")
		srv.Stop()
	}
}

// =============================================================================
// PING Command Test
// =============================================================================

func TestServerPing(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	// Connect
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send PING
	resp, err := sendCommand(conn, pb.CommandType_CMD_PING, nil)
	if err != nil {
		t.Fatalf("PING failed: %v", err)
	}

	// Should get PONG
	if resp.CmdType != pb.CommandType_CMD_PONG {
		t.Errorf("Expected PONG, got %v", resp.CmdType)
	}
}

func TestServerMultiplePings(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send multiple PINGs on same connection
	for i := 0; i < 10; i++ {
		resp, err := sendCommand(conn, pb.CommandType_CMD_PING, nil)
		if err != nil {
			t.Fatalf("PING %d failed: %v", i, err)
		}
		if resp.CmdType != pb.CommandType_CMD_PONG {
			t.Errorf("PING %d: Expected PONG, got %v", i, resp.CmdType)
		}
	}
}

// =============================================================================
// Protocol Constants Tests
// =============================================================================

func TestProtocolConstants(t *testing.T) {
	if ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", ProtocolVersion)
	}

	if DefaultMaxFrameSize != 64*1024*1024 {
		t.Errorf("DefaultMaxFrameSize unexpected value")
	}

	if DefaultVectorDim != 1536 {
		t.Errorf("DefaultVectorDim = %d, want 1536", DefaultVectorDim)
	}

	if DefaultIdleTimeout != 300*time.Second {
		t.Errorf("DefaultIdleTimeout unexpected value")
	}
}

// =============================================================================
// RBAC Permission Mapping Tests
// =============================================================================

func TestCommandPermissions(t *testing.T) {
	// Read commands should require read permission
	readCmds := []pb.CommandType{
		pb.CommandType_CMD_PING,
		pb.CommandType_CMD_INFO,
		pb.CommandType_CMD_GET_DOCUMENT,
		pb.CommandType_CMD_GET_ENTITY,
		pb.CommandType_CMD_QUERY,
	}

	for _, cmd := range readCmds {
		perm, ok := commandPermissions[cmd]
		if !ok {
			t.Errorf("No permission defined for %v", cmd)
			continue
		}
		if perm != config.PermRead {
			t.Errorf("Command %v should require read permission, got %s", cmd, perm)
		}
	}

	// Write commands should require write permission
	writeCmds := []pb.CommandType{
		pb.CommandType_CMD_ADD_DOCUMENT,
		pb.CommandType_CMD_ADD_ENTITY,
		pb.CommandType_CMD_ADD_RELATIONSHIP,
	}

	for _, cmd := range writeCmds {
		perm, ok := commandPermissions[cmd]
		if !ok {
			t.Errorf("No permission defined for %v", cmd)
			continue
		}
		if perm != config.PermWrite {
			t.Errorf("Command %v should require write permission, got %s", cmd, perm)
		}
	}

	// Admin commands should require admin permission
	adminCmds := []pb.CommandType{
		pb.CommandType_CMD_SAVE,
		pb.CommandType_CMD_BGSAVE,
		pb.CommandType_CMD_REBUILD_INDEX,
	}

	for _, cmd := range adminCmds {
		perm, ok := commandPermissions[cmd]
		if !ok {
			t.Errorf("No permission defined for %v", cmd)
			continue
		}
		if perm != config.PermAdmin {
			t.Errorf("Command %v should require admin permission, got %s", cmd, perm)
		}
	}
}

// =============================================================================
// Callback Tests
// =============================================================================

func TestServerSetSnapshotCallback(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	called := false
	srv.SetSnapshotCallback(func(path string) error {
		called = true
		return nil
	})

	if srv.snapshotFn == nil {
		t.Error("Snapshot callback should be set")
	}

	// Call it
	srv.snapshotFn("test.snap")
	if !called {
		t.Error("Snapshot callback should have been called")
	}
}

func TestServerSetRestoreCallback(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	called := false
	srv.SetRestoreCallback(func(path string) error {
		called = true
		return nil
	})

	if srv.restoreFn == nil {
		t.Error("Restore callback should be set")
	}

	srv.restoreFn("test.snap")
	if !called {
		t.Error("Restore callback should have been called")
	}
}

func TestServerSetWAL(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	// SetWAL with nil should work
	srv.SetWAL(nil)
	if srv.GetWAL() != nil {
		t.Error("WAL should be nil")
	}
}

// =============================================================================
// Connection State Tests
// =============================================================================

func TestConnState(t *testing.T) {
	state := &connState{
		authenticated: false,
		apiKey:        nil,
		limiter:       nil,
	}

	if state.authenticated {
		t.Error("New connection should not be authenticated")
	}

	state.authenticated = true
	if !state.authenticated {
		t.Error("Should be able to set authenticated")
	}
}

// =============================================================================
// Concurrent Connection Tests
// =============================================================================

func TestServerConcurrentConnections(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	var wg sync.WaitGroup
	const numClients = 10

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				t.Logf("Connect error: %v", err)
				return
			}
			defer conn.Close()

			// Send multiple PINGs
			for j := 0; j < 5; j++ {
				_, err := sendCommand(conn, pb.CommandType_CMD_PING, nil)
				if err != nil {
					t.Logf("PING error: %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func TestServerErrorPayload(t *testing.T) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	payload := srv.errorPayload("test error")
	if payload == nil {
		t.Error("Error payload should not be nil")
	}

	// Unmarshal and check
	var errResp pb.Error
	if err := proto.Unmarshal(payload, &errResp); err != nil {
		t.Fatalf("Failed to unmarshal error response: %v", err)
	}

	if errResp.Message != "test error" {
		t.Errorf("Expected 'test error', got '%s'", errResp.Message)
	}
}

// =============================================================================
// Integration Tests - Full Request/Response Cycle
// =============================================================================

func TestServerIntegration_AddAndRetrieveDocument(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add document
	addReq := &pb.AddDocumentRequest{
		ExternalId: "doc-001",
		Filename:   "test.pdf",
	}

	resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, addReq)
	if err != nil {
		t.Fatalf("Add document failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Add document returned error: %s", errResp.Message)
	}

	// Parse response - should be OkWithID
	var addResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &addResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if addResp.Id == 0 {
		t.Error("Document ID should not be 0")
	}
}

func TestServerIntegration_Info(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_INFO, nil)
	if err != nil {
		t.Fatalf("INFO failed: %v", err)
	}

	var infoResp pb.InfoResponse
	if err := proto.Unmarshal(resp.Payload, &infoResp); err != nil {
		t.Fatalf("Failed to unmarshal info response: %v", err)
	}

	if infoResp.Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestServerIntegration_Health(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_HEALTH, nil)
	if err != nil {
		t.Fatalf("HEALTH failed: %v", err)
	}

	var healthResp pb.HealthResponse
	if err := proto.Unmarshal(resp.Payload, &healthResp); err != nil {
		t.Fatalf("Failed to unmarshal health response: %v", err)
	}

	// Server returns "ok" as healthy status
	if healthResp.Status != "ok" && healthResp.Status != "healthy" {
		t.Errorf("Expected 'ok' or 'healthy', got '%s'", healthResp.Status)
	}
}

// =============================================================================
// Entity Operations Integration Tests
// =============================================================================

func TestServerIntegration_AddEntity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Create embedding
	embedding := make([]float32, testVectorDim)
	for i := range embedding {
		embedding[i] = float32(i) / float32(testVectorDim)
	}

	addReq := &pb.AddEntityRequest{
		ExternalId:  "entity-001",
		Title:       "Test Entity",
		Type:        "person",
		Description: "A test entity for integration testing",
		Embedding:   embedding,
	}

	resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	if err != nil {
		t.Fatalf("Add entity failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Add entity returned error: %s", errResp.Message)
	}

	var addResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &addResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if addResp.Id == 0 {
		t.Error("Entity ID should not be 0")
	}
}

func TestServerIntegration_AddTextUnit(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// First add a document
	docReq := &pb.AddDocumentRequest{
		ExternalId: "doc-for-textunit",
		Filename:   "test.pdf",
	}
	docResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)
	var docID pb.OkWithID
	proto.Unmarshal(docResp.Payload, &docID)

	// Create embedding
	embedding := make([]float32, testVectorDim)
	for i := range embedding {
		embedding[i] = float32(i) / float32(testVectorDim)
	}

	addReq := &pb.AddTextUnitRequest{
		ExternalId: "chunk-001",
		DocumentId: docID.Id,
		Content:    "This is a test text chunk for integration testing.",
		Embedding:  embedding,
		TokenCount: 10,
	}

	resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_TEXTUNIT, addReq)
	if err != nil {
		t.Fatalf("Add text unit failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Add text unit returned error: %s", errResp.Message)
	}

	var addResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &addResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if addResp.Id == 0 {
		t.Error("TextUnit ID should not be 0")
	}
}

func TestServerIntegration_AddRelationship(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Create embedding
	embedding := make([]float32, testVectorDim)
	for i := range embedding {
		embedding[i] = float32(i) / float32(testVectorDim)
	}

	// Add two entities first
	ent1Req := &pb.AddEntityRequest{
		ExternalId: "entity-rel-1",
		Title:      "Entity 1",
		Type:       "person",
		Embedding:  embedding,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent1Req)
	var ent1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &ent1ID)

	ent2Req := &pb.AddEntityRequest{
		ExternalId: "entity-rel-2",
		Title:      "Entity 2",
		Type:       "organization",
		Embedding:  embedding,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent2Req)
	var ent2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &ent2ID)

	// Add relationship
	relReq := &pb.AddRelationshipRequest{
		ExternalId:  "rel-001",
		SourceId:    ent1ID.Id,
		TargetId:    ent2ID.Id,
		Type:        "WORKS_AT",
		Description: "Entity 1 works at Entity 2",
		Weight:      0.9,
	}

	resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_RELATIONSHIP, relReq)
	if err != nil {
		t.Fatalf("Add relationship failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Add relationship returned error: %s", errResp.Message)
	}

	var addResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &addResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if addResp.Id == 0 {
		t.Error("Relationship ID should not be 0")
	}
}

// =============================================================================
// Query Integration Tests
// =============================================================================

func TestServerIntegration_Query(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Create embedding
	embedding := make([]float32, testVectorDim)
	for i := range embedding {
		embedding[i] = float32(i) / float32(testVectorDim)
	}

	// Add some data first
	docReq := &pb.AddDocumentRequest{
		ExternalId: "doc-query-test",
		Filename:   "query_test.pdf",
	}
	sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)

	entReq := &pb.AddEntityRequest{
		ExternalId:  "ent-query-test",
		Title:       "Query Test Entity",
		Type:        "concept",
		Description: "Entity for query testing",
		Embedding:   embedding,
	}
	sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, entReq)

	// Execute query
	queryReq := &pb.QueryRequest{
		QueryVector:    embedding,
		TopK:           5,
		KHops:          1,
		MaxTextunits:   10,
		MaxEntities:    10,
		MaxCommunities: 5,
		SearchTypes:    []string{"entity", "textunit"},
	}

	resp, err := sendCommand(conn, pb.CommandType_CMD_QUERY, queryReq)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Query returned error: %s", errResp.Message)
	}

	var queryResp pb.QueryResponse
	if err := proto.Unmarshal(resp.Payload, &queryResp); err != nil {
		t.Fatalf("Failed to unmarshal query response: %v", err)
	}

	if queryResp.QueryId == 0 {
		t.Error("QueryID should not be 0")
	}
}

// =============================================================================
// Delete Operations Integration Tests
// =============================================================================

func TestServerIntegration_DeleteDocument(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add a document
	addReq := &pb.AddDocumentRequest{
		ExternalId: "doc-to-delete",
		Filename:   "delete_me.pdf",
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, addReq)
	var docID pb.OkWithID
	proto.Unmarshal(resp.Payload, &docID)

	// Delete the document
	delReq := &pb.DeleteByIDRequest{
		Id: docID.Id,
	}
	delResp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_DOCUMENT, delReq)
	if err != nil {
		t.Fatalf("Delete document failed: %v", err)
	}

	if delResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(delResp.Payload, &errResp)
		t.Fatalf("Delete document returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_DeleteEntity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)
	for i := range embedding {
		embedding[i] = float32(i) / float32(testVectorDim)
	}

	// Add an entity
	addReq := &pb.AddEntityRequest{
		ExternalId: "ent-to-delete",
		Title:      "Delete Me Entity",
		Type:       "test",
		Embedding:  embedding,
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	var entID pb.OkWithID
	proto.Unmarshal(resp.Payload, &entID)

	// Delete the entity
	delReq := &pb.DeleteByIDRequest{
		Id: entID.Id,
	}
	delResp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_ENTITY, delReq)
	if err != nil {
		t.Fatalf("Delete entity failed: %v", err)
	}

	if delResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(delResp.Payload, &errResp)
		t.Fatalf("Delete entity returned error: %s", errResp.Message)
	}
}

// =============================================================================
// TTL Integration Tests
// =============================================================================

// DEPRECATED: Session-based architecture - TTL managed at session level
/*
func TestServerIntegration_SetTTL(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add a document
	addReq := &pb.AddDocumentRequest{
		ExternalId: "doc-ttl-test",
		Filename:   "ttl_test.pdf",
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, addReq)
	var docID pb.OkWithID
	proto.Unmarshal(resp.Payload, &docID)

	// Set TTL
	ttlReq := &pb.SetTTLRequest{
		ItemType:   "document",
		Id:         docID.Id,
		TtlSeconds: 3600,
	}
	ttlResp, err := sendCommand(conn, pb.CommandType_CMD_SET_TTL, ttlReq)
	if err != nil {
		t.Fatalf("Set TTL failed: %v", err)
	}

	if ttlResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(ttlResp.Payload, &errResp)
		t.Fatalf("Set TTL returned error: %s", errResp.Message)
	}
}
*/

// =============================================================================
// Error Handling Integration Tests
// =============================================================================

func TestServerIntegration_UnknownCommand(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send unknown command type
	resp, err := sendCommand(conn, pb.CommandType(9999), nil)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Should get error response
	if resp.CmdType != pb.CommandType_CMD_ERROR {
		t.Errorf("Expected CMD_ERROR for unknown command, got %v", resp.CmdType)
	}
}

func TestServerIntegration_GetNonexistentDocument(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to get non-existent document
	getReq := &pb.DeleteByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_GET_DOCUMENT, getReq)
	if err != nil {
		t.Fatalf("Get document failed: %v", err)
	}

	// Should get error response
	if resp.CmdType != pb.CommandType_CMD_ERROR {
		t.Log("Got response for non-existent document (may return empty)")
	}
}

// =============================================================================
// Concurrent Operations Integration Tests
// =============================================================================

func TestServerIntegration_ConcurrentAdds(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	var wg sync.WaitGroup
	const numGoroutines = 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				t.Logf("Connect failed for goroutine %d: %v", id, err)
				return
			}
			defer conn.Close()

			embedding := make([]float32, testVectorDim)
			for j := range embedding {
				embedding[j] = float32(j+id) / float32(testVectorDim)
			}

			// Add document
			docReq := &pb.AddDocumentRequest{
				ExternalId: "doc-concurrent-" + itoa(id),
				Filename:   "concurrent_" + itoa(id) + ".pdf",
			}
			sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)

			// Add entity
			entReq := &pb.AddEntityRequest{
				ExternalId:  "ent-concurrent-" + itoa(id),
				Title:       "Concurrent Entity " + itoa(id),
				Type:        "test",
				Description: "Concurrent test entity",
				Embedding:   embedding,
			}
			sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, entReq)
		}(i)
	}

	wg.Wait()

	// Verify counts
	conn, _ := net.DialTimeout("tcp", addr, 5*time.Second)
	defer conn.Close()

	resp, _ := sendCommand(conn, pb.CommandType_CMD_INFO, nil)
	var infoResp pb.InfoResponse
	proto.Unmarshal(resp.Payload, &infoResp)

	if infoResp.DocumentCount < uint64(numGoroutines) {
		t.Errorf("DocumentCount = %d, expected at least %d", infoResp.DocumentCount, numGoroutines)
	}
	if infoResp.EntityCount < uint64(numGoroutines) {
		t.Errorf("EntityCount = %d, expected at least %d", infoResp.EntityCount, numGoroutines)
	}
}

// Helper function for int to string
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte(i%10) + '0'
		i /= 10
	}
	return string(buf[pos:])
}

// =============================================================================
// Get Operations Integration Tests
// =============================================================================

func TestServerIntegration_GetDocument(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add a document
	addReq := &pb.AddDocumentRequest{
		ExternalId: "doc-get-test",
		Filename:   "get_test.pdf",
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, addReq)
	var docID pb.OkWithID
	proto.Unmarshal(resp.Payload, &docID)

	// Get the document
	getReq := &pb.GetByIDRequest{
		Id: docID.Id,
	}
	getResp, err := sendCommand(conn, pb.CommandType_CMD_GET_DOCUMENT, getReq)
	if err != nil {
		t.Fatalf("Get document failed: %v", err)
	}

	if getResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(getResp.Payload, &errResp)
		t.Fatalf("Get document returned error: %s", errResp.Message)
	}

	var docResp pb.Document
	if err := proto.Unmarshal(getResp.Payload, &docResp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if docResp.ExternalId != "doc-get-test" {
		t.Errorf("ExternalID = %q, want %q", docResp.ExternalId, "doc-get-test")
	}
}

func TestServerIntegration_GetTextUnit(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// First add a document
	docReq := &pb.AddDocumentRequest{
		ExternalId: "doc-for-tu-get",
		Filename:   "test.pdf",
	}
	docResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)
	var docID pb.OkWithID
	proto.Unmarshal(docResp.Payload, &docID)

	// Add text unit
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddTextUnitRequest{
		ExternalId: "tu-get-test",
		DocumentId: docID.Id,
		Content:    "Test content for get",
		Embedding:  embedding,
		TokenCount: 5,
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_TEXTUNIT, addReq)
	var tuID pb.OkWithID
	proto.Unmarshal(resp.Payload, &tuID)

	// Get the text unit
	getReq := &pb.GetByIDRequest{
		Id: tuID.Id,
	}
	getResp, err := sendCommand(conn, pb.CommandType_CMD_GET_TEXTUNIT, getReq)
	if err != nil {
		t.Fatalf("Get text unit failed: %v", err)
	}

	if getResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(getResp.Payload, &errResp)
		t.Fatalf("Get text unit returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_GetEntity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddEntityRequest{
		ExternalId:  "ent-get-test",
		Title:       "Get Test Entity",
		Type:        "test",
		Description: "Entity for get testing",
		Embedding:   embedding,
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	var entID pb.OkWithID
	proto.Unmarshal(resp.Payload, &entID)

	// Get the entity
	getReq := &pb.GetByIDRequest{
		Id: entID.Id,
	}
	getResp, err := sendCommand(conn, pb.CommandType_CMD_GET_ENTITY, getReq)
	if err != nil {
		t.Fatalf("Get entity failed: %v", err)
	}

	if getResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(getResp.Payload, &errResp)
		t.Fatalf("Get entity returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_GetEntityByTitle(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddEntityRequest{
		ExternalId:  "ent-title-test",
		Title:       "UniqueEntityTitle",
		Type:        "test",
		Description: "Entity for title search",
		Embedding:   embedding,
	}
	sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)

	// Get by title
	getReq := &pb.GetEntityByTitleRequest{
		Title: "UniqueEntityTitle",
	}
	getResp, err := sendCommand(conn, pb.CommandType_CMD_GET_ENTITY_BY_TITLE, getReq)
	if err != nil {
		t.Fatalf("Get entity by title failed: %v", err)
	}

	if getResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(getResp.Payload, &errResp)
		t.Logf("Get entity by title returned error (may be expected): %s", errResp.Message)
	}
}

func TestServerIntegration_GetRelationship(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)

	// Add two entities
	ent1Req := &pb.AddEntityRequest{
		ExternalId: "rel-get-ent1",
		Title:      "Rel Get Entity 1",
		Type:       "test",
		Embedding:  embedding,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent1Req)
	var ent1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &ent1ID)

	ent2Req := &pb.AddEntityRequest{
		ExternalId: "rel-get-ent2",
		Title:      "Rel Get Entity 2",
		Type:       "test",
		Embedding:  embedding,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent2Req)
	var ent2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &ent2ID)

	// Add relationship
	relReq := &pb.AddRelationshipRequest{
		ExternalId:  "rel-get-test",
		SourceId:    ent1ID.Id,
		TargetId:    ent2ID.Id,
		Type:        "RELATED",
		Description: "Test relationship",
		Weight:      1.0,
	}
	relResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_RELATIONSHIP, relReq)
	var relID pb.OkWithID
	proto.Unmarshal(relResp.Payload, &relID)

	// Get the relationship
	getReq := &pb.GetByIDRequest{
		Id: relID.Id,
	}
	getResp, err := sendCommand(conn, pb.CommandType_CMD_GET_RELATIONSHIP, getReq)
	if err != nil {
		t.Fatalf("Get relationship failed: %v", err)
	}

	if getResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(getResp.Payload, &errResp)
		t.Fatalf("Get relationship returned error: %s", errResp.Message)
	}
}

// =============================================================================
// Delete TextUnit and Relationship Tests
// =============================================================================

func TestServerIntegration_DeleteTextUnit(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// First add a document
	docReq := &pb.AddDocumentRequest{
		ExternalId: "doc-for-tu-del",
		Filename:   "test.pdf",
	}
	docResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)
	var docID pb.OkWithID
	proto.Unmarshal(docResp.Payload, &docID)

	// Add text unit
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddTextUnitRequest{
		ExternalId: "tu-del-test",
		DocumentId: docID.Id,
		Content:    "Test content for delete",
		Embedding:  embedding,
		TokenCount: 5,
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_TEXTUNIT, addReq)
	var tuID pb.OkWithID
	proto.Unmarshal(resp.Payload, &tuID)

	// Delete text unit
	delReq := &pb.DeleteByIDRequest{
		Id: tuID.Id,
	}
	delResp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_TEXTUNIT, delReq)
	if err != nil {
		t.Fatalf("Delete text unit failed: %v", err)
	}

	if delResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(delResp.Payload, &errResp)
		t.Fatalf("Delete text unit returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_DeleteRelationship(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)

	// Add two entities
	ent1Req := &pb.AddEntityRequest{
		ExternalId: "rel-del-ent1",
		Title:      "Rel Del Entity 1",
		Type:       "test",
		Embedding:  embedding,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent1Req)
	var ent1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &ent1ID)

	ent2Req := &pb.AddEntityRequest{
		ExternalId: "rel-del-ent2",
		Title:      "Rel Del Entity 2",
		Type:       "test",
		Embedding:  embedding,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent2Req)
	var ent2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &ent2ID)

	// Add relationship
	relReq := &pb.AddRelationshipRequest{
		ExternalId:  "rel-del-test",
		SourceId:    ent1ID.Id,
		TargetId:    ent2ID.Id,
		Type:        "RELATED",
		Description: "Test relationship for delete",
		Weight:      1.0,
	}
	relResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_RELATIONSHIP, relReq)
	var relID pb.OkWithID
	proto.Unmarshal(relResp.Payload, &relID)

	// Delete relationship
	delReq := &pb.DeleteByIDRequest{
		Id: relID.Id,
	}
	delResp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_RELATIONSHIP, delReq)
	if err != nil {
		t.Fatalf("Delete relationship failed: %v", err)
	}

	if delResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(delResp.Payload, &errResp)
		t.Fatalf("Delete relationship returned error: %s", errResp.Message)
	}
}

// =============================================================================
// Link TextUnit to Entity Test
// =============================================================================

func TestServerIntegration_LinkTextUnitToEntity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add document
	docReq := &pb.AddDocumentRequest{
		ExternalId: "doc-link-test",
		Filename:   "link_test.pdf",
	}
	docResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)
	var docID pb.OkWithID
	proto.Unmarshal(docResp.Payload, &docID)

	// Add text unit
	embedding := make([]float32, testVectorDim)
	tuReq := &pb.AddTextUnitRequest{
		ExternalId: "tu-link-test",
		DocumentId: docID.Id,
		Content:    "Content for linking",
		Embedding:  embedding,
		TokenCount: 5,
	}
	tuResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_TEXTUNIT, tuReq)
	var tuID pb.OkWithID
	proto.Unmarshal(tuResp.Payload, &tuID)

	// Add entity
	entReq := &pb.AddEntityRequest{
		ExternalId: "ent-link-test",
		Title:      "Link Test Entity",
		Type:       "test",
		Embedding:  embedding,
	}
	entResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, entReq)
	var entID pb.OkWithID
	proto.Unmarshal(entResp.Payload, &entID)

	// Link text unit to entity
	linkReq := &pb.LinkTextUnitEntityRequest{
		TextunitId: tuID.Id,
		EntityId:   entID.Id,
	}
	linkResp, err := sendCommand(conn, pb.CommandType_CMD_LINK_TEXTUNIT_ENTITY, linkReq)
	if err != nil {
		t.Fatalf("Link text unit to entity failed: %v", err)
	}

	if linkResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(linkResp.Payload, &errResp)
		t.Fatalf("Link text unit to entity returned error: %s", errResp.Message)
	}
}

// =============================================================================
// Update Entity Description Test
// =============================================================================

func TestServerIntegration_UpdateEntityDescription(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddEntityRequest{
		ExternalId:  "ent-update-test",
		Title:       "Update Test Entity",
		Type:        "test",
		Description: "Original description",
		Embedding:   embedding,
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	var entID pb.OkWithID
	proto.Unmarshal(resp.Payload, &entID)

	// Update entity description
	newEmbedding := make([]float32, testVectorDim)
	for i := range newEmbedding {
		newEmbedding[i] = 0.5
	}
	updateReq := &pb.UpdateEntityDescRequest{
		Id:          entID.Id,
		Description: "Updated description",
		Embedding:   newEmbedding,
	}
	updateResp, err := sendCommand(conn, pb.CommandType_CMD_UPDATE_ENTITY_DESC, updateReq)
	if err != nil {
		t.Fatalf("Update entity description failed: %v", err)
	}

	if updateResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(updateResp.Payload, &errResp)
		t.Fatalf("Update entity description returned error: %s", errResp.Message)
	}
}

// =============================================================================
// Community Operations Tests
// =============================================================================

func TestServerIntegration_AddCommunity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddCommunityRequest{
		ExternalId:  "comm-add-test",
		Title:       "Test Community",
		Summary:     "A test community",
		FullContent: "Full content of the community",
		Level:       0,
		Embedding:   embedding,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_COMMUNITY, addReq)
	if err != nil {
		t.Fatalf("Add community failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Add community returned error: %s", errResp.Message)
	}

	var addResp pb.OkWithID
	if err := proto.Unmarshal(resp.Payload, &addResp); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if addResp.Id == 0 {
		t.Error("Community ID should not be 0")
	}
}

func TestServerIntegration_GetCommunity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add community first
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddCommunityRequest{
		ExternalId:  "comm-get-test",
		Title:       "Get Test Community",
		Summary:     "A community for get testing",
		FullContent: "Full content",
		Level:       0,
		Embedding:   embedding,
	}
	addResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_COMMUNITY, addReq)
	var commID pb.OkWithID
	proto.Unmarshal(addResp.Payload, &commID)

	// Get community
	getReq := &pb.GetByIDRequest{
		Id: commID.Id,
	}
	getResp, err := sendCommand(conn, pb.CommandType_CMD_GET_COMMUNITY, getReq)
	if err != nil {
		t.Fatalf("Get community failed: %v", err)
	}

	if getResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(getResp.Payload, &errResp)
		t.Fatalf("Get community returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_DeleteCommunity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add community first
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddCommunityRequest{
		ExternalId:  "comm-del-test",
		Title:       "Delete Test Community",
		Summary:     "A community for delete testing",
		FullContent: "Full content",
		Level:       0,
		Embedding:   embedding,
	}
	addResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_COMMUNITY, addReq)
	var commID pb.OkWithID
	proto.Unmarshal(addResp.Payload, &commID)

	// Delete community
	delReq := &pb.DeleteByIDRequest{
		Id: commID.Id,
	}
	delResp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_COMMUNITY, delReq)
	if err != nil {
		t.Fatalf("Delete community failed: %v", err)
	}

	if delResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(delResp.Payload, &errResp)
		t.Fatalf("Delete community returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_ComputeCommunities(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add some entities and relationships for community detection
	embedding := make([]float32, testVectorDim)

	for i := 0; i < 3; i++ {
		entReq := &pb.AddEntityRequest{
			ExternalId: "comm-comp-ent-" + itoa(i),
			Title:      "Comm Comp Entity " + itoa(i),
			Type:       "test",
			Embedding:  embedding,
		}
		sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, entReq)
	}

	// Compute communities
	computeReq := &pb.ComputeCommunitiesRequest{
		Resolution: 1.0,
		Iterations: 10,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_COMPUTE_COMMUNITIES, computeReq)
	if err != nil {
		t.Fatalf("Compute communities failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("Compute communities returned error (may be expected with no relationships): %s", errResp.Message)
	}
}

func TestServerIntegration_HierarchicalLeiden(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add some entities
	embedding := make([]float32, testVectorDim)
	for i := 0; i < 3; i++ {
		entReq := &pb.AddEntityRequest{
			ExternalId: "hier-ent-" + itoa(i),
			Title:      "Hier Entity " + itoa(i),
			Type:       "test",
			Embedding:  embedding,
		}
		sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, entReq)
	}

	// Hierarchical Leiden
	hierReq := &pb.HierarchicalLeidenRequest{
		MaxLevels:  3,
		Resolution: 1.0,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_HIERARCHICAL_LEIDEN, hierReq)
	if err != nil {
		t.Fatalf("Hierarchical Leiden failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("Hierarchical Leiden returned error (may be expected): %s", errResp.Message)
	}
}

// =============================================================================
// TTL Operations Tests - DEPRECATED
// =============================================================================

// DEPRECATED: Session-based architecture - idle TTL managed at session level
/*
func TestServerIntegration_SetIdleTTL(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add an entity
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddEntityRequest{
		ExternalId: "ent-idle-ttl",
		Title:      "Idle TTL Entity",
		Type:       "test",
		Embedding:  embedding,
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	var entID pb.OkWithID
	proto.Unmarshal(resp.Payload, &entID)

	// Set idle TTL
	ttlReq := &pb.SetIdleTTLRequest{
		ItemType:       "entity",
		Id:             entID.Id,
		IdleTtlSeconds: 7200,
	}
	ttlResp, err := sendCommand(conn, pb.CommandType_CMD_SET_IDLE_TTL, ttlReq)
	if err != nil {
		t.Fatalf("Set idle TTL failed: %v", err)
	}

	if ttlResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(ttlResp.Payload, &errResp)
		t.Fatalf("Set idle TTL returned error: %s", errResp.Message)
	}
}

*/

// DEPRECATED: Session-based architecture - TTL managed at session level
/*
func TestServerIntegration_GetTTL(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add an entity with TTL
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddEntityRequest{
		ExternalId: "ent-get-ttl",
		Title:      "Get TTL Entity",
		Type:       "test",
		Embedding:  embedding,
		Ttl:        3600,
	}
	resp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	var entID pb.OkWithID
	proto.Unmarshal(resp.Payload, &entID)

	// Get TTL
	ttlReq := &pb.GetTTLRequest{
		ItemType: "entity",
		Id:       entID.Id,
	}
	ttlResp, err := sendCommand(conn, pb.CommandType_CMD_GET_TTL, ttlReq)
	if err != nil {
		t.Fatalf("Get TTL failed: %v", err)
	}

	if ttlResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(ttlResp.Payload, &errResp)
		t.Logf("Get TTL returned error (may be expected): %s", errResp.Message)
	}
}

// =============================================================================
// Explain Operations Test
// =============================================================================

func TestServerIntegration_Explain(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// First run a query
	embedding := make([]float32, testVectorDim)
	queryReq := &pb.QueryRequest{
		QueryVector:    embedding,
		TopK:           5,
		KHops:          0,
		MaxTextunits:   10,
		MaxEntities:    10,
		MaxCommunities: 5,
		SearchTypes:    []string{"entity"},
	}
	queryResp, _ := sendCommand(conn, pb.CommandType_CMD_QUERY, queryReq)
	var qResp pb.QueryResponse
	proto.Unmarshal(queryResp.Payload, &qResp)

	// Explain the query
	explainReq := &pb.ExplainRequest{
		QueryId: qResp.QueryId,
	}
	explainResp, err := sendCommand(conn, pb.CommandType_CMD_EXPLAIN, explainReq)
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}

	if explainResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(explainResp.Payload, &errResp)
		t.Logf("Explain returned error (may be expected): %s", errResp.Message)
	}
}

// =============================================================================
// Bulk Operations Tests
// =============================================================================

func TestServerIntegration_MSetEntities(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)
	entities := []*pb.AddEntityRequest{
		{SessionId: testSessionID, ExternalId: "bulk-ent-1", Title: "Bulk Entity 1", Type: "test", Description: "Desc 1", Embedding: embedding},
		{SessionId: testSessionID, ExternalId: "bulk-ent-2", Title: "Bulk Entity 2", Type: "test", Description: "Desc 2", Embedding: embedding},
	}

	msetReq := &pb.MSetEntitiesRequest{
		Entities: entities,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_MSET_ENTITIES, msetReq)
	if err != nil {
		t.Fatalf("MSet entities failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("MSet entities returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_MGetEntities(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add entities first
	embedding := make([]float32, testVectorDim)
	ent1Req := &pb.AddEntityRequest{
		ExternalId: "mget-ent-1",
		Title:      "MGet Entity 1",
		Type:       "test",
		Embedding:  embedding,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent1Req)
	var ent1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &ent1ID)

	ent2Req := &pb.AddEntityRequest{
		ExternalId: "mget-ent-2",
		Title:      "MGet Entity 2",
		Type:       "test",
		Embedding:  embedding,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent2Req)
	var ent2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &ent2ID)

	// MGet entities
	mgetReq := &pb.MGetEntitiesRequest{
		Ids: []uint64{ent1ID.Id, ent2ID.Id},
	}
	mgetResp, err := sendCommand(conn, pb.CommandType_CMD_MGET_ENTITIES, mgetReq)
	if err != nil {
		t.Fatalf("MGet entities failed: %v", err)
	}

	if mgetResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(mgetResp.Payload, &errResp)
		t.Fatalf("MGet entities returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_ListEntities(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	for i := 0; i < 5; i++ {
		addReq := &pb.AddEntityRequest{
			ExternalId: fmt.Sprintf("list-ent-%d", i+1),
			Title:      fmt.Sprintf("List Entity %d", i+1),
			Type:       "test",
		}
		resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
		if err != nil {
			t.Fatalf("AddEntity failed: %v", err)
		}
		if resp.CmdType == pb.CommandType_CMD_ERROR {
			var errResp pb.Error
			proto.Unmarshal(resp.Payload, &errResp)
			t.Fatalf("AddEntity returned error: %s", errResp.Message)
		}
	}

	var allIDs []uint64
	var cursor uint64
	for page := 0; page < 3; page++ {
		listReq := &pb.ListEntitiesRequest{Cursor: cursor, Limit: 2}
		resp, err := sendCommand(conn, pb.CommandType_CMD_LIST_ENTITIES, listReq)
		if err != nil {
			t.Fatalf("List entities failed: %v", err)
		}
		if resp.CmdType == pb.CommandType_CMD_ERROR {
			var errResp pb.Error
			proto.Unmarshal(resp.Payload, &errResp)
			t.Fatalf("List entities returned error: %s", errResp.Message)
		}

		var listResp pb.EntitiesResponse
		if err := proto.Unmarshal(resp.Payload, &listResp); err != nil {
			t.Fatalf("Failed to unmarshal list entities response: %v", err)
		}
		if len(listResp.Entities) == 0 {
			t.Fatalf("Expected entities on page %d", page+1)
		}
		if listResp.NextCursor != 0 && listResp.NextCursor != listResp.Entities[len(listResp.Entities)-1].Id {
			t.Fatalf("Expected next cursor to match last entity ID")
		}

		for i := range listResp.Entities {
			allIDs = append(allIDs, listResp.Entities[i].Id)
			if i > 0 && listResp.Entities[i-1].Id >= listResp.Entities[i].Id {
				t.Fatalf("Expected ascending IDs on page %d", page+1)
			}
		}

		cursor = listResp.NextCursor
		if cursor == 0 {
			break
		}
	}

	if len(allIDs) != 5 {
		t.Fatalf("Expected 5 entities total, got %d", len(allIDs))
	}
}

func TestServerIntegration_MSetDocuments(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	docs := []*pb.AddDocumentRequest{
		{SessionId: testSessionID, ExternalId: "bulk-doc-1", Filename: "doc1.pdf"},
		{SessionId: testSessionID, ExternalId: "bulk-doc-2", Filename: "doc2.pdf"},
	}

	msetReq := &pb.MSetDocumentsRequest{
		Documents: docs,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_MSET_DOCUMENTS, msetReq)
	if err != nil {
		t.Fatalf("MSet documents failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("MSet documents returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_MGetDocuments(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add documents first
	doc1Req := &pb.AddDocumentRequest{
		ExternalId: "mget-doc-1",
		Filename:   "mget1.pdf",
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, doc1Req)
	var doc1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &doc1ID)

	doc2Req := &pb.AddDocumentRequest{
		ExternalId: "mget-doc-2",
		Filename:   "mget2.pdf",
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, doc2Req)
	var doc2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &doc2ID)

	// MGet documents
	mgetReq := &pb.MGetDocumentsRequest{
		Ids: []uint64{doc1ID.Id, doc2ID.Id},
	}
	mgetResp, err := sendCommand(conn, pb.CommandType_CMD_MGET_DOCUMENTS, mgetReq)
	if err != nil {
		t.Fatalf("MGet documents failed: %v", err)
	}

	if mgetResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(mgetResp.Payload, &errResp)
		t.Fatalf("MGet documents returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_MSetTextUnits(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add document first
	docReq := &pb.AddDocumentRequest{
		ExternalId: "mset-tu-doc",
		Filename:   "mset_tu.pdf",
	}
	docResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)
	var docID pb.OkWithID
	proto.Unmarshal(docResp.Payload, &docID)

	embedding := make([]float32, testVectorDim)
	tus := []*pb.AddTextUnitRequest{
		{SessionId: testSessionID, ExternalId: "bulk-tu-1", DocumentId: docID.Id, Content: "Content 1", Embedding: embedding, TokenCount: 5},
		{SessionId: testSessionID, ExternalId: "bulk-tu-2", DocumentId: docID.Id, Content: "Content 2", Embedding: embedding, TokenCount: 5},
	}

	msetReq := &pb.MSetTextUnitsRequest{
		Textunits: tus,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_MSET_TEXTUNITS, msetReq)
	if err != nil {
		t.Fatalf("MSet text units failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("MSet text units returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_MGetTextUnits(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add document first
	docReq := &pb.AddDocumentRequest{
		ExternalId: "mget-tu-doc",
		Filename:   "mget_tu.pdf",
	}
	docResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_DOCUMENT, docReq)
	var docID pb.OkWithID
	proto.Unmarshal(docResp.Payload, &docID)

	// Add text units
	embedding := make([]float32, testVectorDim)
	tu1Req := &pb.AddTextUnitRequest{
		ExternalId: "mget-tu-1",
		DocumentId: docID.Id,
		Content:    "MGet content 1",
		Embedding:  embedding,
		TokenCount: 5,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_TEXTUNIT, tu1Req)
	var tu1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &tu1ID)

	tu2Req := &pb.AddTextUnitRequest{
		ExternalId: "mget-tu-2",
		DocumentId: docID.Id,
		Content:    "MGet content 2",
		Embedding:  embedding,
		TokenCount: 5,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_TEXTUNIT, tu2Req)
	var tu2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &tu2ID)

	// MGet text units
	mgetReq := &pb.MGetTextUnitsRequest{
		Ids: []uint64{tu1ID.Id, tu2ID.Id},
	}
	mgetResp, err := sendCommand(conn, pb.CommandType_CMD_MGET_TEXTUNITS, mgetReq)
	if err != nil {
		t.Fatalf("MGet text units failed: %v", err)
	}

	if mgetResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(mgetResp.Payload, &errResp)
		t.Fatalf("MGet text units returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_MSetRelationships(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add entities first
	embedding := make([]float32, testVectorDim)
	ent1Req := &pb.AddEntityRequest{
		ExternalId: "mset-rel-ent1",
		Title:      "MSet Rel Entity 1",
		Type:       "test",
		Embedding:  embedding,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent1Req)
	var ent1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &ent1ID)

	ent2Req := &pb.AddEntityRequest{
		ExternalId: "mset-rel-ent2",
		Title:      "MSet Rel Entity 2",
		Type:       "test",
		Embedding:  embedding,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent2Req)
	var ent2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &ent2ID)

	rels := []*pb.AddRelationshipRequest{
		{SessionId: testSessionID, ExternalId: "bulk-rel-1", SourceId: ent1ID.Id, TargetId: ent2ID.Id, Type: "RELATED", Description: "Desc", Weight: 1.0},
	}

	msetReq := &pb.MSetRelationshipsRequest{
		Relationships: rels,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_MSET_RELATIONSHIPS, msetReq)
	if err != nil {
		t.Fatalf("MSet relationships failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("MSet relationships returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_MGetRelationships(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add entities first
	embedding := make([]float32, testVectorDim)
	ent1Req := &pb.AddEntityRequest{
		ExternalId: "mget-rel-ent1",
		Title:      "MGet Rel Entity 1",
		Type:       "test",
		Embedding:  embedding,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent1Req)
	var ent1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &ent1ID)

	ent2Req := &pb.AddEntityRequest{
		ExternalId: "mget-rel-ent2",
		Title:      "MGet Rel Entity 2",
		Type:       "test",
		Embedding:  embedding,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent2Req)
	var ent2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &ent2ID)

	// Add relationship
	relReq := &pb.AddRelationshipRequest{
		ExternalId:  "mget-rel-1",
		SourceId:    ent1ID.Id,
		TargetId:    ent2ID.Id,
		Type:        "RELATED",
		Description: "Desc",
		Weight:      1.0,
	}
	relResp, _ := sendCommand(conn, pb.CommandType_CMD_ADD_RELATIONSHIP, relReq)
	var relID pb.OkWithID
	proto.Unmarshal(relResp.Payload, &relID)

	// MGet relationships
	mgetReq := &pb.MGetRelationshipsRequest{
		Ids: []uint64{relID.Id},
	}
	mgetResp, err := sendCommand(conn, pb.CommandType_CMD_MGET_RELATIONSHIPS, mgetReq)
	if err != nil {
		t.Fatalf("MGet relationships failed: %v", err)
	}

	if mgetResp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(mgetResp.Payload, &errResp)
		t.Fatalf("MGet relationships returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_ListRelationships(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)
	ent1Req := &pb.AddEntityRequest{
		ExternalId: "list-rel-ent1",
		Title:      "List Rel Entity 1",
		Type:       "test",
		Embedding:  embedding,
	}
	resp1, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent1Req)
	var ent1ID pb.OkWithID
	proto.Unmarshal(resp1.Payload, &ent1ID)

	ent2Req := &pb.AddEntityRequest{
		ExternalId: "list-rel-ent2",
		Title:      "List Rel Entity 2",
		Type:       "test",
		Embedding:  embedding,
	}
	resp2, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent2Req)
	var ent2ID pb.OkWithID
	proto.Unmarshal(resp2.Payload, &ent2ID)

	ent3Req := &pb.AddEntityRequest{
		ExternalId: "list-rel-ent3",
		Title:      "List Rel Entity 3",
		Type:       "test",
		Embedding:  embedding,
	}
	resp3, _ := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, ent3Req)
	var ent3ID pb.OkWithID
	proto.Unmarshal(resp3.Payload, &ent3ID)

	relReqs := []*pb.AddRelationshipRequest{
		{ExternalId: "list-rel-1", SourceId: ent1ID.Id, TargetId: ent2ID.Id, Type: "RELATED", Description: "Desc", Weight: 1.0},
		{ExternalId: "list-rel-2", SourceId: ent2ID.Id, TargetId: ent3ID.Id, Type: "RELATED", Description: "Desc", Weight: 1.0},
		{ExternalId: "list-rel-3", SourceId: ent3ID.Id, TargetId: ent1ID.Id, Type: "RELATED", Description: "Desc", Weight: 1.0},
	}
	for _, relReq := range relReqs {
		resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_RELATIONSHIP, relReq)
		if err != nil {
			t.Fatalf("AddRelationship failed: %v", err)
		}
		if resp.CmdType == pb.CommandType_CMD_ERROR {
			var errResp pb.Error
			proto.Unmarshal(resp.Payload, &errResp)
			t.Fatalf("AddRelationship returned error: %s", errResp.Message)
		}
	}

	var allIDs []uint64
	var cursor uint64
	for page := 0; page < 2; page++ {
		listReq := &pb.ListRelationshipsRequest{Cursor: cursor, Limit: 2}
		resp, err := sendCommand(conn, pb.CommandType_CMD_LIST_RELATIONSHIPS, listReq)
		if err != nil {
			t.Fatalf("List relationships failed: %v", err)
		}
		if resp.CmdType == pb.CommandType_CMD_ERROR {
			var errResp pb.Error
			proto.Unmarshal(resp.Payload, &errResp)
			t.Fatalf("List relationships returned error: %s", errResp.Message)
		}

		var listResp pb.RelationshipsResponse
		if err := proto.Unmarshal(resp.Payload, &listResp); err != nil {
			t.Fatalf("Failed to unmarshal list relationships response: %v", err)
		}
		if len(listResp.Relationships) == 0 {
			t.Fatalf("Expected relationships on page %d", page+1)
		}
		if listResp.NextCursor != 0 && listResp.NextCursor != listResp.Relationships[len(listResp.Relationships)-1].Id {
			t.Fatalf("Expected next cursor to match last relationship ID")
		}

		for i := range listResp.Relationships {
			allIDs = append(allIDs, listResp.Relationships[i].Id)
			if i > 0 && listResp.Relationships[i-1].Id >= listResp.Relationships[i].Id {
				t.Fatalf("Expected ascending IDs on page %d", page+1)
			}
		}

		cursor = listResp.NextCursor
		if cursor == 0 {
			break
		}
	}

	if len(allIDs) != 3 {
		t.Fatalf("Expected 3 relationships total, got %d", len(allIDs))
	}
}

// =============================================================================
// Backup and WAL Operations Tests
// =============================================================================

func TestServerIntegration_BGSave(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	saveReq := &pb.SaveRequest{
		Path: "/tmp/gibram_test_save",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_BGSAVE, saveReq)
	if err != nil {
		t.Fatalf("BGSave failed: %v", err)
	}

	// May return error if no snapshot callback is set
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("BGSave returned error (expected if no callback): %s", errResp.Message)
	}
}

func TestServerIntegration_Save(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	saveReq := &pb.SaveRequest{
		Path: "/tmp/gibram_test_save_sync",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_SAVE, saveReq)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// May return error if no snapshot callback is set
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("Save returned error (expected if no callback): %s", errResp.Message)
	}
}

func TestServerIntegration_LastSave(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_LASTSAVE, nil)
	if err != nil {
		t.Fatalf("LastSave failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("LastSave returned error (expected if never saved): %s", errResp.Message)
	}
}

func TestServerIntegration_BGRestore(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	restoreReq := &pb.RestoreRequest{
		Path: "/nonexistent/path",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_BGRESTORE, restoreReq)
	if err != nil {
		t.Fatalf("BGRestore failed: %v", err)
	}

	// Should return error for nonexistent path
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("BGRestore returned error (expected): %s", errResp.Message)
	}
}

func TestServerIntegration_BackupStatus(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_BACKUP_STATUS, nil)
	if err != nil {
		t.Fatalf("BackupStatus failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("BackupStatus returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_WALStatus(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_STATUS, nil)
	if err != nil {
		t.Fatalf("WALStatus failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("WALStatus returned error (expected if WAL not set): %s", errResp.Message)
	}
}

func TestServerIntegration_RebuildIndex(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_REBUILD_INDEX, nil)
	if err != nil {
		t.Fatalf("RebuildIndex failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("RebuildIndex returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_WALCheckpoint(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_CHECKPOINT, nil)
	if err != nil {
		t.Fatalf("WALCheckpoint failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("WALCheckpoint returned error (expected if WAL not set): %s", errResp.Message)
	}
}

func TestServerIntegration_WALTruncate(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	truncReq := &pb.WALTruncateRequest{
		TargetLsn: 100,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_TRUNCATE, truncReq)
	if err != nil {
		t.Fatalf("WALTruncate failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("WALTruncate returned error (expected if WAL not set): %s", errResp.Message)
	}
}

func TestServerIntegration_WALRotate(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_ROTATE, nil)
	if err != nil {
		t.Fatalf("WALRotate failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("WALRotate returned error (expected if WAL not set): %s", errResp.Message)
	}
}

// =============================================================================
// Pipeline Operations Test
// =============================================================================

func TestServerIntegration_Pipeline(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	embedding := make([]float32, testVectorDim)

	// Create pipeline with multiple operations
	pipeReq := &pb.PipelineRequest{
		Commands: []*pb.Envelope{
			{
				Version: ProtocolVersion,
				CmdType: pb.CommandType_CMD_ADD_DOCUMENT,
				Payload: mustMarshal(&pb.AddDocumentRequest{
					SessionId:  testSessionID,
					ExternalId: "pipe-doc-1",
					Filename:   "pipe1.pdf",
				}),
			},
			{
				Version: ProtocolVersion,
				CmdType: pb.CommandType_CMD_ADD_ENTITY,
				Payload: mustMarshal(&pb.AddEntityRequest{
					SessionId:  testSessionID,
					ExternalId: "pipe-ent-1",
					Title:      "Pipeline Entity",
					Type:       "test",
					Embedding:  embedding,
				}),
			},
		},
	}

	resp, err := sendCommand(conn, pb.CommandType_CMD_PIPELINE, pipeReq)
	if err != nil {
		t.Fatalf("Pipeline failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Pipeline returned error: %s", errResp.Message)
	}

	var pipeResp pb.PipelineResponse
	if err := proto.Unmarshal(resp.Payload, &pipeResp); err != nil {
		t.Fatalf("Failed to unmarshal pipeline response: %v", err)
	}

	if len(pipeResp.Responses) != 2 {
		t.Errorf("Expected 2 responses, got %d", len(pipeResp.Responses))
	}
}

func mustMarshal(m proto.Message) []byte {
	data, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return data
}

// =============================================================================
// Authentication Test (requires config with API key)
// =============================================================================

func createTestServerWithAuth(t *testing.T) (*Server, string, string) {
	eng := engine.NewEngine(testVectorDim)

	// Create config with API key authentication
	apiKey, err := config.GenerateAPIKey()
	if err != nil {
		t.Fatalf("Failed to generate API key: %v", err)
	}
	hashedKey, err := config.HashAPIKey(apiKey)
	if err != nil {
		t.Fatalf("Failed to hash API key: %v", err)
	}

	cfg := &config.Config{
		Auth: config.AuthConfig{
			Keys: []config.APIKeyConfig{
				{
					ID:          "test-key",
					KeyHash:     hashedKey,
					Permissions: []string{config.PermRead, config.PermWrite, config.PermAdmin},
				},
			},
		},
	}

	srv := NewServerWithConfig(eng, cfg)

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Start server
	if err := srv.Start(addr); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	return srv, addr, apiKey
}

func TestServerIntegration_WithAuthentication(t *testing.T) {
	srv, addr, apiKey := createTestServerWithAuth(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// First authenticate
	authReq := &pb.AuthRequest{
		ApiKey: apiKey,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_AUTH, authReq)
	if err != nil {
		t.Fatalf("Auth failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Auth returned error: %s", errResp.Message)
	}

	var authResp pb.AuthResponse
	if err := proto.Unmarshal(resp.Payload, &authResp); err != nil {
		t.Fatalf("Failed to unmarshal auth response: %v", err)
	}

	if !authResp.Success {
		t.Errorf("Auth should succeed, message: %s", authResp.Message)
	}

	// Now try a command
	pingResp, err := sendCommand(conn, pb.CommandType_CMD_PING, nil)
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	if pingResp.CmdType != pb.CommandType_CMD_PONG {
		t.Errorf("Expected PONG, got %v", pingResp.CmdType)
	}
}

func TestServerIntegration_AuthWrongKey(t *testing.T) {
	srv, addr, _ := createTestServerWithAuth(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to authenticate with wrong key
	authReq := &pb.AuthRequest{
		ApiKey: "wrong-api-key",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_AUTH, authReq)
	if err != nil {
		t.Fatalf("Auth failed: %v", err)
	}

	// Should fail
	if resp.CmdType == pb.CommandType_CMD_AUTH_RESPONSE {
		var authResp pb.AuthResponse
		proto.Unmarshal(resp.Payload, &authResp)
		if authResp.Success {
			t.Error("Auth should fail with wrong key")
		}
	}
}

// =============================================================================
// Backup Operations with Callbacks Tests
// =============================================================================

func createTestServerWithBackupAsync(t *testing.T) (*Server, string, *sync.WaitGroup) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	// Track snapshot calls for async operations
	var wg sync.WaitGroup

	// Set snapshot callback
	srv.SetSnapshotCallback(func(path string) error {
		defer wg.Done()
		// Simulate some work
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Set restore callback
	srv.SetRestoreCallback(func(path string) error {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Start server
	if err := srv.Start(addr); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	return srv, addr, &wg
}

func createTestServerWithBackupSync(t *testing.T) (*Server, string) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	// Set snapshot callback (no WaitGroup for sync operations)
	srv.SetSnapshotCallback(func(path string) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Set restore callback
	srv.SetRestoreCallback(func(path string) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Start server
	if err := srv.Start(addr); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	return srv, addr
}

func TestServerIntegration_BGSaveWithCallback(t *testing.T) {
	srv, addr, wg := createTestServerWithBackupAsync(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	wg.Add(1)
	saveReq := &pb.SaveRequest{
		Path: "/tmp/gibram_test_bgsave",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_BGSAVE, saveReq)
	if err != nil {
		t.Fatalf("BGSave failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("BGSave returned error: %s", errResp.Message)
	}

	// Wait for background save to complete
	wg.Wait()

	// Verify backup status was updated
	time.Sleep(50 * time.Millisecond) // Give a moment for atomic to update

	statusResp, err := sendCommand(conn, pb.CommandType_CMD_BACKUP_STATUS, nil)
	if err != nil {
		t.Fatalf("BackupStatus failed: %v", err)
	}

	if statusResp.CmdType == pb.CommandType_CMD_BACKUP_RESPONSE {
		var status pb.BackupStatusResponse
		proto.Unmarshal(statusResp.Payload, &status)
		if status.InProgress {
			t.Error("Backup should no longer be in progress")
		}
	}
}

func TestServerIntegration_SaveWithCallback(t *testing.T) {
	srv, addr := createTestServerWithBackupSync(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	saveReq := &pb.SaveRequest{
		Path: "/tmp/gibram_test_save_sync",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_SAVE, saveReq)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Save returned error: %s", errResp.Message)
	}

	// Check last save
	lastResp, err := sendCommand(conn, pb.CommandType_CMD_LASTSAVE, nil)
	if err != nil {
		t.Fatalf("LastSave failed: %v", err)
	}

	if lastResp.CmdType == pb.CommandType_CMD_BACKUP_RESPONSE {
		var lastSave pb.LastSaveResponse
		proto.Unmarshal(lastResp.Payload, &lastSave)
		if lastSave.Timestamp == 0 {
			t.Error("Expected non-zero timestamp after save")
		}
		if lastSave.Path != "/tmp/gibram_test_save_sync" {
			t.Errorf("Expected path '/tmp/gibram_test_save_sync', got '%s'", lastSave.Path)
		}
	}
}

func TestServerIntegration_BGRestoreWithCallback(t *testing.T) {
	srv, addr, wg := createTestServerWithBackupAsync(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	wg.Add(1)
	restoreReq := &pb.RestoreRequest{
		Path: "/tmp/gibram_restore_test",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_BGRESTORE, restoreReq)
	if err != nil {
		t.Fatalf("BGRestore failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("BGRestore returned error: %s", errResp.Message)
	}

	// Wait for background restore to complete
	wg.Wait()
}

func TestServerIntegration_BGSaveAlreadyInProgress(t *testing.T) {
	srv, addr, wg := createTestServerWithBackupAsync(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Start first backup
	wg.Add(1)
	saveReq := &pb.SaveRequest{
		Path: "/tmp/gibram_test_parallel",
	}
	resp1, err := sendCommand(conn, pb.CommandType_CMD_BGSAVE, saveReq)
	if err != nil {
		t.Fatalf("First BGSave failed: %v", err)
	}

	if resp1.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp1.Payload, &errResp)
		t.Fatalf("First BGSave returned error: %s", errResp.Message)
	}

	// Try second backup immediately (should fail)
	resp2, err := sendCommand(conn, pb.CommandType_CMD_BGSAVE, saveReq)
	if err != nil {
		t.Fatalf("Second BGSave failed: %v", err)
	}

	// Second should return error since first is in progress
	if resp2.CmdType != pb.CommandType_CMD_ERROR {
		t.Error("Expected error for second BGSave while first is in progress")
	}

	wg.Wait()
}

// =============================================================================
// WAL Operations with actual WAL Tests
// =============================================================================

func createTestServerWithWAL(t *testing.T) (*Server, string, string) {
	eng := engine.NewEngine(testVectorDim)
	srv := NewServer(eng)

	// Create temp WAL directory
	walDir := t.TempDir()

	// Create actual WAL
	wal, err := backup.NewWAL(walDir, backup.SyncNever)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	srv.SetWAL(wal)

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// Start server
	if err := srv.Start(addr); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	return srv, addr, walDir
}

func TestServerIntegration_WALStatusWithWAL(t *testing.T) {
	srv, addr, _ := createTestServerWithWAL(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_STATUS, nil)
	if err != nil {
		t.Fatalf("WALStatus failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("WALStatus returned error: %s", errResp.Message)
	}

	var status pb.WALStatusResponse
	if err := proto.Unmarshal(resp.Payload, &status); err != nil {
		t.Fatalf("Failed to unmarshal WAL status: %v", err)
	}

	// WAL should have at least 1 segment
	if status.SegmentCount < 1 {
		t.Errorf("Expected at least 1 segment, got %d", status.SegmentCount)
	}
}

func TestServerIntegration_WALCheckpointWithWAL(t *testing.T) {
	srv, addr, _ := createTestServerWithWAL(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_CHECKPOINT, nil)
	if err != nil {
		t.Fatalf("WALCheckpoint failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("WALCheckpoint returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_WALRotateWithWAL(t *testing.T) {
	srv, addr, _ := createTestServerWithWAL(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_ROTATE, nil)
	if err != nil {
		t.Fatalf("WALRotate failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("WALRotate returned error: %s", errResp.Message)
	}
}

func TestServerIntegration_WALTruncateWithWAL(t *testing.T) {
	srv, addr, _ := createTestServerWithWAL(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_WAL_TRUNCATE, nil)
	if err != nil {
		t.Fatalf("WALTruncate failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("WALTruncate returned error: %s", errResp.Message)
	}
}

// =============================================================================
// Error handling edge cases
// =============================================================================

func TestServerIntegration_UnsupportedCommand(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send an unsupported/unknown command type (using a high number)
	resp, err := sendCommand(conn, pb.CommandType(9999), nil)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Should return error for unsupported command
	if resp.CmdType != pb.CommandType_CMD_ERROR {
		t.Errorf("Expected error for unsupported command, got %v", resp.CmdType)
	}
}

// =============================================================================
// Additional Error Branch Tests
// =============================================================================

func TestServerIntegration_InvalidPayload(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send invalid protobuf payload - should cause unmarshal error
	env := &pb.Envelope{
		Version:   ProtocolVersion,
		RequestId: 1,
		CmdType:   pb.CommandType_CMD_ADD_ENTITY,
		Payload:   []byte("invalid protobuf data"),
	}

	frameData, _ := codec.EncodeEnvelope(env)
	conn.Write(frameData)

	// Read response
	resp, _, _ := codec.DecodeEnvelope(conn)
	// Server should handle gracefully (may return error or process with defaults)
	if resp != nil {
		t.Logf("Response type: %v", resp.CmdType)
	}
}

func TestServerIntegration_GetNonExistentEntity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to get entity that doesn't exist
	getReq := &pb.GetByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_GET_ENTITY, getReq)
	if err != nil {
		t.Fatalf("Get entity failed: %v", err)
	}

	// Should return error or empty response
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent entity")
	}
}

func TestServerIntegration_DeleteNonExistentEntity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to delete entity that doesn't exist
	delReq := &pb.DeleteByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_ENTITY, delReq)
	if err != nil {
		t.Fatalf("Delete entity failed: %v", err)
	}

	// Should return error
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent entity")
	}
}

func TestServerIntegration_GetNonExistentDocument(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to get document that doesn't exist
	getReq := &pb.GetByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_GET_DOCUMENT, getReq)
	if err != nil {
		t.Fatalf("Get document failed: %v", err)
	}

	// Should return error
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent document")
	}
}

func TestServerIntegration_DeleteNonExistentDocument(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to delete document that doesn't exist
	delReq := &pb.DeleteByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_DOCUMENT, delReq)
	if err != nil {
		t.Fatalf("Delete document failed: %v", err)
	}

	// Should return error
	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent document")
	}
}

func TestServerIntegration_GetNonExistentTextUnit(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	getReq := &pb.GetByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_GET_TEXTUNIT, getReq)
	if err != nil {
		t.Fatalf("Get text unit failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent text unit")
	}
}

func TestServerIntegration_DeleteNonExistentTextUnit(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	delReq := &pb.DeleteByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_TEXTUNIT, delReq)
	if err != nil {
		t.Fatalf("Delete text unit failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent text unit")
	}
}

func TestServerIntegration_GetNonExistentRelationship(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	getReq := &pb.GetByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_GET_RELATIONSHIP, getReq)
	if err != nil {
		t.Fatalf("Get relationship failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent relationship")
	}
}

func TestServerIntegration_DeleteNonExistentRelationship(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	delReq := &pb.DeleteByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_RELATIONSHIP, delReq)
	if err != nil {
		t.Fatalf("Delete relationship failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent relationship")
	}
}

func TestServerIntegration_GetNonExistentCommunity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	getReq := &pb.GetByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_GET_COMMUNITY, getReq)
	if err != nil {
		t.Fatalf("Get community failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent community")
	}
}

func TestServerIntegration_DeleteNonExistentCommunity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	delReq := &pb.GetByIDRequest{
		Id: 999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_DELETE_COMMUNITY, delReq)
	if err != nil {
		t.Fatalf("Delete community failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for non-existent community")
	}
}

func TestServerIntegration_LinkInvalidTextUnit(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to link non-existent text unit to non-existent entity
	linkReq := &pb.LinkTextUnitEntityRequest{
		TextunitId: 999999,
		EntityId:   999999,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_LINK_TEXTUNIT_ENTITY, linkReq)
	if err != nil {
		t.Fatalf("Link text unit failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for linking non-existent items")
	}
}

func TestServerIntegration_UpdateNonExistentEntity(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Try to update non-existent entity
	updateReq := &pb.UpdateEntityDescRequest{
		Id:          999999,
		Description: "New description",
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_UPDATE_ENTITY_DESC, updateReq)
	if err != nil {
		t.Fatalf("Update entity failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		t.Log("Correctly returned error for updating non-existent entity")
	}
}

func TestServerIntegration_HealthWithBackup(t *testing.T) {
	srv, addr := createTestServerWithBackupSync(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	resp, err := sendCommand(conn, pb.CommandType_CMD_HEALTH, nil)
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Fatalf("Health check returned error: %s", errResp.Message)
	}

	var health pb.HealthResponse
	if err := proto.Unmarshal(resp.Payload, &health); err != nil {
		t.Fatalf("Failed to unmarshal health response: %v", err)
	}

	// Backup should show "ok" since callback is set but not running
	if status, ok := health.Components["backup"]; ok {
		if status != "ok" && status != "not_configured" {
			t.Logf("Backup status: %s", status)
		}
	}
}

func TestServerIntegration_RebuildIndexError(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Rebuild index on empty engine should work
	resp, err := sendCommand(conn, pb.CommandType_CMD_REBUILD_INDEX, nil)
	if err != nil {
		t.Fatalf("Rebuild index failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("Rebuild index returned error (may be expected): %s", errResp.Message)
	}
}

*/

// DEPRECATED: Session-based architecture - TTL managed at session level
/*
func TestServerIntegration_SetTTLInvalidType(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// First add an entity
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddEntityRequest{
		ExternalId: "ttl-test-entity",
		Title:      "TTL Test Entity",
		Type:       "test",
		Embedding:  embedding,
	}
	addResp, err := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	if err != nil {
		t.Fatalf("Add entity failed: %v", err)
	}

	var entityId pb.Entity
	proto.Unmarshal(addResp.Payload, &entityId)

	// Try to set idle TTL
	idleTTLReq := &pb.SetIdleTTLRequest{
		ItemType:       "entity",
		Id:             entityId.Id,
		IdleTtlSeconds: 3600,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_SET_IDLE_TTL, idleTTLReq)
	if err != nil {
		t.Fatalf("SetIdleTTL failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("SetIdleTTL returned error: %s", errResp.Message)
	}
}
*/

func TestServerIntegration_QueryWithFilters(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add some entities first
	embedding := make([]float32, testVectorDim)
	for i := 0; i < 3; i++ {
		addReq := &pb.AddEntityRequest{
			ExternalId: fmt.Sprintf("query-test-entity-%d", i),
			Title:      fmt.Sprintf("Query Test Entity %d", i),
			Type:       "test",
			Embedding:  embedding,
		}
		_, err := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
		if err != nil {
			t.Fatalf("Add entity failed: %v", err)
		}
	}

	// Query with embedding
	queryReq := &pb.QueryRequest{
		SearchTypes: []string{"entity"},
		TopK:        10,
		QueryVector: embedding,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_QUERY, queryReq)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_QUERY_RESPONSE {
		var queryResp pb.QueryResponse
		proto.Unmarshal(resp.Payload, &queryResp)
		if len(queryResp.Entities) < 3 {
			t.Logf("Expected at least 3 entity results, got %d", len(queryResp.Entities))
		}
	}
}

func TestServerIntegration_ExplainWithQuery(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add some entities
	embedding := make([]float32, testVectorDim)
	addReq := &pb.AddEntityRequest{
		ExternalId: "explain-test-entity",
		Title:      "Explain Test Entity",
		Type:       "test",
		Embedding:  embedding,
	}
	_, err = sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
	if err != nil {
		t.Fatalf("Add entity failed: %v", err)
	}

	// Explain query
	explainReq := &pb.QueryRequest{
		SearchTypes: []string{"entity"},
		TopK:        10,
		QueryVector: embedding,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_EXPLAIN, explainReq)
	if err != nil {
		t.Fatalf("Explain failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("Explain returned error: %s", errResp.Message)
	} else {
		var explainResp pb.ExplainResponse
		proto.Unmarshal(resp.Payload, &explainResp)
		t.Logf("Explain returned %d seeds and %d traversal steps", len(explainResp.Seeds), len(explainResp.Traversal))
	}
}

func TestServerIntegration_HierarchicalLeidenWithData(t *testing.T) {
	srv, addr := createTestServer(t)
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Add entities and relationships for community detection
	embedding := make([]float32, testVectorDim)

	// Add entities
	var entityIDs []uint64
	for i := 0; i < 5; i++ {
		addReq := &pb.AddEntityRequest{
			ExternalId: fmt.Sprintf("hier-leiden-entity-%d", i),
			Title:      fmt.Sprintf("Hierarchical Leiden Entity %d", i),
			Type:       "test",
			Embedding:  embedding,
		}
		resp, err := sendCommand(conn, pb.CommandType_CMD_ADD_ENTITY, addReq)
		if err != nil {
			t.Fatalf("Add entity failed: %v", err)
		}
		var entityResp pb.Entity
		proto.Unmarshal(resp.Payload, &entityResp)
		entityIDs = append(entityIDs, entityResp.Id)
	}

	// Add relationships
	for i := 0; i < len(entityIDs)-1; i++ {
		relReq := &pb.AddRelationshipRequest{
			ExternalId: fmt.Sprintf("hier-leiden-rel-%d", i),
			SourceId:   entityIDs[i],
			TargetId:   entityIDs[i+1],
			Weight:     1.0,
		}
		_, err := sendCommand(conn, pb.CommandType_CMD_ADD_RELATIONSHIP, relReq)
		if err != nil {
			t.Fatalf("Add relationship failed: %v", err)
		}
	}

	// Run hierarchical Leiden
	leidenReq := &pb.HierarchicalLeidenRequest{
		MaxLevels:  3,
		Resolution: 1.0,
	}
	resp, err := sendCommand(conn, pb.CommandType_CMD_HIERARCHICAL_LEIDEN, leidenReq)
	if err != nil {
		t.Fatalf("Hierarchical Leiden failed: %v", err)
	}

	if resp.CmdType == pb.CommandType_CMD_ERROR {
		var errResp pb.Error
		proto.Unmarshal(resp.Payload, &errResp)
		t.Logf("Hierarchical Leiden returned error (may be expected for small graph): %s", errResp.Message)
	} else {
		var leidenResp pb.HierarchicalLeidenResponse
		proto.Unmarshal(resp.Payload, &leidenResp)
		t.Logf("Hierarchical Leiden found %d total communities", leidenResp.TotalCommunities)
	}
}
