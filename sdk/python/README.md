# GibRAM Python SDK v0.1.0

GraphRAG-style knowledge graph indexing with automatic entity extraction, relationship detection, and community discovery.

## Installation

```bash
cd sdk/python
pip install -e .
```

## Quick Start

```python
from gibram import GibRAMIndexer

# Initialize indexer with OpenAI
indexer = GibRAMIndexer(
    session_id="my-project",
    llm_api_key="sk-...",  # or set OPENAI_API_KEY env
)

# Index documents (automatic chunking, extraction, embedding)
stats = indexer.index_documents([
    "Einstein was born in 1879 in Ulm, Germany.",
    "He developed the theory of relativity in 1905.",
    "Einstein received the Nobel Prize in Physics in 1921.",
])

print(f"Indexed {stats.entities_extracted} entities in {stats.indexing_time_seconds:.2f}s")

# Query knowledge graph
result = indexer.query("Einstein's achievements", top_k=5)

for entity in result.entities:
    print(f"{entity.title} ({entity.type}): {entity.description}")
```

## Configuration

### Environment Variables

```bash
export OPENAI_API_KEY="sk-..."
```

### Initialization Parameters

```python
indexer = GibRAMIndexer(
    # Required
    session_id="unique-project-id",
    
    # Server connection
    host="localhost",
    port=6161,
    
    # LLM configuration
    llm_provider="openai",           # Only OpenAI supported in v0.1.0
    llm_api_key="sk-...",            # Auto-detect from OPENAI_API_KEY
    llm_model="gpt-4o",              # GPT-4o recommended
    
    # Embedding configuration
    embedding_provider="openai",
    embedding_model="text-embedding-3-small",
    embedding_dimensions=1536,       # Must match server config
    
    # Chunking configuration
    chunk_size=512,                  # Tokens per chunk
    chunk_overlap=50,                # Overlap between chunks
    
    # Community detection
    auto_detect_communities=True,    # Auto-run after indexing
    community_resolution=1.0,        # Leiden algorithm resolution
)
```

## API Reference

### GibRAMIndexer

Main class for indexing and querying.

#### `index_documents(documents, batch_size=10, show_progress=True) -> IndexStats`

Index documents into knowledge graph.

**Arguments:**
- `documents`: List of strings or dicts `{"id": ..., "text": ..., "metadata": ...}`
- `batch_size`: Batch size for LLM/API calls (default: 10)
- `show_progress`: Show progress bar (default: True)

**Returns:** `IndexStats` with counts and timing

**Pipeline:**
1. Chunk documents → TextUnits
2. Extract entities & relationships (LLM)
3. Generate embeddings
4. Store in graph
5. Link entities to text units
6. Detect communities (if enabled)

**Example:**
```python
stats = indexer.index_documents([
    {"id": "doc1", "text": "...", "metadata": {"source": "wiki"}},
    {"id": "doc2", "text": "..."},
])
```

#### `query(query, mode="local", top_k=10, include_entities=True, include_text_units=True, include_communities=False) -> QueryResult`

Query knowledge graph.

**Arguments:**
- `query`: Natural language query
- `mode`: Query mode (v0.1.0 only supports "local")
- `top_k`: Number of results (default: 10)
- `include_entities`: Include entity results
- `include_text_units`: Include text unit results
- `include_communities`: Include community results

**Returns:** `QueryResult` with scored results

**Example:**
```python
result = indexer.query("machine learning applications", top_k=5)

for entity in result.entities:
    print(f"{entity.title}: {entity.score:.3f}")

for text_unit in result.text_units:
    print(f"{text_unit.content[:100]}... (score: {text_unit.score:.3f})")
```

#### `get_stats() -> IndexStats`

Get current indexing statistics.

#### `close()`

Close connection to server.

### Types

#### `IndexStats`

```python
@dataclass
class IndexStats:
    documents_indexed: int = 0
    text_units_created: int = 0
    entities_extracted: int = 0
    relationships_extracted: int = 0
    communities_detected: int = 0
    indexing_time_seconds: float = 0.0
```

#### `QueryResult`

```python
@dataclass
class QueryResult:
    entities: List[ScoredEntity]
    text_units: List[ScoredTextUnit]
    communities: List[ScoredCommunity]
    execution_time_ms: float
```

#### `ScoredEntity`

```python
@dataclass
class ScoredEntity:
    id: int
    title: str
    type: str
    description: str
    score: float  # Similarity score
```

### Exceptions

All exceptions inherit from `GibRAMError`:

- `ConnectionError`: Server connection failed
- `TimeoutError`: Operation timed out
- `ProtocolError`: Protocol encoding/decoding error
- `ServerError`: Server returned error
- `NotFoundError`: Resource not found
- `ValidationError`: Input validation failed
- `ExtractionError`: LLM extraction failed
- `EmbeddingError`: Embedding generation failed
- `ConfigurationError`: Invalid configuration

## Advanced Usage

### Custom Extractors

Implement `BaseExtractor` for custom entity/relationship extraction:

```python
from gibram.extractors import BaseExtractor
from gibram.types import ExtractedEntity, ExtractedRelationship

class MyExtractor(BaseExtractor):
    def extract(self, text: str) -> tuple[list[ExtractedEntity], list[ExtractedRelationship]]:
        # Your custom logic
        entities = [...]
        relationships = [...]
        return entities, relationships

indexer = GibRAMIndexer(
    session_id="custom",
    extractor=MyExtractor(),
    embedder=...,  # Still need embedder
)
```

### Custom Embedders

Implement `BaseEmbedder` for custom embeddings:

```python
from gibram.embedders import BaseEmbedder

class MyEmbedder(BaseEmbedder):
    def embed(self, texts: list[str]) -> list[list[float]]:
        # Your custom logic
        return [[0.1, 0.2, ...], ...]
    
    def embed_single(self, text: str) -> list[float]:
        return self.embed([text])[0]

indexer = GibRAMIndexer(
    session_id="custom",
    embedder=MyEmbedder(),
)
```

### Context Manager

Use context manager for automatic cleanup:

```python
with GibRAMIndexer(session_id="project") as indexer:
    stats = indexer.index_documents(documents)
    result = indexer.query("some query")
    # Connection automatically closed
```

## Requirements

- Python 3.8+
- GibRAM server running (Docker recommended)
- OpenAI API key (for extraction & embeddings)

## Server Setup

Start GibRAM server with Docker:

```bash
docker run -d \
  --name gibram-server \
  -p 6161:6161 \
  -e EMBEDDING_DIM=1536 \
  gibram:latest
```

## License

MIT

## Version

v0.1.0 - Initial release with OpenAI extraction & embeddings
