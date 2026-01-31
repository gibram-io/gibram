// Package client - comprehensive tests for client usage scenarios
package client

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gibram-io/gibram/pkg/codec"
	"github.com/gibram-io/gibram/pkg/config"
	"github.com/gibram-io/gibram/pkg/engine"
	"github.com/gibram-io/gibram/pkg/server"
	"github.com/gibram-io/gibram/pkg/types"
)

// =============================================================================
// Test Infrastructure
// =============================================================================

type testServer struct {
	srv  *server.Server
	addr string
}

func closeClient(tb testing.TB, c *Client) {
	tb.Helper()
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		tb.Logf("Close error: %v", err)
	}
}

func startTestServer(t *testing.T) *testServer {
	eng := engine.NewEngine(64)
	srv := server.NewServer(eng)

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("Failed to close listener: %v", err)
	}

	if err := srv.Start(addr); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	return &testServer{srv: srv, addr: addr}
}

func startTestServerWithAuth(t *testing.T) (*testServer, string) {
	eng := engine.NewEngine(64)

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

	srv := server.NewServerWithConfig(eng, cfg)

	// Find available port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("Failed to close listener: %v", err)
	}

	if err := srv.Start(addr); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	return &testServer{srv: srv, addr: addr}, apiKey
}

func (ts *testServer) Stop() {
	if ts.srv != nil {
		ts.srv.Stop()
	}
}

// =============================================================================
// Connection Pool Tests
// =============================================================================

func TestConnPool_Create(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	pool, err := NewConnPool(ts.addr, DefaultPoolConfig())
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	active, available := pool.Stats()
	if active != 1 {
		t.Errorf("Expected 1 active connection from warmup, got %d", active)
	}
	t.Logf("Pool stats: active=%d, available=%d", active, available)
}

func TestConnPool_ConfigDefaults(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	// Create pool with zero config (should use defaults)
	cfg := PoolConfig{}
	pool, err := NewConnPool(ts.addr, cfg)
	if err != nil {
		t.Fatalf("Failed to create pool with zero config: %v", err)
	}
	defer pool.Close()
}

func TestConnPool_Stats(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	pool, err := NewConnPool(ts.addr, DefaultPoolConfig())
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	active, available := pool.Stats()
	// Warmup creates 1 connection and returns it to available
	if active < 0 || active > DefaultPoolSize {
		t.Errorf("Active connections out of range: %d", active)
	}
	if available < 0 {
		t.Errorf("Available connections should not be negative: %d", available)
	}
}

func TestConnPool_Close(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	pool, err := NewConnPool(ts.addr, DefaultPoolConfig())
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	// Close pool
	pool.Close()

	// Double close should not panic
	pool.Close()
}

func TestConnPool_ClosedPool(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	pool, err := NewConnPool(ts.addr, DefaultPoolConfig())
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	pool.Close()

	// Operations on closed pool should fail
	// We can't call getConn directly, but closing sets the closed flag
}

func TestConnPool_InvalidAddress(t *testing.T) {
	cfg := DefaultPoolConfig()
	cfg.ConnTimeout = 100 * time.Millisecond

	_, err := NewConnPool("127.0.0.1:99999", cfg)
	if err == nil {
		t.Error("Expected error connecting to invalid address")
	}
}

// =============================================================================
// Client Creation Tests
// =============================================================================

func TestNewClient(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Verify client was created (cannot check internal addr)
	if client == nil {
		t.Error("Client should not be nil")
	}
}

func TestNewClientWithConfig(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	cfg := DefaultPoolConfig()
	cfg.MaxConnections = 5
	cfg.ConnTimeout = 2 * time.Second

	client, err := NewClientWithConfig(ts.addr, testSessionID, cfg)
	if err != nil {
		t.Fatalf("Failed to create client with config: %v", err)
	}
	defer closeClient(t, client)
}

func TestNewClient_InvalidAddress(t *testing.T) {
	_, err := NewClient("invalid:99999", testSessionID)
	if err == nil {
		t.Error("Expected error for invalid address")
	}
}

// =============================================================================
// Client Operation Tests - PING
// =============================================================================

func TestClient_Ping(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	if err := client.Ping(); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestClient_PingConcurrent(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	var wg sync.WaitGroup
	const numPings = 20
	errCh := make(chan error, numPings)

	for i := 0; i < numPings; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := client.Ping(); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("Ping failed: %v", err)
	}
}

// =============================================================================
// Client Operation Tests - Info
// =============================================================================

func TestClient_Info(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	info, err := client.Info()
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}

	if info.Version == "" {
		t.Error("Version should not be empty")
	}
}

// =============================================================================
// Client Operation Tests - Health
// =============================================================================

func TestClient_Health(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if health.Status == "" {
		t.Error("Health status should not be empty")
	}
}

