// Package engine provides the query engine tests
package engine

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gibram-io/gibram/pkg/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

const testVectorDim = 64

func randomVector(dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(i%10) / 10.0
	}
	return v
}

func createTestEngine() *Engine {
	return NewEngine(testVectorDim)
}

const testSessionID = "test-session-1"

// =============================================================================
// Engine Creation Tests
// =============================================================================

func TestNewEngine(t *testing.T) {
	e := NewEngine(128)

	if e == nil {
		t.Fatal("NewEngine returned nil")
	}

	if e.vectorDim != 128 {
		t.Errorf("Expected vectorDim 128, got %d", e.vectorDim)
	}

	// Verify session-based architecture
	if e.sessions == nil {
		t.Error("sessions map is nil")
	}
}

// =============================================================================
// Document Operations Tests
// =============================================================================

func TestEngine_AddDocument(t *testing.T) {
	e := createTestEngine()

	doc, err := e.AddDocument(testSessionID, "ext-doc-1", "test.txt")
	if err != nil {
		t.Fatalf("AddDocument failed: %v", err)
	}

	if doc.ID == 0 {
		t.Error("Document ID should not be 0")
	}
	if doc.ExternalID != "ext-doc-1" {
		t.Errorf("Expected ExternalID 'ext-doc-1', got '%s'", doc.ExternalID)
	}
	if doc.Filename != "test.txt" {
		t.Errorf("Expected Filename 'test.txt', got '%s'", doc.Filename)
	}
	if doc.Status != types.DocStatusUploaded {
		t.Errorf("Expected status Uploaded, got %s", doc.Status)
	}
}

func TestEngine_AddDocument_Duplicate(t *testing.T) {
	e := createTestEngine()

	_, err := e.AddDocument(testSessionID, "ext-doc-1", "test.txt")
	if err != nil {
		t.Fatalf("First AddDocument failed: %v", err)
	}

	_, err = e.AddDocument(testSessionID, "ext-doc-1", "test2.txt")
	if err == nil {
		t.Error("Duplicate AddDocument should fail")
	}
}

func TestEngine_GetDocument(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")

	retrieved, ok := e.GetDocument(testSessionID, doc.ID)
	if !ok {
		t.Error("GetDocument should return true")
	}
	if retrieved.ExternalID != "ext-doc-1" {
		t.Errorf("Expected ExternalID 'ext-doc-1', got '%s'", retrieved.ExternalID)
	}
}

func TestEngine_GetDocument_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.GetDocument(testSessionID, 99999)
	if ok {
		t.Error("GetDocument should return false for non-existent document")
	}
}

func TestEngine_UpdateDocumentStatus(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")

	success := e.UpdateDocumentStatus(testSessionID, doc.ID, types.DocStatusReady)
	if !success {
		t.Error("UpdateDocumentStatus should return true")
	}

	retrieved, _ := e.GetDocument(testSessionID, doc.ID)
	if retrieved.Status != types.DocStatusReady {
		t.Errorf("Expected status Ready, got %s", retrieved.Status)
	}
}

func TestEngine_UpdateDocumentStatus_NotFound(t *testing.T) {
	e := createTestEngine()

	success := e.UpdateDocumentStatus(testSessionID, 99999, types.DocStatusReady)
	if success {
		t.Error("UpdateDocumentStatus should return false for non-existent document")
	}
}

// =============================================================================
// TextUnit Operations Tests
// =============================================================================

func TestEngine_AddTextUnit(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")

	embedding := randomVector(testVectorDim)
	tu := mustAddTextUnit(t, e, testSessionID, "ext-tu-1", doc.ID, "Test content", embedding, 10)

	if tu.ID == 0 {
		t.Error("TextUnit ID should not be 0")
	}
	if tu.ExternalID != "ext-tu-1" {
		t.Errorf("Expected ExternalID 'ext-tu-1', got '%s'", tu.ExternalID)
	}
	if tu.DocumentID != doc.ID {
		t.Errorf("Expected DocumentID %d, got %d", doc.ID, tu.DocumentID)
	}
	if tu.Content != "Test content" {
		t.Errorf("Expected content 'Test content', got '%s'", tu.Content)
	}
}

