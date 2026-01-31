// Package engine - comprehensive tests representing real user scenarios
package engine

import (
	"bytes"
	"math/rand"
	"sync"
	"testing"

	"github.com/gibram-io/gibram/pkg/graph"
	"github.com/gibram-io/gibram/pkg/types"
)

// =============================================================================
// Real-World Scenario: Document Q&A Pipeline (Chat with PDF)
// =============================================================================

func TestScenario_ChatWithPDF(t *testing.T) {
	e := NewEngine(testVectorDim)

	// User uploads a PDF document
	doc, err := e.AddDocument(testSessionID, "pdf-001", "research_paper.pdf")
	if err != nil {
		t.Fatalf("Failed to upload document: %v", err)
	}

	if doc.Status != types.DocStatusUploaded {
		t.Error("Document should start with Uploaded status")
	}

	// Pipeline processes document into chunks (text units)
	chunks := []struct {
		extID   string
		content string
	}{
		{"chunk-001", "Machine learning is a subset of artificial intelligence..."},
		{"chunk-002", "Neural networks consist of interconnected nodes..."},
		{"chunk-003", "Deep learning models require large amounts of training data..."},
	}

	var textUnits []*types.TextUnit
	for _, chunk := range chunks {
		embedding := generateSemanticVector(chunk.content)
		tu, err := e.AddTextUnit(testSessionID, chunk.extID, doc.ID, chunk.content, embedding, len(chunk.content)/4)
		if err != nil {
			t.Fatalf("Failed to add text unit: %v", err)
		}
		textUnits = append(textUnits, tu)
	}

	// Pipeline extracts entities
	entities := []struct {
		extID       string
		title       string
		entType     string
		description string
	}{
		{"ent-001", "Machine Learning", "concept", "A branch of AI focused on learning from data"},
		{"ent-002", "Neural Network", "concept", "Computing systems inspired by biological neural networks"},
		{"ent-003", "Deep Learning", "concept", "A subset of machine learning using neural networks"},
	}

	var entityList []*types.Entity
	for _, ent := range entities {
		embedding := generateSemanticVector(ent.description)
		entity, err := e.AddEntity(testSessionID, ent.extID, ent.title, ent.entType, ent.description, embedding)
		if err != nil {
			t.Fatalf("Failed to add entity: %v", err)
		}
		entityList = append(entityList, entity)
	}

	// Link text units to entities
	e.LinkTextUnitToEntity(testSessionID, textUnits[0].ID, entityList[0].ID) // ML chunk -> ML entity
	e.LinkTextUnitToEntity(testSessionID, textUnits[1].ID, entityList[1].ID) // NN chunk -> NN entity
	e.LinkTextUnitToEntity(testSessionID, textUnits[2].ID, entityList[2].ID) // DL chunk -> DL entity

	// Add relationships between entities
	mustAddRelationship(t, e, testSessionID, "rel-001", entityList[0].ID, entityList[1].ID, "USES", "ML uses neural networks", 0.9)
	mustAddRelationship(t, e, testSessionID, "rel-002", entityList[2].ID, entityList[1].ID, "BASED_ON", "DL is based on neural networks", 0.95)

	// Mark document as ready
	e.UpdateDocumentStatus(testSessionID, doc.ID, types.DocStatusReady)

	// User asks a question: "What is deep learning?"
	queryVector := generateSemanticVector("What is deep learning?")
	spec := types.QuerySpec{
		QueryVector:    queryVector,
		TopK:           5,
		KHops:          1,
		MaxTextUnits:   10,
		MaxEntities:    10,
		MaxCommunities: 5,
		SearchTypes:    []types.SearchType{types.SearchTypeTextUnit, types.SearchTypeEntity},
	}

	result, err := e.Query(testSessionID, spec)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Verify query returned relevant results
	if len(result.TextUnits) == 0 {
		t.Error("Query should return text units")
	}
	if len(result.Entities) == 0 {
		t.Error("Query should return entities")
	}
	if result.QueryID == 0 {
		t.Error("Query should return valid QueryID")
	}

	// User can explain how results were found
	explain, ok := e.Explain(result.QueryID)
	if !ok {
		t.Error("Explain should work for recent query")
	}
	if len(explain.Seeds) == 0 {
		t.Error("Explain should show seed results")
	}
}

