package engine

import (
	"testing"

	"github.com/gibram-io/gibram/pkg/types"
)

func mustAddDocument(tb testing.TB, e *Engine, sessionID, extID, filename string) *types.Document {
	tb.Helper()
	doc, err := e.AddDocument(sessionID, extID, filename)
	if err != nil {
		tb.Fatalf("AddDocument() error: %v", err)
	}
	return doc
}

func mustAddTextUnit(tb testing.TB, e *Engine, sessionID, extID string, docID uint64, content string, embedding []float32, tokenCount int) *types.TextUnit {
	tb.Helper()
	tu, err := e.AddTextUnit(sessionID, extID, docID, content, embedding, tokenCount)
	if err != nil {
		tb.Fatalf("AddTextUnit() error: %v", err)
	}
	return tu
}

func mustAddEntity(tb testing.TB, e *Engine, sessionID, extID, title, entType, description string, embedding []float32) *types.Entity {
	tb.Helper()
	ent, err := e.AddEntity(sessionID, extID, title, entType, description, embedding)
	if err != nil {
		tb.Fatalf("AddEntity() error: %v", err)
	}
	return ent
}

func mustAddRelationship(tb testing.TB, e *Engine, sessionID, extID string, sourceID, targetID uint64, relType, description string, weight float32) *types.Relationship {
	tb.Helper()
	rel, err := e.AddRelationship(sessionID, extID, sourceID, targetID, relType, description, weight)
	if err != nil {
		tb.Fatalf("AddRelationship() error: %v", err)
	}
	return rel
}