func TestEngine_AddTextUnit_Duplicate(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")

	embedding := randomVector(testVectorDim)
	_, err := e.AddTextUnit(testSessionID, "ext-tu-1", doc.ID, "Content 1", embedding, 10)
	if err != nil {
		t.Fatalf("First AddTextUnit failed: %v", err)
	}

	_, err = e.AddTextUnit(testSessionID, "ext-tu-1", doc.ID, "Content 2", embedding, 10)
	if err == nil {
		t.Error("Duplicate AddTextUnit should fail")
	}
}

func TestEngine_GetTextUnit(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	embedding := randomVector(testVectorDim)
	tu := mustAddTextUnit(t, e, testSessionID, "ext-tu-1", doc.ID, "Test content", embedding, 10)

	retrieved, ok := e.GetTextUnit(testSessionID, tu.ID)
	if !ok {
		t.Error("GetTextUnit should return true")
	}
	if retrieved.Content != "Test content" {
		t.Errorf("Expected content 'Test content', got '%s'", retrieved.Content)
	}
}

// =============================================================================
// Entity Operations Tests
// =============================================================================

func TestEngine_AddEntity(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	ent, err := e.AddEntity(testSessionID, "ext-ent-1", "Bank Indonesia", "organization", "Central bank", embedding)
	if err != nil {
		t.Fatalf("AddEntity failed: %v", err)
	}

	if ent.ID == 0 {
		t.Error("Entity ID should not be 0")
	}
	if ent.ExternalID != "ext-ent-1" {
		t.Errorf("Expected ExternalID 'ext-ent-1', got '%s'", ent.ExternalID)
	}
}

func TestEngine_GetEntity(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	ent := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Bank Indonesia", "organization", "Central bank", embedding)

	retrieved, ok := e.GetEntity(testSessionID, ent.ID)
	if !ok {
		t.Error("GetEntity should return true")
	}
	// Title is normalized to uppercase
	if retrieved.Title != "BANK INDONESIA" {
		t.Errorf("Expected title 'BANK INDONESIA', got '%s'", retrieved.Title)
	}
}

func TestEngine_GetEntityByTitle(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	mustAddEntity(t, e, testSessionID, "ext-ent-1", "Bank Indonesia", "organization", "Central bank", embedding)

	// Should find with different case
	retrieved, ok := e.GetEntityByTitle(testSessionID, "bank indonesia")
	if !ok {
		t.Error("GetEntityByTitle should return true")
	}
	if retrieved.Title != "BANK INDONESIA" {
		t.Errorf("Expected title 'BANK INDONESIA', got '%s'", retrieved.Title)
	}
}

func TestEngine_UpdateEntityDescription(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	ent := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Bank Indonesia", "organization", "Central bank", embedding)

	newEmbedding := randomVector(testVectorDim)
	success := e.UpdateEntityDescription(testSessionID, ent.ID, "Updated description", newEmbedding)
	if !success {
		t.Error("UpdateEntityDescription should return true")
	}

	retrieved, _ := e.GetEntity(testSessionID, ent.ID)
	if retrieved.Description != "Updated description" {
		t.Errorf("Expected description 'Updated description', got '%s'", retrieved.Description)
	}
}

// =============================================================================
// Relationship Operations Tests
// =============================================================================

func TestEngine_AddRelationship(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	ent1 := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc 1", embedding)
	ent2 := mustAddEntity(t, e, testSessionID, "ext-ent-2", "Entity 2", "test", "Desc 2", embedding)

	rel, err := e.AddRelationship(testSessionID, "ext-rel-1", ent1.ID, ent2.ID, "RELATED_TO", "Relationship desc", 1.0)
	if err != nil {
		t.Fatalf("AddRelationship failed: %v", err)
	}

	if rel.ID == 0 {
		t.Error("Relationship ID should not be 0")
	}
	if rel.SourceID != ent1.ID {
		t.Errorf("Expected SourceID %d, got %d", ent1.ID, rel.SourceID)
	}
	if rel.TargetID != ent2.ID {
		t.Errorf("Expected TargetID %d, got %d", ent2.ID, rel.TargetID)
	}
}