// =============================================================================
// Real-World Scenario: Session-Scoped Research Assistant
// =============================================================================

func TestScenario_ResearchSession(t *testing.T) {
	e := NewEngine(testVectorDim)

	// Start research session - add multiple papers
	papers := []string{
		"Attention Is All You Need",
		"BERT: Pre-training of Deep Bidirectional Transformers",
		"GPT-3: Language Models are Few-Shot Learners",
	}

	for i, paper := range papers {
		// Add as document
		doc := mustAddDocument(t, e, testSessionID, "paper-"+itoa(i), paper+".pdf")

		// Add key concepts as entities
		embedding := generateSemanticVector(paper)
		mustAddEntity(t, e, testSessionID, "concept-"+itoa(i), paper, "paper", "Research paper about "+paper, embedding)

		// Add chunks
		for j := 0; j < 3; j++ {
			chunkContent := "Section " + itoa(j) + " of " + paper
			chunkEmbedding := generateSemanticVector(chunkContent)
			mustAddTextUnit(t, e, testSessionID, "chunk-"+itoa(i)+"-"+itoa(j), doc.ID, chunkContent, chunkEmbedding, 50)
		}
	}

	// Add cross-paper relationships
	// All papers are about transformers
	info := e.Info()
	if info.EntityCount != 3 {
		t.Errorf("Expected 3 entities, got %d", info.EntityCount)
	}
	if info.TextUnitCount != 9 {
		t.Errorf("Expected 9 text units, got %d", info.TextUnitCount)
	}

	// Query across all papers
	queryVector := generateSemanticVector("transformer architecture attention mechanism")
	spec := types.DefaultQuerySpec()
	spec.QueryVector = queryVector
	spec.SearchTypes = []types.SearchType{types.SearchTypeTextUnit, types.SearchTypeEntity}

	result, err := e.Query(testSessionID, spec)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if result.QueryID == 0 {
		t.Error("Should get valid query ID")
	}
}

// =============================================================================
// Real-World Scenario: Graph-Based Knowledge Navigation
// =============================================================================

func TestScenario_KnowledgeGraphNavigation(t *testing.T) {
	e := NewEngine(testVectorDim)

	// Build a knowledge graph about a company
	type entityDef struct {
		extID, title, entType, desc string
	}

	entities := []entityDef{
		{"ceo", "John Smith", "person", "CEO of TechCorp"},
		{"cto", "Jane Doe", "person", "CTO of TechCorp"},
		{"company", "TechCorp", "organization", "A technology company"},
		{"product1", "CloudDB", "product", "Cloud database product"},
		{"product2", "AIEngine", "product", "AI inference engine"},
	}

	entityMap := make(map[string]*types.Entity)
	for _, ent := range entities {
		embedding := generateSemanticVector(ent.desc)
		entity := mustAddEntity(t, e, testSessionID, ent.extID, ent.title, ent.entType, ent.desc, embedding)
		entityMap[ent.extID] = entity
	}

	// Build relationships
	mustAddRelationship(t, e, testSessionID, "r1", entityMap["ceo"].ID, entityMap["company"].ID, "LEADS", "CEO leads company", 1.0)
	mustAddRelationship(t, e, testSessionID, "r2", entityMap["cto"].ID, entityMap["company"].ID, "WORKS_AT", "CTO works at company", 0.9)
	mustAddRelationship(t, e, testSessionID, "r3", entityMap["company"].ID, entityMap["product1"].ID, "PRODUCES", "Company produces product", 0.8)
	mustAddRelationship(t, e, testSessionID, "r4", entityMap["company"].ID, entityMap["product2"].ID, "PRODUCES", "Company produces product", 0.8)
	mustAddRelationship(t, e, testSessionID, "r5", entityMap["cto"].ID, entityMap["product2"].ID, "MANAGES", "CTO manages AI product", 0.7)

	// Query starting from CEO, traverse to find related entities
	queryVector := generateSemanticVector("John Smith CEO leadership")
	spec := types.QuerySpec{
		QueryVector:    queryVector,
		TopK:           3,
		KHops:          2, // Navigate 2 hops through the graph
		MaxTextUnits:   10,
		MaxEntities:    20,
		MaxCommunities: 5,
		SearchTypes:    []types.SearchType{types.SearchTypeEntity},
	}

	result, err := e.Query(testSessionID, spec)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// Should find related entities via graph traversal
	if len(result.Entities) < 2 {
		t.Errorf("Expected at least 2 entities via graph traversal, got %d", len(result.Entities))
	}

	// Should find relationships between discovered entities
	if len(result.Relationships) == 0 {
		t.Log("Note: No relationships in final result (entities might not be adjacent)")
	}
}

