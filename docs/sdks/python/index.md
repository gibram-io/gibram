# Python SDK (v0.1.0)

The GibRAM Python SDK provides a GraphRAG-style workflow for indexing documents and querying knowledge graphs with minimal code.

## What is the Python SDK?

A high-level library that automates the GraphRAG pipeline:

```
Documents → Chunk → Extract → Embed → Store → Query
```

Instead of manually handling each step, you write:

```python
from gibram import GibRAMIndexer

indexer = GibRAMIndexer(session_id="my-project")
stats = indexer.index_documents(["Your document text here"])
result = indexer.query("Your question")
```

The SDK handles: ✅ Text chunking (with overlap) • ✅ Entity & relationship extraction (via LLM) • ✅ Embedding generation (via OpenAI) • ✅ Graph storage (via GibRAM protocol) • ✅ Community detection (hierarchical clustering) • ✅ Hybrid query (vector + graph traversal)

## When to Use the SDK

**Use the Python SDK when**:
- You want GraphRAG workflow out-of-the-box
- You're building RAG applications
- You need automatic entity extraction
- You want customizable components (chunker, extractor, embedder)

**Don't use the SDK when**:
- You need custom protocol operations (use low-level client)
- You're building non-Python applications (use Go client or implement protocol)
- You have pre-extracted entities (use protocol directly)

## Architecture

### High-Level vs Low-Level

**High-Level (`GibRAMIndexer`)**:
```python
# All-in-one workflow
indexer = GibRAMIndexer(session_id="my-project")
stats = indexer.index_documents(documents)
```

**Low-Level (`_Client`)**:
```python
# Manual protocol operations
from gibram._client import _Client

client = _Client(host="localhost", port=6161, session_id="test")
client.connect()

doc_id = client.add_document(external_id="doc-001", filename="file.txt")
entity_id = client.add_entity(
    external_id="ent-001",
    title="EINSTEIN",
    entity_type="person",
    description="...",
    embedding=[0.1, 0.2, ...]  # You provide embedding
)
```

**When to use low-level**: Custom workflows, pre-computed embeddings, fine-grained control.

### Component Architecture

The SDK is modular. You can swap components:

```python
from gibram import GibRAMIndexer
from gibram.chunkers import TokenChunker
from gibram.extractors import OpenAIExtractor
from gibram.embedders import OpenAIEmbedder

# Default (OpenAI for everything)
indexer = GibRAMIndexer(session_id="my-project")

# Custom components
indexer = GibRAMIndexer(
    session_id="my-project",
    chunker=TokenChunker(chunk_size=1024, chunk_overlap=100),
    extractor=OpenAIExtractor(model="gpt-4o-mini"),
    embedder=OpenAIEmbedder(model="text-embedding-3-large", dimensions=3072)
)
```

**Interfaces**:
- `BaseChunker` - Splits text into chunks
- `BaseExtractor` - Extracts entities and relationships
- `BaseEmbedder` - Generates embeddings

See [Custom Components](advanced/custom-components.md) for implementation guide.

## Installation

```bash
pip install gibram
```

**Requirements**:
- Python 3.8+
- GibRAM server running (see [Getting Started](../../getting-started/server.md))

**Dependencies** (auto-installed):
- `protobuf` - Protocol communication
- `openai` - Default LLM and embeddings
- `tqdm` - Progress bars

## Quick Example

```python
from gibram import GibRAMIndexer

# Initialize
indexer = GibRAMIndexer(
    session_id="my-project",
    llm_api_key="sk-...",  # Or set OPENAI_API_KEY
)

# Index
stats = indexer.index_documents([
    "Albert Einstein developed the theory of relativity.",
    "He received the Nobel Prize in Physics in 1921.",
])

print(f"Entities extracted: {stats.entities_extracted}")
print(f"Relationships: {stats.relationships_extracted}")

# Query
result = indexer.query("Einstein's achievements", top_k=5)

for entity in result.entities:
    print(f"{entity.title}: {entity.score:.3f}")
```

## Key Concepts

### Session Isolation

Each `GibRAMIndexer` instance operates in an isolated **session**:

```python
# Project A data
indexer_a = GibRAMIndexer(session_id="project-a")
indexer_a.index_documents(docs_a)

# Project B data (completely separate)
indexer_b = GibRAMIndexer(session_id="project-b")
indexer_b.index_documents(docs_b)

# Queries only see data from their session
result_a = indexer_a.query("query")  # Only searches project-a data
```