// =============================================================================
// Client Operation Tests - Document CRUD
// =============================================================================

func TestClient_AddDocument(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docID, err := client.AddDocument("ext-doc-001", "test.pdf")
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	if docID == 0 {
		t.Error("Document ID should not be 0")
	}
}

func TestClient_GetDocument(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add document first
	docID, err := client.AddDocument("ext-doc-001", "test.pdf")
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	// Retrieve it
	doc, err := client.GetDocument(docID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}

	if doc == nil {
		t.Fatal("Document should not be nil")
	}

	if doc.ExternalID != "ext-doc-001" {
		t.Errorf("ExternalID = %q, want %q", doc.ExternalID, "ext-doc-001")
	}
}

func TestClient_GetDocument_NotFound(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	_, err = client.GetDocument(999999)
	if err == nil {
		t.Error("Expected error for non-existent document")
	}
}

// =============================================================================
// Client Operation Tests - Entity CRUD
// =============================================================================

func TestClient_AddEntity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	for i := range embedding {
		embedding[i] = float32(i) / 64.0
	}

	entityID, err := client.AddEntity("ext-ent-001", "Test Entity", "test", "Description", embedding)
	if err != nil {
		t.Fatalf("AddEntity failed: %v", err)
	}

	if entityID == 0 {
		t.Error("Entity ID should not be 0")
	}
}

func TestClient_GetEntity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	entityID := mustAddEntity(t, client, "ext-ent-001", "Test Entity", "test", "Description", embedding)

	entity, err := client.GetEntity(entityID)
	if err != nil {
		t.Fatalf("GetEntity failed: %v", err)
	}

	if entity == nil {
		t.Fatal("Entity should not be nil")
	}

	// Title should be normalized to uppercase
	if entity.Title != "TEST ENTITY" {
		t.Errorf("Title = %q, want %q", entity.Title, "TEST ENTITY")
	}
}

func TestClient_GetEntityByTitle(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	mustAddEntity(t, client, "ext-ent-001", "Unique Entity Title", "test", "Description", embedding)

	entity, err := client.GetEntityByTitle("unique entity title") // lowercase lookup
	if err != nil {
		t.Fatalf("GetEntityByTitle failed: %v", err)
	}

	if entity == nil {
		t.Fatal("Entity should not be nil")
	}
}

// =============================================================================
// Client Operation Tests - TextUnit CRUD
// =============================================================================

func TestClient_AddTextUnit(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// First add document
	docID := mustAddDocument(t, client, "doc-001", "test.pdf")

	embedding := make([]float32, 64)
	tuID, err := client.AddTextUnit("tu-001", docID, "This is test content", embedding, 5)
	if err != nil {
		t.Fatalf("AddTextUnit failed: %v", err)
	}

	if tuID == 0 {
		t.Error("TextUnit ID should not be 0")
	}
}

func TestClient_GetTextUnit(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docID := mustAddDocument(t, client, "doc-001", "test.pdf")
	embedding := make([]float32, 64)
	tuID := mustAddTextUnit(t, client, "tu-001", docID, "Test content", embedding, 5)

	tu, err := client.GetTextUnit(tuID)
	if err != nil {
		t.Fatalf("GetTextUnit failed: %v", err)
	}

	if tu == nil {
		t.Fatal("TextUnit should not be nil")
	}

	if tu.Content != "Test content" {
		t.Errorf("Content = %q, want %q", tu.Content, "Test content")
	}
}

// =============================================================================
// Client Operation Tests - Relationship CRUD
// =============================================================================

func TestClient_AddRelationship(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "ent-001", "Entity 1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "ent-002", "Entity 2", "test", "Desc", embedding)

	relID, err := client.AddRelationship("rel-001", ent1ID, ent2ID, "RELATED_TO", "Relationship description", 1.0)
	if err != nil {
		t.Fatalf("AddRelationship failed: %v", err)
	}

	if relID == 0 {
		t.Error("Relationship ID should not be 0")
	}
}

func TestClient_GetRelationship(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "ent-001", "Entity 1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "ent-002", "Entity 2", "test", "Desc", embedding)
	relID := mustAddRelationship(t, client, "rel-001", ent1ID, ent2ID, "RELATED_TO", "Desc", 1.0)

	rel, err := client.GetRelationship(relID)
	if err != nil {
		t.Fatalf("GetRelationship failed: %v", err)
	}

	if rel == nil {
		t.Fatal("Relationship should not be nil")
	}

	if rel.Type != "RELATED_TO" {
		t.Errorf("Type = %q, want %q", rel.Type, "RELATED_TO")
	}
}

// =============================================================================
// Client Operation Tests - Query
// =============================================================================