func TestEngine_GetRelationship(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	ent1 := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc 1", embedding)
	ent2 := mustAddEntity(t, e, testSessionID, "ext-ent-2", "Entity 2", "test", "Desc 2", embedding)

	rel := mustAddRelationship(t, e, testSessionID, "ext-rel-1", ent1.ID, ent2.ID, "RELATED_TO", "Desc", 1.0)

	retrieved, ok := e.GetRelationship(testSessionID, rel.ID)
	if !ok {
		t.Error("GetRelationship should return true")
	}
	if retrieved.Type != "RELATED_TO" {
		t.Errorf("Expected type 'RELATED_TO', got '%s'", retrieved.Type)
	}
}

func TestEngine_GetRelationshipByEntities(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	ent1 := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc 1", embedding)
	ent2 := mustAddEntity(t, e, testSessionID, "ext-ent-2", "Entity 2", "test", "Desc 2", embedding)

	mustAddRelationship(t, e, testSessionID, "ext-rel-1", ent1.ID, ent2.ID, "RELATED_TO", "Desc", 1.0)

	retrieved, ok := e.GetRelationshipByEntities(testSessionID, ent1.ID, ent2.ID)
	if !ok {
		t.Error("GetRelationshipByEntities should return true")
	}
	if retrieved.SourceID != ent1.ID || retrieved.TargetID != ent2.ID {
		t.Error("Retrieved wrong relationship")
	}
}

// =============================================================================
// Community Operations Tests
// =============================================================================

func TestEngine_AddCommunity(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	comm, err := e.AddCommunity(testSessionID, "ext-comm-1", "Test Community", "Summary", "Full content", 0, []uint64{1, 2, 3}, []uint64{1, 2}, embedding)
	if err != nil {
		t.Fatalf("AddCommunity failed: %v", err)
	}

	if comm.ID == 0 {
		t.Error("Community ID should not be 0")
	}
	if comm.Title != "Test Community" {
		t.Errorf("Expected title 'Test Community', got '%s'", comm.Title)
	}
}

func TestEngine_GetCommunity(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	comm, _ := e.AddCommunity(testSessionID, "ext-comm-1", "Test Community", "Summary", "Full content", 0, []uint64{1, 2, 3}, []uint64{1, 2}, embedding)

	retrieved, ok := e.GetCommunity(testSessionID, comm.ID)
	if !ok {
		t.Error("GetCommunity should return true")
	}
	if retrieved.Summary != "Summary" {
		t.Errorf("Expected summary 'Summary', got '%s'", retrieved.Summary)
	}
}

// =============================================================================
// Query Pipeline Tests
// =============================================================================

func TestEngine_Query_Basic(t *testing.T) {
	e := createTestEngine()

	// Add test data
	embedding1 := randomVector(testVectorDim)
	embedding2 := randomVector(testVectorDim)

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	mustAddTextUnit(t, e, testSessionID, "ext-tu-1", doc.ID, "Test content 1", embedding1, 10)
	mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Description 1", embedding1)
	e.AddCommunity(testSessionID, "ext-comm-1", "Community 1", "Summary", "Full", 0, []uint64{1}, []uint64{}, embedding2)

	// Query
	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding1

	result, err := e.Query(testSessionID, spec)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.QueryID == 0 {
		t.Error("QueryID should not be 0")
	}
}

func TestEngine_Query_EmptyIndex(t *testing.T) {
	e := createTestEngine()

	// Add a document to ensure session exists (even though indices are empty)
	mustAddDocument(t, e, testSessionID, "doc-1", "test.pdf")

	spec := types.DefaultQuerySpec()
	spec.QueryVector = randomVector(testVectorDim)

	result, err := e.Query(testSessionID, spec)
	if err != nil {
		t.Fatalf("Query on empty index should not error: %v", err)
	}

	if len(result.TextUnits) != 0 {
		t.Error("TextUnits should be empty on empty index")
	}
}

