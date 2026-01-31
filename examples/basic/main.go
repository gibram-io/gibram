// Example: Basic GibRAM usage
// Demonstrates client connection with authentication, entity creation, relationships, and queries

package main

import (
	"fmt"
	"log"

	"github.com/gibram-io/gibram/pkg/client"
	"github.com/gibram-io/gibram/pkg/types"
)

func main() {
	// Connect to GibRAM server with authentication
	config := client.DefaultPoolConfig()
	config.APIKey = "" // No auth in insecure mode

	sessionID := "example-session"

	c, err := client.NewClientWithConfig("localhost:6161", sessionID, config)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("Close error: %v", err)
		}
	}()

	fmt.Println("✓ Connected to GibRAM server")

	// 1. Add documents
	docID, err := c.AddDocument("doc-001", "annual_report_2023.pdf")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Created document: annual_report_2023.pdf (ID: %d)\n", docID)

	// 2. Add text units (chunks) with embeddings
	embedding1 := getEmbedding("Bank Indonesia is the central bank of Indonesia")
	tu1ID, err := c.AddTextUnit("chunk-001", docID,
		"Bank Indonesia is the central bank of Indonesia",
		embedding1, 50)
	if err != nil {
		log.Fatal(err)
	}

	embedding2 := getEmbedding("The bank manages monetary policy and currency stability")
	tu2ID, err := c.AddTextUnit("chunk-002", docID,
		"The bank manages monetary policy and currency stability",
		embedding2, 55)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Created 2 text units\n")

	// 3. Add entities
	biEmbedding := getEmbedding("Bank Indonesia central bank organization")
	bankIndonesiaID, err := c.AddEntity("ent-001",
		"BANK INDONESIA", // uppercase for dedup
		"organization",
		"The central bank of Indonesia responsible for monetary policy",
		biEmbedding)
	if err != nil {
		log.Fatal(err)
	}

	policyEmbedding := getEmbedding("monetary policy economic regulation")
	monetaryPolicyID, err := c.AddEntity("ent-002",
		"MONETARY POLICY",
		"concept",
		"Economic policy regarding money supply and interest rates",
		policyEmbedding)
	if err != nil {
		log.Fatal(err)
	}

	indonesiaID, err := c.AddEntity("ent-003",
		"INDONESIA",
		"location",
		"Southeast Asian country and archipelago",
		nil) // No embedding for location
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Created 3 entities\n")

	// 4. Link text units to entities
	if err := c.LinkTextUnitToEntity(tu1ID, bankIndonesiaID); err != nil {
		log.Fatal(err)
	}
	if err := c.LinkTextUnitToEntity(tu2ID, bankIndonesiaID); err != nil {
		log.Fatal(err)
	}
	if err := c.LinkTextUnitToEntity(tu2ID, monetaryPolicyID); err != nil {
		log.Fatal(err)
	}

	// 5. Add relationships
	managesID, err := c.AddRelationship("rel-001",
		bankIndonesiaID, monetaryPolicyID,
		"MANAGES",
		"Bank Indonesia manages monetary policy",
		0.95)
	if err != nil {
		log.Fatal(err)
	}

	locatedInID, err := c.AddRelationship("rel-002",
		bankIndonesiaID, indonesiaID,
		"LOCATED_IN",
		"Bank Indonesia is located in Indonesia",
		1.0)
	if err != nil {
		log.Fatal(err)
	}

	manages, err := c.GetRelationship(managesID)
	if err != nil {
		log.Fatal(err)
	}
	locatedIn, err := c.GetRelationship(locatedInID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Created 2 relationships\n")
	fmt.Printf("  %s -> %s (%s)\n", "BANK INDONESIA", "MONETARY POLICY", manages.Type)
	fmt.Printf("  %s -> %s (%s)\n", "BANK INDONESIA", "INDONESIA", locatedIn.Type)

	// 6. Query
	queryEmbedding := getEmbedding("Indonesian central banking system")
	spec := types.DefaultQuerySpec()
	spec.QueryVector = queryEmbedding
	spec.TopK = 5
	spec.KHops = 2

	result, err := c.Query(spec)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nQuery Results:\n")
	fmt.Printf("Found %d entities, %d text units, %d relationships\n",
		len(result.Entities), len(result.TextUnits), len(result.Relationships))

	fmt.Println("\nTop Entities:")
	for i, entity := range result.Entities {
		fmt.Printf("  %d. %s (type: %s, similarity: %.3f)\n",
			i+1, entity.Entity.Title, entity.Entity.Type, entity.Similarity)
	}

	fmt.Println("\nTop Text Units:")
	for i, tu := range result.TextUnits {
		content := tu.TextUnit.Content
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		fmt.Printf("  %d. %s (similarity: %.3f)\n", i+1, content, tu.Similarity)
	}

	fmt.Println("\nRelationships:")
	for _, rel := range result.Relationships {
		fmt.Printf("  %s -> %s (%s)\n",
			rel.SourceTitle, rel.TargetTitle, rel.Relationship.Type)
	}

	// 7. Get session info
	info, err := c.Info()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nSession Info:\n")
	fmt.Printf("  Documents: %d\n", info.DocumentCount)
	fmt.Printf("  Text Units: %d\n", info.TextUnitCount)
	fmt.Printf("  Entities: %d\n", info.EntityCount)
	fmt.Printf("  Relationships: %d\n", info.RelationshipCount)
	fmt.Printf("  Communities: %d\n", info.CommunityCount)
}

// Mock embedding function (replace with actual OpenAI/etc call)
func getEmbedding(text string) []float32 {
	// In production, call your embedding model
	// e.g., OpenAI ada-002, Cohere, etc.
	embedding := make([]float32, 1536)

	// Simple hash-based mock for demo
	hash := 0
	for _, c := range text {
		hash = hash*31 + int(c)
	}

	for i := range embedding {
		hash = hash*1103515245 + 12345
		embedding[i] = float32(hash%1000) / 1000.0
	}

	return embedding
}
