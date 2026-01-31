// Example: Large Dataset Query
// Demonstrates handling large datasets efficiently

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/gibram-io/gibram/pkg/client"
	"github.com/gibram-io/gibram/pkg/types"
)

func main() {
	// Connect to GibRAM server with authentication
	config := client.DefaultPoolConfig()
	config.APIKey = "" // No auth in insecure mode

	sessionID := "large-dataset-demo"

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

	// Populate with dataset using bulk API
	fmt.Println("Populating dataset...")
	numEntities := 5000 // Reduced from 50k for faster demo
	batchSize := 100    // Reduced to stay under 4MB frame limit
	start := time.Now()

	for batch := 0; batch < numEntities/batchSize; batch++ {
		var entities []types.BulkEntityInput

		for i := 0; i < batchSize; i++ {
			idx := batch*batchSize + i
			embedding := make([]float32, 1536)
			for j := range embedding {
				embedding[j] = float32((idx*j)%1000) / 1000.0
			}

			entities = append(entities, types.BulkEntityInput{
				ExternalID:  fmt.Sprintf("ent-%d", idx),
				Title:       fmt.Sprintf("ENTITY %d", idx),
				Type:        "concept",
				Description: "Test entity",
				Embedding:   embedding,
			})
		}

		_, err := c.MSetEntities(entities)
		if err != nil {
			log.Printf("Failed to insert batch %d: %v", batch, err)
			continue
		}

		if ((batch+1)*batchSize)%1000 == 0 {
			fmt.Printf("  Loaded %d entities...\n", (batch+1)*batchSize)
		}
	}

	loadTime := time.Since(start)
	fmt.Printf("Loaded %d entities in %v\n", numEntities, loadTime)

	// Query
	query := make([]float32, 1536)
	for i := range query {
		query[i] = 0.5
	}

	spec := types.DefaultQuerySpec()
	spec.QueryVector = query
	spec.TopK = 100 // Return top 100 results

	fmt.Println("\nExecuting query...")
	queryStart := time.Now()

	result, err := c.Query(spec)
	if err != nil {
		log.Fatal(err)
	}

	queryTime := time.Since(queryStart)

	fmt.Printf("\nQuery Complete:\n")
	fmt.Printf("  Query Time: %v\n", queryTime)
	fmt.Printf("  Entities Returned: %d\n", len(result.Entities))
	fmt.Printf("  Text Units Returned: %d\n", len(result.TextUnits))

	// Show top 5 results
	fmt.Println("\nTop 5 Results:")
	for i := 0; i < 5 && i < len(result.Entities); i++ {
		entity := result.Entities[i]
		fmt.Printf("  %d. %s (similarity: %.3f)\n", i+1, entity.Entity.Title, entity.Similarity)
	}

	// Get session info
	info, err := c.Info()
	if err != nil {
		log.Fatalf("Info failed: %v", err)
	}
	fmt.Printf("\nSession Info:\n")
	fmt.Printf("  Entities: %d\n", info.EntityCount)
	fmt.Printf("  Relationships: %d\n", info.RelationshipCount)
}