func TestEngine_Query_WithGraphExpansion(t *testing.T) {
	e := createTestEngine()

	// Setup: entities with relationships
	embedding := randomVector(testVectorDim)

	ent1 := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc 1", embedding)
	ent2 := mustAddEntity(t, e, testSessionID, "ext-ent-2", "Entity 2", "test", "Desc 2", randomVector(testVectorDim))
	mustAddRelationship(t, e, testSessionID, "rel-1", ent1.ID, ent2.ID, "RELATED", "Desc", 1.0)

	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding
	spec.KHops = 2
	spec.SearchTypes = []types.SearchType{types.SearchTypeEntity}

	result, err := e.Query(testSessionID, spec)
	if err != nil {
		t.Fatalf("Query with graph expansion failed: %v", err)
	}

	// Should find entities via graph traversal
	if result.QueryID == 0 {
		t.Error("QueryID should not be 0")
	}
}

// =============================================================================
// Explain Tests
// =============================================================================

func TestEngine_Explain(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc 1", embedding)

	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding

	result, _ := e.Query(testSessionID, spec)

	explain, ok := e.Explain(result.QueryID)
	if !ok {
		t.Error("Explain should return true for valid QueryID")
	}

	if explain.QueryID != result.QueryID {
		t.Errorf("Explain QueryID mismatch: got %d, want %d", explain.QueryID, result.QueryID)
	}
}

func TestEngine_Explain_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.Explain(99999)
	if ok {
		t.Error("Explain should return false for non-existent QueryID")
	}
}

// =============================================================================
// Info Tests
// =============================================================================

func TestEngine_Info(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc 1", embedding)

	info := e.Info()

	if info.Version == "" {
		t.Error("Version should not be empty")
	}
	if info.DocumentCount != 1 {
		t.Errorf("Expected DocumentCount 1, got %d", info.DocumentCount)
	}
	if info.EntityCount != 1 {
		t.Errorf("Expected EntityCount 1, got %d", info.EntityCount)
	}
	if info.VectorDim != testVectorDim {
		t.Errorf("Expected VectorDim %d, got %d", testVectorDim, info.VectorDim)
	}
}

// =============================================================================
// TTL Tests
// =============================================================================

// DEPRECATED v0.1.0: TTL management moved to session level
// func TestEngine_SetTTL(t *testing.T) { ... }

// DEPRECATED v0.1.0
// func TestEngine_SetTTL_NotFound(t *testing.T) { ... }

// DEPRECATED v0.1.0
// func TestEngine_SetIdleTTL(t *testing.T) { ... }

// =============================================================================
// Link Operations Tests
// =============================================================================

func TestEngine_LinkTextUnitToEntity(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	embedding := randomVector(testVectorDim)
	tu := mustAddTextUnit(t, e, testSessionID, "ext-tu-1", doc.ID, "Test content", embedding, 10)
	ent := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc", embedding)

	success := e.LinkTextUnitToEntity(testSessionID, tu.ID, ent.ID)
	if !success {
		t.Error("LinkTextUnitToEntity should return true")
	}

	// Verify link
	retrievedTU, _ := e.GetTextUnit(testSessionID, tu.ID)
	found := false
	for _, eid := range retrievedTU.EntityIDs {
		if eid == ent.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("TextUnit should have entity linked")
	}

	// Verify reverse link
	retrievedEnt, _ := e.GetEntity(testSessionID, ent.ID)
	found = false
	for _, tuID := range retrievedEnt.TextUnitIDs {
		if tuID == tu.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Entity should have text unit linked")
	}
}

// =============================================================================
// LRU Cache Tests
// =============================================================================

func TestQueryLogLRU_Basic(t *testing.T) {
	cache := newQueryLogLRU(10)

	log := &queryLog{spec: types.DefaultQuerySpec()}
	cache.Set(1, log)

	retrieved, ok := cache.Get(1)
	if !ok {
		t.Error("Get should return true")
	}
	if retrieved == nil {
		t.Error("Retrieved log should not be nil")
	}
}

func TestQueryLogLRU_Eviction(t *testing.T) {
	cache := newQueryLogLRU(3)

	// Add 4 items (capacity is 3)
	for i := uint64(1); i <= 4; i++ {
		cache.Set(i, &queryLog{})
	}

	// First item should be evicted
	_, ok := cache.Get(1)
	if ok {
		t.Error("Item 1 should have been evicted")
	}

	// Latest items should exist
	_, ok = cache.Get(4)
	if !ok {
		t.Error("Item 4 should exist")
	}
}

