// GibRAM CLI - Interactive command-line client
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gibram-io/gibram/pkg/client"
	"github.com/gibram-io/gibram/pkg/types"
	"github.com/gibram-io/gibram/pkg/version"
)

func main() {
	host := flag.String("h", "localhost:6161", "Server address")
	useTLS := flag.Bool("tls", true, "Use TLS (default: true)")
	skipVerify := flag.Bool("insecure", true, "Skip TLS certificate verification (default: true for self-signed)")
	apiKey := flag.String("key", "", "API key for authentication")
	flag.Parse()

	fmt.Println("╔═══════════════════════════════════════╗")
	fmt.Printf("║         GibRAM CLI v%-7s        ║\n", version.Version)
	fmt.Println("║     Type 'help' for commands          ║")
	fmt.Println("╚═══════════════════════════════════════╝")
	fmt.Println()

	// Connect with TLS config
	config := client.DefaultPoolConfig()
	config.TLSEnabled = *useTLS
	config.TLSSkipVerify = *skipVerify
	config.APIKey = *apiKey

	c, err := client.NewClientWithConfig(*host, "cli-session", config)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := c.Close(); err != nil {
			fmt.Printf("Close error: %v\n", err)
		}
	}()

	fmt.Printf("Connected to %s\n\n", *host)

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("gibram %s> ", *host)
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := strings.ToUpper(parts[0])
		args := parts[1:]

		switch cmd {
		case "QUIT", "EXIT":
			fmt.Println("Bye!")
			return

		case "HELP":
			printHelp()

		case "PING":
			start := time.Now()
			if err := c.Ping(); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("PONG (%v)\n", time.Since(start))
			}

		case "INFO":
			info, err := c.Info()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Println("┌─────────────────────────────────────┐")
			fmt.Printf("│ Server: GraphMemoryRAG v%s      │\n", info.Version)
			fmt.Println("├─────────────────────────────────────┤")
			fmt.Printf("│ Documents:     %-5d                │\n", info.DocumentCount)
			fmt.Printf("│ TextUnits:     %-5d                │\n", info.TextUnitCount)
			fmt.Printf("│ Entities:      %-5d                │\n", info.EntityCount)
			fmt.Printf("│ Relationships: %-5d                │\n", info.RelationshipCount)
			fmt.Printf("│ Communities:   %-5d                │\n", info.CommunityCount)
			fmt.Printf("│ VectorDim:     %-5d                │\n", info.VectorDim)
			fmt.Println("└─────────────────────────────────────┘")

		case "ADDDOC":
			// ADDDOC <ext_id> <filename>
			if len(args) < 2 {
				fmt.Println("Usage: ADDDOC <ext_id> <filename>")
				continue
			}
			id, err := c.AddDocument(args[0], args[1])
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("OK (doc_id: %d)\n", id)
			}

		case "ADDTEXTUNIT", "ADDTU":
			// ADDTU <ext_id> <doc_id> <content>
			if len(args) < 3 {
				fmt.Println("Usage: ADDTU <ext_id> <doc_id> <content...>")
				continue
			}
			docID, _ := strconv.ParseUint(args[1], 10, 64)
			content := strings.Join(args[2:], " ")

			// Generate random embedding for testing
			embedding := randomEmbedding(1536)

			id, err := c.AddTextUnit(args[0], docID, content, embedding, len(content)/4)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("OK (textunit_id: %d)\n", id)
			}

		case "ADDENTITY", "ADDENT":
			// ADDENT <ext_id> <title> <type> <description...>
			if len(args) < 4 {
				fmt.Println("Usage: ADDENT <ext_id> <title> <type> <description...>")
				continue
			}
			description := strings.Join(args[3:], " ")

			// Generate random embedding for testing
			embedding := randomEmbedding(1536)

			id, err := c.AddEntity(args[0], args[1], args[2], description, embedding)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("OK (entity_id: %d)\n", id)
			}

		case "GETENT":
			// GETENT <id>
			if len(args) < 1 {
				fmt.Println("Usage: GETENT <id>")
				continue
			}
			id, _ := strconv.ParseUint(args[0], 10, 64)
			ent, err := c.GetEntity(id)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				data, _ := json.MarshalIndent(ent, "", "  ")
				fmt.Println(string(data))
			}

		case "GETENTBYTITLE":
			// GETENTBYTITLE <title>
			if len(args) < 1 {
				fmt.Println("Usage: GETENTBYTITLE <title>")
				continue
			}
			title := strings.Join(args, " ")
			ent, err := c.GetEntityByTitle(title)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				data, _ := json.MarshalIndent(ent, "", "  ")
				fmt.Println(string(data))
			}

		case "ADDREL":
			// ADDREL <source_id> <target_id> <type> [description...]
			if len(args) < 3 {
				fmt.Println("Usage: ADDREL <source_id> <target_id> <type> [description...]")
				continue
			}
			sourceID, _ := strconv.ParseUint(args[0], 10, 64)
			targetID, _ := strconv.ParseUint(args[1], 10, 64)
			relType := args[2]
			description := ""
			if len(args) > 3 {
				description = strings.Join(args[3:], " ")
			}

			id, err := c.AddRelationship("", sourceID, targetID, relType, description, 1.0)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Printf("OK (rel_id: %d)\n", id)
			}

		case "LINK":
			// LINK <textunit_id> <entity_id>
			if len(args) < 2 {
				fmt.Println("Usage: LINK <textunit_id> <entity_id>")
				continue
			}
			tuID, _ := strconv.ParseUint(args[0], 10, 64)
			entID, _ := strconv.ParseUint(args[1], 10, 64)
			if err := c.LinkTextUnitToEntity(tuID, entID); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Println("OK")
			}

		case "COMMUNITY":
			// COMMUNITY COMPUTE [resolution]
			// COMMUNITY LIST
			if len(args) < 1 {
				fmt.Println("Usage: COMMUNITY COMPUTE [resolution] | COMMUNITY LIST")
				continue
			}
			subCmd := strings.ToUpper(args[0])
			switch subCmd {
			case "COMPUTE":
				resolution := 1.0
				if len(args) > 1 {
					resolution, _ = strconv.ParseFloat(args[1], 64)
				}
				result, err := c.ComputeCommunities(resolution, 10)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Printf("OK - Found %d communities\n", result.Count)
					for _, comm := range result.Communities {
						fmt.Printf("  [%d] %s (%d entities)\n", comm.ID, comm.Title, len(comm.EntityIDs))
					}
				}
			default:
				fmt.Println("Usage: COMMUNITY COMPUTE [resolution]")
			}

		case "QUERY":
			// QUERY <topK> <hops> [max_entities] [max_textunits]
			if len(args) < 2 {
				fmt.Println("Usage: QUERY <topK> <hops> [max_entities] [max_textunits]")
				continue
			}
			topK, _ := strconv.Atoi(args[0])
			hops, _ := strconv.Atoi(args[1])
			maxEnts := 50
			maxTUs := 10
			if len(args) > 2 {
				maxEnts, _ = strconv.Atoi(args[2])
			}
			if len(args) > 3 {
				maxTUs, _ = strconv.Atoi(args[3])
			}

			// Generate random query vector for testing
			queryVec := randomEmbedding(1536)

			spec := types.QuerySpec{
				QueryVector:    queryVec,
				SearchTypes:    []types.SearchType{types.SearchTypeTextUnit, types.SearchTypeEntity, types.SearchTypeCommunity},
				TopK:           topK,
				KHops:          hops,
				MaxEntities:    maxEnts,
				MaxTextUnits:   maxTUs,
				MaxCommunities: 5,
			}

			result, err := c.Query(spec)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			fmt.Printf("Query ID: %d\n", result.QueryID)
			fmt.Printf("Stats: %d textunits, %d entities, %d communities, %dμs\n",
				len(result.TextUnits), len(result.Entities), len(result.Communities), result.Stats.DurationMicros)

			if len(result.TextUnits) > 0 {
				fmt.Println("TextUnits:")
				for i, tu := range result.TextUnits {
					content := tu.TextUnit.Content
					if len(content) > 60 {
						content = content[:60] + "..."
					}
					fmt.Printf("  %d. [id=%d hop=%d sim=%.3f] %s\n", i+1, tu.TextUnit.ID, tu.Hop, tu.Similarity, content)
				}
			}

			if len(result.Entities) > 0 {
				fmt.Println("Entities:")
				for i, ent := range result.Entities {
					if i >= 5 {
						fmt.Printf("  ... and %d more\n", len(result.Entities)-5)
						break
					}
					fmt.Printf("  - %s (%s) [hop=%d]\n", ent.Entity.Title, ent.Entity.Type, ent.Hop)
				}
			}

			if len(result.Relationships) > 0 {
				fmt.Println("Relationships:")
				for i, rel := range result.Relationships {
					if i >= 5 {
						fmt.Printf("  ... and %d more\n", len(result.Relationships)-5)
						break
					}
					fmt.Printf("  - %s -[%s]-> %s\n", rel.SourceTitle, rel.Relationship.Type, rel.TargetTitle)
				}
			}

		case "EXPLAIN":
			// EXPLAIN <query_id>
			if len(args) < 1 {
				fmt.Println("Usage: EXPLAIN <query_id>")
				continue
			}
			queryID, _ := strconv.ParseUint(args[0], 10, 64)
			explain, err := c.Explain(queryID)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			fmt.Printf("Query ID: %d\n", explain.QueryID)
			fmt.Printf("\nSeeds (%d):\n", len(explain.Seeds))
			for i, seed := range explain.Seeds {
				if i >= 5 {
					fmt.Printf("  ... and %d more\n", len(explain.Seeds)-5)
					break
				}
				fmt.Printf("  - [%s] id=%d ext=%s sim=%.3f\n", seed.Type, seed.ID, seed.ExternalID, seed.Similarity)
			}
			fmt.Printf("\nTraversal (%d steps):\n", len(explain.Traversal))
			for i, step := range explain.Traversal {
				if i >= 10 {
					fmt.Printf("  ... and %d more\n", len(explain.Traversal)-10)
					break
				}
				fmt.Printf("  Hop %d: %d -[%s]-> %d (weight=%.2f)\n",
					step.Hop, step.FromEntityID, step.RelType, step.ToEntityID, step.Weight)
			}

		// DEPRECATED: TTL commands removed - session-level management only
		/*
			case "SETTTL":
				// SETTTL <type> <id> <ttl_seconds>
				if len(args) < 3 {
					fmt.Println("Usage: SETTTL <document|textunit|entity|community> <id> <ttl_seconds>")
					continue
				}
				itemType := types.ItemType(strings.ToLower(args[0]))
				id, _ := strconv.ParseUint(args[1], 10, 64)
				ttl, _ := strconv.ParseInt(args[2], 10, 64)
				if err := c.SetTTL(itemType, id, ttl); err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Println("OK")
				}

			case "TTL":
				// TTL <type> <id>
				if len(args) < 2 {
					fmt.Println("Usage: TTL <document|textunit|entity|community> <id>")
					continue
				}
				itemType := types.ItemType(strings.ToLower(args[0]))
				id, _ := strconv.ParseUint(args[1], 10, 64)
				ttl, err := c.GetTTL(itemType, id)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					if ttl == -2 {
						fmt.Println("(not found)")
					} else if ttl == -1 {
						fmt.Println("(no expiry)")
					} else {
						fmt.Printf("%d seconds\n", ttl)
					}
				}
		*/

		case "SNAPSHOT", "SAVE":
			if err := c.Save(""); err != nil {
				fmt.Printf("Error: %v\n", err)
			} else {
				fmt.Println("OK - Snapshot saved")
			}

		default:
			fmt.Printf("Unknown command: %s (type 'help' for commands)\n", cmd)
		}
	}
}