func TestClient_Query(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add some data
	embedding := make([]float32, 64)
	for i := range embedding {
		embedding[i] = float32(i) / 64.0
	}

	mustAddEntity(t, client, "ent-001", "Test Entity", "test", "Description", embedding)

	// Query using QuerySpec
	spec := types.QuerySpec{
		QueryVector:    embedding,
		TopK:           5,
		KHops:          0,
		MaxTextUnits:   10,
		MaxEntities:    10,
		MaxCommunities: 5,
		SearchTypes:    []types.SearchType{types.SearchTypeTextUnit, types.SearchTypeEntity},
	}
	result, err := client.Query(spec)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result == nil {
		t.Fatal("Query result should not be nil")
	}

	if result.QueryID == 0 {
		t.Error("QueryID should not be 0")
	}
}

// =============================================================================
// Client Operation Tests - TTL
// =============================================================================

func TestClient_SetTTL(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docID := mustAddDocument(t, client, "doc-001", "test.pdf")

	// DEPRECATED: Session-based architecture - TTL managed at session level
	_ = docID
	// err = client.SetTTL("document", docID, 3600)
	// if err != nil {
	//	t.Errorf("SetTTL failed: %v", err)
	// }
}

// DEPRECATED: Session-based architecture - TTL managed at session level
/*
func TestClient_GetTTL(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docID := mustAddDocument(t, client, "doc-001", "test.pdf")
	// client.SetTTL("document", docID, 3600)

	_ = docID
	// ttl, err := client.GetTTL("document", docID)
	// if err != nil {
	//	t.Fatalf("GetTTL failed: %v", err)
	// }

	// if ttl <= 0 {
	//	t.Error("TTL should be positive after setting")
	// }
}
*/

// =============================================================================
// Wire Protocol Tests
// =============================================================================

func TestCodecType(t *testing.T) {
	if codec.CodecProtobuf != 1 {
		t.Errorf("CodecProtobuf = %d, want 1", codec.CodecProtobuf)
	}
}

// =============================================================================
// Integration Tests - Full Workflow
// =============================================================================

func TestClient_FullWorkflow_ChatWithPDF(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// 1. Upload document
	docID, err := client.AddDocument("pdf-001", "research_paper.pdf")
	if err != nil {
		t.Fatalf("Failed to add document: %v", err)
	}
	t.Logf("Created document with ID: %d", docID)

	// 2. Add text chunks
	embedding := make([]float32, 64)
	for i := range embedding {
		embedding[i] = float32(i) / 64.0
	}

	tu1ID := mustAddTextUnit(t, client, "chunk-1", docID, "Machine learning is...", embedding, 10)
	tu2ID := mustAddTextUnit(t, client, "chunk-2", docID, "Neural networks are...", embedding, 10)
	t.Logf("Created text units: %d, %d", tu1ID, tu2ID)

	// 3. Add entities
	ent1ID := mustAddEntity(t, client, "ent-ml", "Machine Learning", "concept", "ML description", embedding)
	ent2ID := mustAddEntity(t, client, "ent-nn", "Neural Network", "concept", "NN description", embedding)
	t.Logf("Created entities: %d, %d", ent1ID, ent2ID)

	// 4. Add relationship
	relID := mustAddRelationship(t, client, "rel-1", ent1ID, ent2ID, "USES", "ML uses NNs", 0.9)
	t.Logf("Created relationship: %d", relID)

	// 5. Link text units to entities
	if err := client.LinkTextUnitToEntity(tu1ID, ent1ID); err != nil {
		t.Fatalf("LinkTextUnitToEntity failed: %v", err)
	}
	if err := client.LinkTextUnitToEntity(tu2ID, ent2ID); err != nil {
		t.Fatalf("LinkTextUnitToEntity failed: %v", err)
	}

	// 6. Query
	spec := types.QuerySpec{
		QueryVector:    embedding,
		TopK:           5,
		KHops:          1,
		MaxTextUnits:   10,
		MaxEntities:    10,
		MaxCommunities: 5,
		SearchTypes:    []types.SearchType{types.SearchTypeTextUnit, types.SearchTypeEntity},
	}
	result, err := client.Query(spec)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	t.Logf("Query returned %d text units, %d entities", len(result.TextUnits), len(result.Entities))

	// 7. Check info
	info, err := client.Info()
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}
	if info.DocumentCount != 1 {
		t.Errorf("DocumentCount = %d, want 1", info.DocumentCount)
	}
	if info.TextUnitCount != 2 {
		t.Errorf("TextUnitCount = %d, want 2", info.TextUnitCount)
	}
	if info.EntityCount != 2 {
		t.Errorf("EntityCount = %d, want 2", info.EntityCount)
	}
}