func TestQueryLogLRU_Len(t *testing.T) {
	cache := newQueryLogLRU(10)

	cache.Set(1, &queryLog{})
	cache.Set(2, &queryLog{})

	if cache.Len() != 2 {
		t.Errorf("Expected Len 2, got %d", cache.Len())
	}
}

// =============================================================================
// Concurrent Tests
// =============================================================================

func TestEngine_ConcurrentAccess(t *testing.T) {
	e := createTestEngine()

	var wg sync.WaitGroup
	const n = 20

	// Concurrent document additions
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			mustAddDocument(t, e, testSessionID, "doc-"+itoa(id), "file.txt")
		}(i)
	}

	// Concurrent entity additions
	embedding := randomVector(testVectorDim)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			mustAddEntity(t, e, testSessionID, "ent-"+itoa(id), "Entity "+itoa(id), "test", "Desc", embedding)
		}(i)
	}

	wg.Wait()

	info := e.Info()
	if info.DocumentCount != n {
		t.Errorf("Expected %d documents, got %d", n, info.DocumentCount)
	}
	if info.EntityCount != n {
		t.Errorf("Expected %d entities, got %d", n, info.EntityCount)
	}
}

func TestEngine_ConcurrentQueries(t *testing.T) {
	e := createTestEngine()

	// Setup data
	embedding := randomVector(testVectorDim)
	for i := 0; i < 10; i++ {
		mustAddEntity(t, e, testSessionID, "ent-"+itoa(i), "Entity "+itoa(i), "test", "Desc", embedding)
	}

	var wg sync.WaitGroup
	const n = 20

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spec := types.DefaultQuerySpec()
			spec.QueryVector = embedding
			e.Query(testSessionID, spec)
		}()
	}

	wg.Wait()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte(i%10) + '0'
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// =============================================================================
// Additional Coverage Tests
// =============================================================================

func TestEngine_GetTextUnit_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.GetTextUnit(testSessionID, 99999)
	if ok {
		t.Error("GetTextUnit should return false for non-existent")
	}
}

func TestEngine_GetEntity_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.GetEntity(testSessionID, 99999)
	if ok {
		t.Error("GetEntity should return false for non-existent")
	}
}

func TestEngine_GetEntityByTitle_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.GetEntityByTitle(testSessionID, "Non Existent Entity")
	if ok {
		t.Error("GetEntityByTitle should return false for non-existent")
	}
}

func TestEngine_UpdateEntityDescription_NotFound(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	success := e.UpdateEntityDescription(testSessionID, 99999, "New desc", embedding)
	if success {
		t.Error("UpdateEntityDescription should return false for non-existent")
	}
}

func TestEngine_GetRelationship_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.GetRelationship(testSessionID, 99999)
	if ok {
		t.Error("GetRelationship should return false for non-existent")
	}
}

func TestEngine_GetRelationshipByEntities_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.GetRelationshipByEntities(testSessionID, 99999, 99998)
	if ok {
		t.Error("GetRelationshipByEntities should return false for non-existent")
	}
}

func TestEngine_GetCommunity_NotFound(t *testing.T) {
	e := createTestEngine()

	_, ok := e.GetCommunity(testSessionID, 99999)
	if ok {
		t.Error("GetCommunity should return false for non-existent")
	}
}

func TestEngine_AddRelationship_Duplicate(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	ent1 := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity 1", "test", "Desc 1", embedding)
	ent2 := mustAddEntity(t, e, testSessionID, "ext-ent-2", "Entity 2", "test", "Desc 2", embedding)

	_, err := e.AddRelationship(testSessionID, "ext-rel-1", ent1.ID, ent2.ID, "RELATED_TO", "Desc", 1.0)
	if err != nil {
		t.Fatalf("First AddRelationship failed: %v", err)
	}

	_, err = e.AddRelationship(testSessionID, "ext-rel-2", ent1.ID, ent2.ID, "ANOTHER_TYPE", "Desc", 1.0)
	if err == nil {
		t.Error("Duplicate relationship should fail")
	}
}