**Best Practice**: Use one session per project or experiment.

### Indexing Pipeline

When you call `index_documents()`:

1. **Chunking**: Splits documents into ~512 token chunks (configurable)
2. **Extraction**: Calls LLM to extract entities and relationships per chunk
3. **Deduplication**: Merges entities with same title across chunks
4. **Embedding**: Generates embeddings for chunks and entities
5. **Storage**: Stores in GibRAM server via protocol
6. **Linking**: Links entities to their source chunks
7. **Clustering**: Detects communities (if enabled)

**Cost**: Each chunk = 1 LLM call + embeddings. See [Quickstart](quickstart.md) for estimation.

### Query Execution

When you call `query()`:

1. **Embedding**: Generates query embedding
2. **Vector Search**: Finds similar chunks, entities, communities
3. **Graph Traversal**: Expands via relationships (k-hops)
4. **Ranking**: Combines similarity scores
5. **Return**: Sorted results with scores

**Results include**:
- `entities` - Relevant entities (with descriptions)
- `text_units` - Source chunks (with content)
- `communities` - Cluster summaries (if available)

## Configuration

### Essential Parameters

```python
indexer = GibRAMIndexer(
    session_id="required-unique-id",  # REQUIRED
    host="localhost",                 # Server host
    port=6161,                        # Server port
)
```

### LLM Configuration

```python
indexer = GibRAMIndexer(
    llm_provider="openai",       # Only OpenAI in v0.1.0
    llm_api_key="sk-...",        # Or env OPENAI_API_KEY
    llm_model="gpt-4o",          # Default: gpt-4o
)
```

**Supported models**:
- `gpt-4o` - Best quality (default)
- `gpt-4o-mini` - Faster, cheaper
- `gpt-4-turbo` - Alternative

### Embedding Configuration

```python
indexer = GibRAMIndexer(
    embedding_provider="openai",
    embedding_model="text-embedding-3-small",  # Default
    embedding_dimensions=1536,                 # MUST match server
)
```

**⚠️ CRITICAL**: `embedding_dimensions` must match server `vector_dim`.

**Supported models**:
- `text-embedding-3-small` - 1536 dims (default)
- `text-embedding-3-large` - 3072 dims (higher quality)
- `text-embedding-ada-002` - 1536 dims (legacy)

### Chunking Configuration

```python
indexer = GibRAMIndexer(
    chunk_size=512,      # Tokens per chunk
    chunk_overlap=50,    # Overlap between chunks
)
```

**Trade-offs**:
- Larger chunks: Fewer LLM calls (cheaper), but less precise retrieval
- Smaller chunks: More LLM calls (costlier), but more precise retrieval

### Community Detection

```python
indexer = GibRAMIndexer(
    auto_detect_communities=True,  # Default: True
    community_resolution=1.0,      # Higher = more granular clusters
)
```

## Error Handling

```python
from gibram.exceptions import (
    GibRAMError,
    ConfigurationError,
    ConnectionError,
    ExtractionError,
    EmbeddingError,
)

try:
    indexer = GibRAMIndexer(session_id="test")
    stats = indexer.index_documents(documents)
except ConfigurationError as e:
    print(f"Configuration issue: {e}")
except ConnectionError as e:
    print(f"Can't reach server: {e}")
except ExtractionError as e:
    print(f"LLM extraction failed: {e}")
except EmbeddingError as e:
    print(f"Embedding generation failed: {e}")
```

## Performance

**Typical throughput**:
- Indexing: ~5-10 documents/minute (depends on LLM API latency)
- Querying: ~100-200ms per query

**Bottlenecks**:
1. LLM API calls (dominant)
2. Embedding API calls
3. Network latency to GibRAM server

**Optimization tips**:
- Increase `batch_size` for faster embedding calls
- Use `gpt-4o-mini` instead of `gpt-4o` for faster extraction
- Disable `auto_detect_communities` if not needed

## Next Steps

- **[Quickstart](quickstart.md)** - Step-by-step examples
- **[Indexing Workflow](workflow-indexing.md)** - Deep dive into indexing
- **[Query Workflow](workflow-query.md)** - Deep dive into querying
- **[Custom Components](advanced/custom-components.md)** - Build your own chunker/extractor/embedder