func TestClient_ConcurrentOperations(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	var wg sync.WaitGroup
	const numOps = 20
	errCh := make(chan error, numOps*3)

	// Concurrent document additions
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if _, err := client.AddDocument("doc-"+itoa(id), "file.pdf"); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent entity additions
	embedding := make([]float32, 64)
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if _, err := client.AddEntity("ent-"+itoa(id), "Entity "+itoa(id), "test", "Desc", embedding); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent queries
	for i := 0; i < numOps/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spec := types.QuerySpec{
				QueryVector:    embedding,
				TopK:           5,
				KHops:          0,
				MaxTextUnits:   10,
				MaxEntities:    10,
				MaxCommunities: 5,
				SearchTypes:    []types.SearchType{types.SearchTypeEntity},
			}
			if _, err := client.Query(spec); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("Concurrent query error: %v", err)
	}

	// Verify final state
	info, err := client.Info()
	if err != nil {
		t.Fatalf("Info failed: %v", err)
	}
	t.Logf("Final state: %d documents, %d entities", info.DocumentCount, info.EntityCount)
}

// Helper for number to string
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
// Client Operation Tests - Delete Operations
// =============================================================================

func TestClient_DeleteDocument(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add document first
	docID, err := client.AddDocument("doc-to-delete", "test.pdf")
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	// Delete it
	err = client.DeleteDocument(docID)
	if err != nil {
		t.Errorf("DeleteDocument failed: %v", err)
	}

	// Try to get the deleted document - should fail
	_, err = client.GetDocument(docID)
	if err == nil {
		t.Error("Expected error getting deleted document")
	}
}

func TestClient_DeleteTextUnit(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add document and text unit first
	docID := mustAddDocument(t, client, "doc-001", "test.pdf")
	embedding := make([]float32, 64)
	tuID, err := client.AddTextUnit("tu-to-delete", docID, "Content to delete", embedding, 5)
	if err != nil {
		t.Fatalf("AddTextUnit failed: %v", err)
	}

	// Delete it
	err = client.DeleteTextUnit(tuID)
	if err != nil {
		t.Errorf("DeleteTextUnit failed: %v", err)
	}

	// Try to get the deleted text unit - should fail
	_, err = client.GetTextUnit(tuID)
	if err == nil {
		t.Error("Expected error getting deleted text unit")
	}
}

func TestClient_DeleteEntity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add entity first
	embedding := make([]float32, 64)
	entityID, err := client.AddEntity("ent-to-delete", "Delete Test", "test", "Desc", embedding)
	if err != nil {
		t.Fatalf("AddEntity failed: %v", err)
	}

	// Delete it
	err = client.DeleteEntity(entityID)
	if err != nil {
		t.Errorf("DeleteEntity failed: %v", err)
	}

	// Try to get the deleted entity - should fail
	_, err = client.GetEntity(entityID)
	if err == nil {
		t.Error("Expected error getting deleted entity")
	}
}

func TestClient_DeleteRelationship(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add entities and relationship first
	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "ent-1", "Entity 1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "ent-2", "Entity 2", "test", "Desc", embedding)
	relID, err := client.AddRelationship("rel-to-delete", ent1ID, ent2ID, "RELATED", "Desc", 1.0)
	if err != nil {
		t.Fatalf("AddRelationship failed: %v", err)
	}

	// Delete it
	err = client.DeleteRelationship(relID)
	if err != nil {
		t.Errorf("DeleteRelationship failed: %v", err)
	}

	// Try to get the deleted relationship - should fail
	_, err = client.GetRelationship(relID)
	if err == nil {
		t.Error("Expected error getting deleted relationship")
	}
}

func TestClient_UpdateEntityDescription(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add entity first
	embedding := make([]float32, 64)
	entityID, err := client.AddEntity("ent-update", "Update Test", "test", "Original desc", embedding)
	if err != nil {
		t.Fatalf("AddEntity failed: %v", err)
	}

	// Update the description with new embedding
	newEmbedding := make([]float32, 64)
	for i := range newEmbedding {
		newEmbedding[i] = 0.5
	}
	err = client.UpdateEntityDescription(entityID, "Updated description", newEmbedding)
	if err != nil {
		t.Errorf("UpdateEntityDescription failed: %v", err)
	}

	// Verify the update
	entity, err := client.GetEntity(entityID)
	if err != nil {
		t.Fatalf("GetEntity failed: %v", err)
	}

	if entity.Description != "Updated description" {
		t.Errorf("Description = %q, want %q", entity.Description, "Updated description")
	}
}

// =============================================================================
// Client Operation Tests - Community Operations
// =============================================================================

func TestClient_AddCommunity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	communityID, err := client.AddCommunity("comm-001", "Test Community", "Summary of the community", "Full content", 0, nil, nil, embedding)
	if err != nil {
		t.Fatalf("AddCommunity failed: %v", err)
	}

	if communityID == 0 {
		t.Error("Community ID should not be 0")
	}
}