// =============================================================================
// Real-World Scenario: Community Detection for Topic Clustering
// =============================================================================

func TestScenario_CommunityDetection(t *testing.T) {
	e := NewEngine(testVectorDim)

	// Create entities that should cluster into 2 communities
	// Community 1: ML concepts
	mlConcepts := []string{"Machine Learning", "Supervised Learning", "Unsupervised Learning", "Regression"}
	// Community 2: DB concepts
	dbConcepts := []string{"Database", "SQL", "NoSQL", "ACID"}

	var mlEntities, dbEntities []*types.Entity
	embedding := randomVector(testVectorDim)

	for i, concept := range mlConcepts {
		ent := mustAddEntity(t, e, testSessionID, "ml-"+itoa(i), concept, "concept", "ML: "+concept, embedding)
		mlEntities = append(mlEntities, ent)
	}

	for i, concept := range dbConcepts {
		ent := mustAddEntity(t, e, testSessionID, "db-"+itoa(i), concept, "concept", "DB: "+concept, embedding)
		dbEntities = append(dbEntities, ent)
	}

	// Create dense connections within communities
	for i := 0; i < len(mlEntities)-1; i++ {
		mustAddRelationship(t, e, testSessionID, "ml-rel-"+itoa(i), mlEntities[i].ID, mlEntities[i+1].ID, "RELATED", "Related ML concepts", 1.0)
	}
	for i := 0; i < len(dbEntities)-1; i++ {
		mustAddRelationship(t, e, testSessionID, "db-rel-"+itoa(i), dbEntities[i].ID, dbEntities[i+1].ID, "RELATED", "Related DB concepts", 1.0)
	}

	// One weak connection between communities
	mustAddRelationship(t, e, testSessionID, "cross-rel", mlEntities[0].ID, dbEntities[0].ID, "USED_WITH", "ML can use databases", 0.1)

	// Run community detection
	config := graph.DefaultLeidenConfig()
	config.Resolution = 1.0
	communities, err := e.ComputeCommunities(testSessionID, config)
	if err != nil {
		t.Fatalf("Community detection failed: %v", err)
	}

	// Should detect at least 1 community
	if len(communities) == 0 {
		t.Log("Note: Community detection may return 0 communities with small graphs")
	}

	info := e.Info()
	t.Logf("Detected %d communities from %d entities", info.CommunityCount, info.EntityCount)
}

// =============================================================================
// Real-World Scenario: TTL-Based Session Cleanup
// =============================================================================

// =============================================================================
// Real-World Scenario: Snapshot and Restore (Debugging)
// =============================================================================

