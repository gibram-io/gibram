package client

import "testing"

func mustAddDocument(tb testing.TB, c *Client, extID, filename string) uint64 {
	tb.Helper()
	id, err := c.AddDocument(extID, filename)
	if err != nil {
		tb.Fatalf("AddDocument() error: %v", err)
	}
	return id
}

func mustAddTextUnit(tb testing.TB, c *Client, extID string, docID uint64, content string, embedding []float32, tokenCount int) uint64 {
	tb.Helper()
	id, err := c.AddTextUnit(extID, docID, content, embedding, tokenCount)
	if err != nil {
		tb.Fatalf("AddTextUnit() error: %v", err)
	}
	return id
}

func mustAddEntity(tb testing.TB, c *Client, extID, title, entType, description string, embedding []float32) uint64 {
	tb.Helper()
	id, err := c.AddEntity(extID, title, entType, description, embedding)
	if err != nil {
		tb.Fatalf("AddEntity() error: %v", err)
	}
	return id
}

func mustAddRelationship(tb testing.TB, c *Client, extID string, sourceID, targetID uint64, relType, description string, weight float32) uint64 {
	tb.Helper()
	id, err := c.AddRelationship(extID, sourceID, targetID, relType, description, weight)
	if err != nil {
		tb.Fatalf("AddRelationship() error: %v", err)
	}
	return id
}
