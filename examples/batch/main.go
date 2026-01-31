// Example: Batch Insert with GibRAM
// Demonstrates efficient bulk data loading with authentication

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

	sessionID := "batch-demo"

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

	fmt.Println("Starting batch insert...")
	start := time.Now()

	// Add 10,000 entities using bulk API
	numEntities := 10000
	batchSize := 100 // Reduced to 100 to stay under 4MB frame limit (1536 dims * 4 bytes * 100 = ~600KB)

	for batch := 0; batch < numEntities/batchSize; batch++ {
		var entities []types.BulkEntityInput

		for i := 0; i < batchSize; i++ {
			idx := batch*batchSize + i
			entities = append(entities, types.BulkEntityInput{
				ExternalID:  fmt.Sprintf("entity-%d", idx),
				Title:       fmt.Sprintf("ENTITY %d", idx),
				Type:        "concept",
				Description: fmt.Sprintf("This is test entity number %d", idx),
				Embedding:   mockEmbedding(idx),
			})
		}

		_, err := c.MSetEntities(entities)
		if err != nil {
			log.Printf("Failed to insert batch %d: %v", batch, err)
			continue
		}

		fmt.Printf("Inserted %d entities...\n", (batch+1)*batchSize)
	}

	elapsed := time.Since(start)

	// Verify
	info, err := c.Info()
	if err != nil {
		log.Fatalf("Info failed: %v", err)
	}

	fmt.Printf("\nBatch Insert Complete:\n")
	fmt.Printf("  Total Entities: %d\n", info.EntityCount)
	fmt.Printf("  Duration: %v\n", elapsed)
	fmt.Printf("  Throughput: %.0f entities/sec\n", float64(info.EntityCount)/elapsed.Seconds())
}

func mockEmbedding(seed int) []float32 {
	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32((seed*i)%1000) / 1000.0
	}
	return embedding
}