func TestEngine_AddCommunity_Duplicate(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	_, err := e.AddCommunity(testSessionID, "ext-comm-1", "Community 1", "Summary", "Full", 0, []uint64{1}, []uint64{}, embedding)
	if err != nil {
		t.Fatalf("First AddCommunity failed: %v", err)
	}

	_, err = e.AddCommunity(testSessionID, "ext-comm-1", "Community 2", "Summary", "Full", 0, []uint64{1}, []uint64{}, embedding)
	if err == nil {
		t.Error("Duplicate community should fail")
	}
}

func TestEngine_LinkTextUnitToEntity_NotFound(t *testing.T) {
	e := createTestEngine()

	success := e.LinkTextUnitToEntity(testSessionID, 99999, 99998)
	if success {
		t.Error("LinkTextUnitToEntity should return false for non-existent")
	}
}

// DEPRECATED v0.1.0: TTL management removed
/*
func TestEngine_SetTTL_AllTypes(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	embedding := randomVector(testVectorDim)
	tu := mustAddTextUnit(t, e, testSessionID, "ext-tu-1", doc.ID, "Content", embedding, 10)
	ent := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity", "test", "Desc", embedding)
	comm, _ := e.AddCommunity("ext-comm-1", "Comm", "Sum", "Full", 0, []uint64{}, []uint64{}, embedding, 0, 0)

	tests := []struct {
		itemType types.ItemType
		id       uint64
	}{
		{types.ItemTypeDocument, doc.ID},
		{types.ItemTypeTextUnit, tu.ID},
		{types.ItemTypeEntity, ent.ID},
		{types.ItemTypeCommunity, comm.ID},
	}

	for _, tt := range tests {
		success := e.SetTTL(tt.itemType, tt.id, 3600)
		if !success {
			t.Errorf("SetTTL failed for %s", tt.itemType)
		}

		ttl := e.GetTTL(tt.itemType, tt.id)
		if ttl <= 0 {
			t.Errorf("GetTTL should be positive for %s", tt.itemType)
		}
	}
}
*/

// DEPRECATED v0.1.0
/*
func TestEngine_SetIdleTTL_AllTypes(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	embedding := randomVector(testVectorDim)
	tu := mustAddTextUnit(t, e, testSessionID, "ext-tu-1", doc.ID, "Content", embedding, 10)
	ent := mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity", "test", "Desc", embedding)
	comm, _ := e.AddCommunity("ext-comm-1", "Comm", "Sum", "Full", 0, []uint64{}, []uint64{}, embedding, 0, 0)

	tests := []struct {
		itemType types.ItemType
		id       uint64
	}{
		{types.ItemTypeDocument, doc.ID},
		{types.ItemTypeTextUnit, tu.ID},
		{types.ItemTypeEntity, ent.ID},
		{types.ItemTypeCommunity, comm.ID},
	}

	for _, tt := range tests {
		success := e.SetIdleTTL(tt.itemType, tt.id, 600)
		if !success {
			t.Errorf("SetIdleTTL failed for %s", tt.itemType)
		}
	}
}
*/

// DEPRECATED v0.1.0
/*
func TestEngine_GetTTL_InvalidType(t *testing.T) {
	e := createTestEngine()

	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	e.SetTTL(types.ItemTypeDocument, doc.ID, 3600)

	// Test with unknown item type - returns negative value
	ttl := e.GetTTL("unknown", doc.ID)
	if ttl >= 0 {
		t.Errorf("GetTTL with invalid type should return negative value, got %d", ttl)
	}
}
*/

func TestEngine_Query_AllSearchTypes(t *testing.T) {
	e := createTestEngine()

	embedding := randomVector(testVectorDim)
	doc := mustAddDocument(t, e, testSessionID, "ext-doc-1", "test.txt")
	mustAddTextUnit(t, e, testSessionID, "ext-tu-1", doc.ID, "Content", embedding, 10)
	mustAddEntity(t, e, testSessionID, "ext-ent-1", "Entity", "test", "Desc", embedding)
	e.AddCommunity(testSessionID, "ext-comm-1", "Comm", "Summary", "Full", 0, []uint64{}, []uint64{}, embedding)

	// Test each search type
	searchTypes := []types.SearchType{
		types.SearchTypeTextUnit,
		types.SearchTypeEntity,
		types.SearchTypeCommunity,
	}

	for _, st := range searchTypes {
		spec := types.DefaultQuerySpec()
		spec.QueryVector = embedding
		spec.SearchTypes = []types.SearchType{st}

		result, err := e.Query(testSessionID, spec)
		if err != nil {
			t.Errorf("Query with search type %s failed: %v", st, err)
		}
		if result.QueryID == 0 {
			t.Errorf("Query with search type %s returned invalid QueryID", st)
		}
	}
}