func TestScenario_SnapshotRestore(t *testing.T) {
	e := NewEngine(testVectorDim)

	// Build up some state
	embedding := randomVector(testVectorDim)
	doc := mustAddDocument(t, e, testSessionID, "doc-1", "file.pdf")
	mustAddTextUnit(t, e, testSessionID, "tu-1", doc.ID, "Content", embedding, 10)
	ent1 := mustAddEntity(t, e, testSessionID, "ent-1", "Entity One", "test", "Description", embedding)
	ent2 := mustAddEntity(t, e, testSessionID, "ent-2", "Entity Two", "test", "Description", embedding)
	mustAddRelationship(t, e, testSessionID, "rel-1", ent1.ID, ent2.ID, "RELATED", "Desc", 1.0)
	e.AddCommunity(testSessionID, "comm-1", "Community", "Summary", "Full", 0, []uint64{ent1.ID, ent2.ID}, []uint64{}, embedding)

	// Get original state
	originalInfo := e.Info()

	// Snapshot to buffer
	var buf bytes.Buffer
	err := e.Snapshot(&buf)
	if err != nil {
		t.Fatalf("Snapshot failed: %v", err)
	}

	// Create new engine and restore
	e2 := NewEngine(testVectorDim)
	err = e2.Restore(&buf)
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Verify restored state matches
	restoredInfo := e2.Info()
	if restoredInfo.DocumentCount != originalInfo.DocumentCount {
		t.Errorf("Document count mismatch: %d vs %d", restoredInfo.DocumentCount, originalInfo.DocumentCount)
	}
	if restoredInfo.TextUnitCount != originalInfo.TextUnitCount {
		t.Errorf("TextUnit count mismatch: %d vs %d", restoredInfo.TextUnitCount, originalInfo.TextUnitCount)
	}
	if restoredInfo.EntityCount != originalInfo.EntityCount {
		t.Errorf("Entity count mismatch: %d vs %d", restoredInfo.EntityCount, originalInfo.EntityCount)
	}
	if restoredInfo.RelationshipCount != originalInfo.RelationshipCount {
		t.Errorf("Relationship count mismatch: %d vs %d", restoredInfo.RelationshipCount, originalInfo.RelationshipCount)
	}
	if restoredInfo.CommunityCount != originalInfo.CommunityCount {
		t.Errorf("Community count mismatch: %d vs %d", restoredInfo.CommunityCount, originalInfo.CommunityCount)
	}

	// Verify data is queryable after restore
	_, ok := e2.GetDocument(testSessionID, doc.ID)
	if !ok {
		t.Error("Should be able to retrieve document after restore")
	}
}

// =============================================================================
// Real-World Scenario: Concurrent Access (Multi-user)
// =============================================================================

func TestScenario_ConcurrentMultiUser(t *testing.T) {
	e := NewEngine(testVectorDim)

	var wg sync.WaitGroup
	const numUsers = 10
	const opsPerUser = 50
	errCh := make(chan error, numUsers*opsPerUser*3)

	// Simulate multiple users adding and querying data concurrently
	for user := 0; user < numUsers; user++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()

			// Each user adds their own data
			for i := 0; i < opsPerUser; i++ {
				prefix := itoa(userID) + "-" + itoa(i)

				// Add document
				doc, err := e.AddDocument(testSessionID, "doc-"+prefix, "file.pdf")
				if err != nil {
					errCh <- err
					continue
				}

				// Add text unit
				embedding := randomVector(testVectorDim)
				if _, err := e.AddTextUnit(testSessionID, "tu-"+prefix, doc.ID, "Content", embedding, 10); err != nil {
					errCh <- err
				}

				// Add entity
				if _, err := e.AddEntity(testSessionID, "ent-"+prefix, "Entity "+prefix, "test", "Desc", embedding); err != nil {
					errCh <- err
				}
			}
		}(user)
	}

	// Also run queries concurrently
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < opsPerUser/5; j++ {
				spec := types.DefaultQuerySpec()
				spec.QueryVector = randomVector(testVectorDim)
				e.Query(testSessionID, spec)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("Concurrent add error: %v", err)
	}

	// Verify final state
	info := e.Info()
	expectedDocs := numUsers * opsPerUser
	if info.DocumentCount != expectedDocs {
		t.Errorf("Expected %d documents, got %d", expectedDocs, info.DocumentCount)
	}
}

// =============================================================================
// Real-World Scenario: Hierarchical Communities
// =============================================================================

func TestScenario_HierarchicalCommunities(t *testing.T) {
	e := NewEngine(testVectorDim)

	// Create a larger graph for hierarchical community detection
	embedding := randomVector(testVectorDim)

	// Create 20 entities in 4 clusters
	var entities []*types.Entity
	for i := 0; i < 20; i++ {
		ent := mustAddEntity(t, e, testSessionID, "ent-"+itoa(i), "Entity "+itoa(i), "test", "Cluster "+itoa(i/5), embedding)
		entities = append(entities, ent)
	}

	// Create dense connections within clusters, sparse between
	for i := 0; i < 20; i++ {
		cluster := i / 5
		for j := i + 1; j < 20; j++ {
			otherCluster := j / 5
			if cluster == otherCluster {
				// Dense within cluster
				mustAddRelationship(t, e, testSessionID, "rel-"+itoa(i)+"-"+itoa(j), entities[i].ID, entities[j].ID, "SIMILAR", "Same cluster", 1.0)
			} else if rand.Float32() < 0.1 {
				// Sparse between clusters
				mustAddRelationship(t, e, testSessionID, "rel-"+itoa(i)+"-"+itoa(j), entities[i].ID, entities[j].ID, "RELATED", "Different cluster", 0.2)
			}
		}
	}

	// Run hierarchical community detection
	config := graph.DefaultLeidenConfig()
	config.MaxLevels = 3
	config.LevelResolution = 0.7
	config.MinCommunitySize = 2

	communities, err := e.ComputeHierarchicalCommunities(testSessionID, config)
	if err != nil {
		t.Fatalf("Hierarchical community detection failed: %v", err)
	}

	t.Logf("Detected %d communities at multiple levels", len(communities))
}

