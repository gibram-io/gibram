package store

import (
	"testing"

	"github.com/gibram-io/gibram/pkg/types"
)

func mustAddDocument(tb testing.TB, store *SessionStore, extID, filename string) *types.Document {
	tb.Helper()
	doc, err := store.AddDocument(extID, filename)
	if err != nil {
		tb.Fatalf("AddDocument() error: %v", err)
	}
	return doc
}

func mustAddTextUnit(tb testing.TB, store *SessionStore, extID string, docID uint64, content string, embedding []float32, tokenCount int) *types.TextUnit {
	tb.Helper()
	tu, err := store.AddTextUnit(extID, docID, content, embedding, tokenCount)
	if err != nil {
		tb.Fatalf("AddTextUnit() error: %v", err)
	}
	return tu
}

func mustAddEntity(tb testing.TB, store *SessionStore, extID, title, entType, description string, embedding []float32) *types.Entity {
	tb.Helper()
	ent, err := store.AddEntity(extID, title, entType, description, embedding)
	if err != nil {
		tb.Fatalf("AddEntity() error: %v", err)
	}
	return ent
}

func mustAddRelationship(tb testing.TB, store *SessionStore, extID string, sourceID, targetID uint64, relType, description string, weight float32) *types.Relationship {
	tb.Helper()
	rel, err := store.AddRelationship(extID, sourceID, targetID, relType, description, weight)
	if err != nil {
		tb.Fatalf("AddRelationship() error: %v", err)
	}
	return rel
}

func mustAddCommunity(tb testing.TB, store *SessionStore, extID, title, summary, fullContent string, level int, entityIDs, relIDs []uint64, embedding []float32) *types.Community {
	tb.Helper()
	comm, err := store.AddCommunity(extID, title, summary, fullContent, level, entityIDs, relIDs, embedding)
	if err != nil {
		tb.Fatalf("AddCommunity() error: %v", err)
	}
	return comm
}