func TestClient_GetCommunity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	communityID, err := client.AddCommunity("comm-001", "Test Community", "Summary", "Full content", 0, nil, nil, embedding)
	if err != nil {
		t.Fatalf("AddCommunity failed: %v", err)
	}

	community, err := client.GetCommunity(communityID)
	if err != nil {
		t.Fatalf("GetCommunity failed: %v", err)
	}

	if community == nil {
		t.Fatal("Community should not be nil")
	}

	if community.Title != "Test Community" {
		t.Errorf("Title = %q, want %q", community.Title, "Test Community")
	}
}

func TestClient_DeleteCommunity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	communityID, err := client.AddCommunity("comm-del", "Delete Me", "Summary", "Full content", 0, nil, nil, embedding)
	if err != nil {
		t.Fatalf("AddCommunity failed: %v", err)
	}

	err = client.DeleteCommunity(communityID)
	if err != nil {
		t.Errorf("DeleteCommunity failed: %v", err)
	}

	// Try to get the deleted community - should fail
	_, err = client.GetCommunity(communityID)
	if err == nil {
		t.Error("Expected error getting deleted community")
	}
}

func TestClient_ComputeCommunities(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add some entities and relationships for community detection
	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "ent-a", "Entity A", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "ent-b", "Entity B", "test", "Desc", embedding)
	ent3ID := mustAddEntity(t, client, "ent-c", "Entity C", "test", "Desc", embedding)
	mustAddRelationship(t, client, "rel-ab", ent1ID, ent2ID, "RELATED", "Desc", 1.0)
	mustAddRelationship(t, client, "rel-bc", ent2ID, ent3ID, "RELATED", "Desc", 1.0)

	result, err := client.ComputeCommunities(1.0, 10)
	if err != nil {
		t.Fatalf("ComputeCommunities failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}

	t.Logf("Computed %d communities", result.Count)
}

func TestClient_HierarchicalLeiden(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add some entities and relationships for community detection
	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "ent-h1", "Entity H1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "ent-h2", "Entity H2", "test", "Desc", embedding)
	ent3ID := mustAddEntity(t, client, "ent-h3", "Entity H3", "test", "Desc", embedding)
	mustAddRelationship(t, client, "rel-h12", ent1ID, ent2ID, "RELATED", "Desc", 1.0)
	mustAddRelationship(t, client, "rel-h23", ent2ID, ent3ID, "RELATED", "Desc", 1.0)

	result, err := client.HierarchicalLeiden(3, 1.0)
	if err != nil {
		t.Fatalf("HierarchicalLeiden failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}

	t.Logf("Hierarchical Leiden found %d total communities", result.TotalCommunities)
}

func TestClient_HierarchicalLeidenDefault(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add some entities and relationships
	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "ent-d1", "Entity D1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "ent-d2", "Entity D2", "test", "Desc", embedding)
	mustAddRelationship(t, client, "rel-d12", ent1ID, ent2ID, "RELATED", "Desc", 1.0)

	result, err := client.HierarchicalLeidenDefault()
	if err != nil {
		t.Fatalf("HierarchicalLeidenDefault failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}
}

// =============================================================================
// Client Operation Tests - Pool Stats
// =============================================================================

func TestClient_PoolStats(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	active, available := client.PoolStats()
	if active < 0 {
		t.Errorf("Active connections should not be negative: %d", active)
	}
	t.Logf("Pool stats: active=%d, available=%d", active, available)
}

// =============================================================================
// Client Operation Tests - IdleTTL
// =============================================================================

func TestClient_SetIdleTTL(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add entity
	embedding := make([]float32, 64)
	entityID, err := client.AddEntity("ent-idle", "Idle Test", "test", "Desc", embedding)
	if err != nil {
		t.Fatalf("AddEntity failed: %v", err)
	}

	// DEPRECATED: Session-based architecture - idle TTL managed at session level
	_ = entityID
	// err = client.SetIdleTTL("entity", entityID, 7200)
	// if err != nil {
	//	t.Errorf("SetIdleTTL failed: %v", err)
	// }
}

// =============================================================================
// Client Operation Tests - Explain
// =============================================================================

func TestClient_Explain(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Add data first
	embedding := make([]float32, 64)
	for i := range embedding {
		embedding[i] = float32(i) / 64.0
	}
	mustAddEntity(t, client, "ent-exp", "Explain Entity", "test", "Desc", embedding)

	// First run a query
	spec := types.QuerySpec{
		QueryVector:    embedding,
		TopK:           5,
		KHops:          0,
		MaxTextUnits:   10,
		MaxEntities:    10,
		MaxCommunities: 5,
		SearchTypes:    []types.SearchType{types.SearchTypeEntity},
	}
	result, err := client.Query(spec)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Explain the query
	explanation, err := client.Explain(result.QueryID)
	if err != nil {
		t.Logf("Explain failed (may be expected): %v", err)
	} else {
		if explanation == nil {
			t.Error("Explanation should not be nil")
		}
	}
}

// =============================================================================
// Client Operation Tests - Bulk Operations (MSet/MGet)
// =============================================================================

func TestClient_MSetEntities(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	entities := []types.BulkEntityInput{
		{ExternalID: "bulk-ent-1", Title: "Bulk Entity 1", Type: "test", Description: "Desc 1", Embedding: embedding},
		{ExternalID: "bulk-ent-2", Title: "Bulk Entity 2", Type: "test", Description: "Desc 2", Embedding: embedding},
		{ExternalID: "bulk-ent-3", Title: "Bulk Entity 3", Type: "test", Description: "Desc 3", Embedding: embedding},
	}

	ids, err := client.MSetEntities(entities)
	if err != nil {
		t.Fatalf("MSetEntities failed: %v", err)
	}

	if len(ids) != 3 {
		t.Errorf("Expected 3 IDs, got %d", len(ids))
	}
}

func TestClient_MGetEntities(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "mget-ent-1", "MGet Entity 1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "mget-ent-2", "MGet Entity 2", "test", "Desc", embedding)

	entities, err := client.MGetEntities([]uint64{ent1ID, ent2ID})
	if err != nil {
		t.Fatalf("MGetEntities failed: %v", err)
	}

	if len(entities) != 2 {
		t.Errorf("Expected 2 entities, got %d", len(entities))
	}
}

func TestClient_MSetDocuments(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docs := []types.BulkDocumentInput{
		{ExternalID: "bulk-doc-1", Filename: "file1.pdf"},
		{ExternalID: "bulk-doc-2", Filename: "file2.pdf"},
	}

	ids, err := client.MSetDocuments(docs)
	if err != nil {
		t.Fatalf("MSetDocuments failed: %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("Expected 2 IDs, got %d", len(ids))
	}
}

func TestClient_MGetDocuments(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	doc1ID := mustAddDocument(t, client, "mget-doc-1", "file1.pdf")
	doc2ID := mustAddDocument(t, client, "mget-doc-2", "file2.pdf")

	docs, err := client.MGetDocuments([]uint64{doc1ID, doc2ID})
	if err != nil {
		t.Fatalf("MGetDocuments failed: %v", err)
	}

	if len(docs) != 2 {
		t.Errorf("Expected 2 documents, got %d", len(docs))
	}
}

func TestClient_MSetTextUnits(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docID := mustAddDocument(t, client, "mset-tu-doc", "test.pdf")
	embedding := make([]float32, 64)

	tus := []types.BulkTextUnitInput{
		{ExternalID: "bulk-tu-1", DocumentID: docID, Content: "Content 1", Embedding: embedding, TokenCount: 5},
		{ExternalID: "bulk-tu-2", DocumentID: docID, Content: "Content 2", Embedding: embedding, TokenCount: 5},
	}

	ids, err := client.MSetTextUnits(tus)
	if err != nil {
		t.Fatalf("MSetTextUnits failed: %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("Expected 2 IDs, got %d", len(ids))
	}
}

func TestClient_MGetTextUnits(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docID := mustAddDocument(t, client, "mget-tu-doc", "test.pdf")
	embedding := make([]float32, 64)
	tu1ID := mustAddTextUnit(t, client, "mget-tu-1", docID, "Content 1", embedding, 5)
	tu2ID := mustAddTextUnit(t, client, "mget-tu-2", docID, "Content 2", embedding, 5)

	tus, err := client.MGetTextUnits([]uint64{tu1ID, tu2ID})
	if err != nil {
		t.Fatalf("MGetTextUnits failed: %v", err)
	}

	if len(tus) != 2 {
		t.Errorf("Expected 2 text units, got %d", len(tus))
	}
}

func TestClient_MSetRelationships(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "mset-rel-1", "Entity 1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "mset-rel-2", "Entity 2", "test", "Desc", embedding)
	ent3ID := mustAddEntity(t, client, "mset-rel-3", "Entity 3", "test", "Desc", embedding)

	rels := []types.BulkRelationshipInput{
		{ExternalID: "bulk-rel-1", SourceID: ent1ID, TargetID: ent2ID, Type: "RELATED", Description: "Desc", Weight: 1.0},
		{ExternalID: "bulk-rel-2", SourceID: ent2ID, TargetID: ent3ID, Type: "RELATED", Description: "Desc", Weight: 1.0},
	}

	ids, err := client.MSetRelationships(rels)
	if err != nil {
		t.Fatalf("MSetRelationships failed: %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("Expected 2 IDs, got %d", len(ids))
	}
}

func TestClient_MGetRelationships(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	embedding := make([]float32, 64)
	ent1ID := mustAddEntity(t, client, "mget-rel-1", "Entity 1", "test", "Desc", embedding)
	ent2ID := mustAddEntity(t, client, "mget-rel-2", "Entity 2", "test", "Desc", embedding)
	rel1ID := mustAddRelationship(t, client, "mget-rel-a", ent1ID, ent2ID, "TYPE_A", "Desc", 1.0)
	rel2ID := mustAddRelationship(t, client, "mget-rel-b", ent2ID, ent1ID, "TYPE_B", "Desc", 1.0)

	rels, err := client.MGetRelationships([]uint64{rel1ID, rel2ID})
	if err != nil {
		t.Fatalf("MGetRelationships failed: %v", err)
	}

	if len(rels) != 2 {
		t.Errorf("Expected 2 relationships, got %d", len(rels))
	}
}

// =============================================================================
// Client Operation Tests - Backup Operations
// =============================================================================

func TestClient_BGSave(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// BGSave starts a background save to a path
	err = client.BGSave("/tmp/gibram_test_save")
	if err != nil {
		t.Logf("BGSave failed (may be expected if datadir not configured): %v", err)
	}
}

func TestClient_Save(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Save performs a blocking save to a path
	err = client.Save("/tmp/gibram_test_save")
	if err != nil {
		t.Logf("Save failed (may be expected if datadir not configured): %v", err)
	}
}

func TestClient_LastSave(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	info, err := client.LastSave()
	if err != nil {
		t.Logf("LastSave failed (may be expected if never saved): %v", err)
	} else {
		t.Logf("Last save at %d", info.Timestamp)
	}
}

func TestClient_BGRestore(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// BGRestore tries to restore from a path
	err = client.BGRestore("/nonexistent/path")
	if err != nil {
		t.Logf("BGRestore failed (expected for nonexistent path): %v", err)
	}
}

func TestClient_BackupStatus(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	status, err := client.BackupStatus()
	if err != nil {
		t.Logf("BackupStatus failed (may be expected): %v", err)
	} else {
		t.Logf("Backup status: in_progress=%v, type=%s", status.InProgress, status.Type)
	}
}

// =============================================================================
// Client Operation Tests - Connection Pool Edge Cases
// =============================================================================

func TestConnPool_MultipleClients(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	// Create multiple clients to test pool management
	cfg := DefaultPoolConfig()
	cfg.MaxConnections = 3

	pool, err := NewConnPool(ts.addr, cfg)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	// Perform multiple concurrent operations to test pool usage
	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Use pool via Client
			client := &Client{pool: pool}
			if err := client.Ping(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("Ping failed: %v", err)
	}

	active, available := pool.Stats()
	t.Logf("After concurrent ops: active=%d, available=%d", active, available)
}

func TestConnPool_IdleConnectionCleanup(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	// Create pool with short idle timeout
	cfg := DefaultPoolConfig()
	cfg.IdleTimeout = 100 * time.Millisecond

	pool, err := NewConnPool(ts.addr, cfg)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	// Do something to create a connection
	client := &Client{pool: pool}
	if err := client.Ping(); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// Wait for idle connection to expire
	time.Sleep(150 * time.Millisecond)

	active, _ := pool.Stats()
	t.Logf("After idle wait: active=%d", active)
}

func TestClient_WithTLS(t *testing.T) {
	// Test TLS config creation (without actual TLS server)
	cfg := DefaultPoolConfig()
	cfg.TLSEnabled = true
	cfg.TLSSkipVerify = true
	cfg.ConnTimeout = 100 * time.Millisecond

	// This will fail to connect but tests the TLS path
	_, err := NewConnPool("127.0.0.1:59999", cfg)
	if err == nil {
		t.Error("Expected error connecting to invalid TLS endpoint")
	}
}

func TestClient_WithAPIKey(t *testing.T) {
	// Test API key config (requires server with auth)
	cfg := DefaultPoolConfig()
	cfg.APIKey = "test-api-key"
	cfg.ConnTimeout = 100 * time.Millisecond

	// This will fail but tests the API key config path
	_, err := NewConnPool("127.0.0.1:59998", cfg)
	if err == nil {
		t.Error("Expected error connecting to invalid endpoint")
	}
}

// =============================================================================
// Wire Protocol Tests
// =============================================================================

func TestWriteEnvelope(t *testing.T) {
	// Test envelope serialization - this is internal but affects coverage
	// We can test it indirectly through client operations
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// This exercises writeEnvelope and readEnvelope
	if err := client.Ping(); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func TestClient_ErrorHandling_InvalidEntity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Try to get non-existent entity
	_, err = client.GetEntity(999999999)
	if err == nil {
		t.Error("Expected error for non-existent entity")
	}
}

func TestClient_ErrorHandling_InvalidRelationship(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Try to get non-existent relationship
	_, err = client.GetRelationship(999999999)
	if err == nil {
		t.Error("Expected error for non-existent relationship")
	}
}

func TestClient_ErrorHandling_InvalidTextUnit(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Try to get non-existent text unit
	_, err = client.GetTextUnit(999999999)
	if err == nil {
		t.Error("Expected error for non-existent text unit")
	}
}

func TestClient_ErrorHandling_InvalidCommunity(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Try to get non-existent community
	_, err = client.GetCommunity(999999999)
	if err == nil {
		t.Error("Expected error for non-existent community")
	}
}

func TestClient_MGetEntities_EmptyIDs(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	// Get with empty IDs
	entities, err := client.MGetEntities([]uint64{})
	if err != nil {
		t.Logf("MGetEntities with empty IDs: %v", err)
	}
	t.Logf("Returned %d entities for empty IDs", len(entities))
}

func TestClient_MGetDocuments_EmptyIDs(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	docs, err := client.MGetDocuments([]uint64{})
	if err != nil {
		t.Logf("MGetDocuments with empty IDs: %v", err)
	}
	t.Logf("Returned %d documents for empty IDs", len(docs))
}

// =============================================================================
// Additional Entity Operations
// =============================================================================

func TestClient_GetEntityByTitle_NotFound(t *testing.T) {
	ts := startTestServer(t)
	defer ts.Stop()

	client, err := NewClient(ts.addr, testSessionID)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer closeClient(t, client)

	_, err = client.GetEntityByTitle("nonexistent entity title")
	if err == nil {
		t.Error("Expected error for non-existent entity title")
	}
}

// =============================================================================
// Authentication Tests
// =============================================================================

func TestClient_WithAuthentication(t *testing.T) {
	ts, apiKey := startTestServerWithAuth(t)
	defer ts.Stop()

	// Create client with API key
	cfg := DefaultPoolConfig()
	cfg.APIKey = apiKey

	client, err := NewClientWithConfig(ts.addr, testSessionID, cfg)
	if err != nil {
		t.Fatalf("Failed to create authenticated client: %v", err)
	}
	defer closeClient(t, client)

	// Verify client can operate
	if err := client.Ping(); err != nil {
		t.Errorf("Ping failed: %v", err)
	}

	// Get info
	info, err := client.Info()
	if err != nil {
		t.Errorf("Info failed: %v", err)
	}
	if info.Version == "" {
		t.Error("Version should not be empty")
	}
}

func TestClient_WithWrongAPIKey(t *testing.T) {
	ts, _ := startTestServerWithAuth(t)
	defer ts.Stop()

	// Create client with wrong API key
	cfg := DefaultPoolConfig()
	cfg.APIKey = "wrong-api-key"
	cfg.ConnTimeout = 1 * time.Second

	_, err := NewClientWithConfig(ts.addr, testSessionID, cfg)
	if err == nil {
		t.Error("Expected error for wrong API key")
	}
}

func TestClient_NoAPIKeyOnAuthServer(t *testing.T) {
	ts, _ := startTestServerWithAuth(t)
	defer ts.Stop()

	// Create client without API key on auth-required server
	cfg := DefaultPoolConfig()
	cfg.ConnTimeout = 1 * time.Second

	client, err := NewClientWithConfig(ts.addr, testSessionID, cfg)
	if err != nil {
		// Some servers may reject immediately
		t.Logf("Connection rejected (expected): %v", err)
		return
	}
	defer closeClient(t, client)

	// Try an operation - should fail without auth
	err = client.Ping()
	if err != nil {
		t.Logf("Ping failed without auth (may be expected): %v", err)
	}
}

func TestClient_AuthenticatedOperations(t *testing.T) {
	ts, apiKey := startTestServerWithAuth(t)
	defer ts.Stop()

	cfg := DefaultPoolConfig()
	cfg.APIKey = apiKey

	client, err := NewClientWithConfig(ts.addr, testSessionID, cfg)
	if err != nil {
		t.Fatalf("Failed to create authenticated client: %v", err)
	}
	defer closeClient(t, client)

	// Test add document
	docID, err := client.AddDocument("auth-doc-001", "test.pdf")
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	// Test get document
	doc, err := client.GetDocument(docID)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	if doc.ExternalID != "auth-doc-001" {
		t.Errorf("ExternalID = %q, want %q", doc.ExternalID, "auth-doc-001")
	}

	// Test add entity
	embedding := make([]float32, 64)
	entityID, err := client.AddEntity("auth-ent-001", "Auth Entity", "test", "Desc", embedding)
	if err != nil {
		t.Fatalf("AddEntity failed: %v", err)
	}

	// Test get entity
	entity, err := client.GetEntity(entityID)
	if err != nil {
		t.Fatalf("GetEntity failed: %v", err)
	}
	if entity.Title != "AUTH ENTITY" {
		t.Errorf("Title = %q, want %q", entity.Title, "AUTH ENTITY")
	}

	// Test health
	health, err := client.Health()
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if health.Status == "" {
		t.Error("Health status should not be empty")
	}
}