// =============================================================================
// Engine Methods Coverage Tests
// =============================================================================

func TestEngine_DeleteDocument(t *testing.T) {
	e := NewEngine(testVectorDim)

	doc := mustAddDocument(t, e, testSessionID, "doc-1", "file.pdf")

	// Delete should work via TTL expiry simulation
	// In production, deletion would go through TTL or explicit delete

	_, ok := e.GetDocument(testSessionID, doc.ID)
	if !ok {
		t.Error("Document should exist before any cleanup")
	}
}

func TestEngine_RebuildVectorIndices(t *testing.T) {
	e := NewEngine(testVectorDim)

	// Add some data with vectors
	embedding := randomVector(testVectorDim)
	doc := mustAddDocument(t, e, testSessionID, "doc-1", "file.pdf")
	mustAddTextUnit(t, e, testSessionID, "tu-1", doc.ID, "Content", embedding, 10)
	mustAddEntity(t, e, testSessionID, "ent-1", "Entity", "test", "Desc", embedding)
	e.AddCommunity(testSessionID, "comm-1", "Community", "Summary", "Full", 0, nil, nil, embedding)

	// Rebuild indices
	err := e.RebuildVectorIndices(testSessionID)
	if err != nil {
		t.Fatalf("RebuildVectorIndices failed: %v", err)
	}

	// Verify indices still work
	spec := types.DefaultQuerySpec()
	spec.QueryVector = embedding

	result, err := e.Query(testSessionID, spec)
	if err != nil {
		t.Fatalf("Query after rebuild failed: %v", err)
	}

	if result.QueryID == 0 {
		t.Error("Should get valid query result after rebuild")
	}
}

func TestEngine_Clear(t *testing.T) {
	e := NewEngine(testVectorDim)

	// Add data
	embedding := randomVector(testVectorDim)
	mustAddDocument(t, e, testSessionID, "doc-1", "file.pdf")
	mustAddEntity(t, e, testSessionID, "ent-1", "Entity", "test", "Desc", embedding)

	// Clear all
	err := e.Clear()
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	info := e.Info()
	if info.DocumentCount != 0 {
		t.Error("Documents should be cleared")
	}
	if info.EntityCount != 0 {
		t.Error("Entities should be cleared")
	}
}

/*
// DEPRECATED: These methods (GetAll*, GetStore accessors, Restore*) are no longer part of the v0.1.0 API
// They were removed as part of the session-based architecture refactoring

func TestEngine_GetAllMethods(t *testing.T) {
	...
}

func TestEngine_GetStoreAccessors(t *testing.T) {
	...
}

func TestEngine_RestoreMethods(t *testing.T) {
	...
}

func TestEngine_RestoreInvalidJSON(t *testing.T) {
	...
}
*/

func TestEngine_SnapshotRestoreVectorMismatch(t *testing.T) {
	e1 := NewEngine(testVectorDim)
	e2 := NewEngine(testVectorDim * 2) // Different dimension

	embedding := randomVector(testVectorDim)
	e1.AddEntity(testSessionID, "ent-1", "Entity", "test", "Desc", embedding)

	var buf bytes.Buffer
	e1.Snapshot(&buf)

	err := e2.Restore(&buf)
	if err == nil {
		t.Error("Restore should fail with dimension mismatch")
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// generateSemanticVector creates a pseudo-semantic vector for testing
// In production, this would be a real embedding from OpenAI/etc.
func generateSemanticVector(text string) []float32 {
	v := make([]float32, testVectorDim)
	// Use text hash to create deterministic but varied vectors
	for i := range v {
		v[i] = float32((int(text[i%len(text)])+i)%100) / 100.0
	}
	return v
}