func printHelp() {
	fmt.Println(`Commands:
  PING                                    Check connection
  INFO                                    Server statistics

  ADDDOC <ext_id> <filename> [ttl]        Add document
  ADDTU <ext_id> <doc_id> <content>       Add text unit (chunk)
  ADDENT <ext_id> <title> <type> <desc>   Add entity
  ADDREL <src_id> <tgt_id> <type> [desc]  Add relationship
  LINK <textunit_id> <entity_id>          Link text unit to entity

  GETENT <id>                             Get entity by ID
  GETENTBYTITLE <title>                   Get entity by title

  COMMUNITY COMPUTE [resolution]          Compute communities (Leiden)

  QUERY <topK> <hops> [maxEnts] [maxTUs]  Vector + graph query
  EXPLAIN <query_id>                      Explain query path

  SETTTL <type> <id> <seconds>            Set TTL
  TTL <type> <id>                         Get remaining TTL

  SNAPSHOT                                Force snapshot
  HELP                                    Show this help
  QUIT                                    Exit`)
}

func randomEmbedding(dim int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = rand.Float32()*2 - 1
	}
	// Normalize
	var sum float32
	for _, v := range vec {
		sum += v * v
	}
	if sum > 0 {
		norm := float32(1.0 / float64(sum))
		for i := range vec {
			vec[i] *= norm
		}
	}
	return vec
}