func TestEngine_ListEntitiesPagination(t *testing.T) {
	e := createTestEngine()

	for i := 0; i < 5; i++ {
		extID := fmt.Sprintf("ent-%d", i+1)
		title := fmt.Sprintf("Entity %d", i+1)
		mustAddEntity(t, e, testSessionID, extID, title, "person", "desc", nil)
	}

	entities, next := e.ListEntities(testSessionID, 0, 2)
	if len(entities) != 2 {
		t.Fatalf("Expected 2 entities, got %d", len(entities))
	}
	if next == 0 {
		t.Fatalf("Expected non-zero next cursor")
	}
	if entities[0].ID >= entities[1].ID {
		t.Errorf("Expected ascending IDs, got %d then %d", entities[0].ID, entities[1].ID)
	}

	entities2, next2 := e.ListEntities(testSessionID, next, 2)
	if len(entities2) != 2 {
		t.Fatalf("Expected 2 entities, got %d", len(entities2))
	}
	if next2 == 0 {
		t.Fatalf("Expected non-zero next cursor for page 2")
	}

	entities3, next3 := e.ListEntities(testSessionID, next2, 2)
	if len(entities3) != 1 {
		t.Fatalf("Expected 1 entity, got %d", len(entities3))
	}
	if next3 != 0 {
		t.Fatalf("Expected next cursor 0 at end, got %d", next3)
	}
}

func TestEngine_ListRelationshipsPagination(t *testing.T) {
	e := createTestEngine()

	e1 := mustAddEntity(t, e, testSessionID, "ent-001", "Entity 1", "person", "desc", nil)
	e2 := mustAddEntity(t, e, testSessionID, "ent-002", "Entity 2", "person", "desc", nil)
	e3 := mustAddEntity(t, e, testSessionID, "ent-003", "Entity 3", "person", "desc", nil)

	mustAddRelationship(t, e, testSessionID, "rel-001", e1.ID, e2.ID, "KNOWS", "desc", 1.0)
	mustAddRelationship(t, e, testSessionID, "rel-002", e2.ID, e3.ID, "KNOWS", "desc", 1.0)
	mustAddRelationship(t, e, testSessionID, "rel-003", e3.ID, e1.ID, "KNOWS", "desc", 1.0)

	rels, next := e.ListRelationships(testSessionID, 0, 2)
	if len(rels) != 2 {
		t.Fatalf("Expected 2 relationships, got %d", len(rels))
	}
	if next == 0 {
		t.Fatalf("Expected non-zero next cursor")
	}
	if rels[0].ID >= rels[1].ID {
		t.Errorf("Expected ascending IDs, got %d then %d", rels[0].ID, rels[1].ID)
	}

	rels2, next2 := e.ListRelationships(testSessionID, next, 2)
	if len(rels2) != 1 {
		t.Fatalf("Expected 1 relationship, got %d", len(rels2))
	}
	if next2 != 0 {
		t.Fatalf("Expected next cursor 0 at end, got %d", next2)
	}
}

func TestQueryLogLRU_Update(t *testing.T) {
	cache := newQueryLogLRU(3)

	cache.Set(1, &queryLog{})
	cache.Set(2, &queryLog{})
	cache.Set(3, &queryLog{})

	// Add new item, should evict oldest item (item 1)
	cache.Set(4, &queryLog{})

	_, ok := cache.Get(1)
	if ok {
		t.Error("Item 1 should have been evicted (oldest)")
	}

	// Items 2, 3, 4 should exist
	_, ok = cache.Get(4)
	if !ok {
		t.Error("Item 4 should exist")
	}
}
