# GibRAM

**Graph in-Buffer Retrieval & Associative Memory**

High-performance in-memory knowledge graph server designed for GraphRAG applications. GibRAM combines graph-based retrieval with vector search to enable fast, context-aware question answering over large document collections.

Built for applications that need to extract knowledge from unstructured text, understand relationships between entities, and answer complex queries that require both semantic similarity and graph traversal.

## Features

### 🗄️ **Rich Graph Storage**
- **Entities**: Named entities with types, descriptions, and embeddings
- **Relationships**: Typed, weighted connections between entities  
- **Communities**: Auto-detected entity clusters via Leiden algorithm
- **Text Units**: Document chunks linked to extracted entities
- **TTL-based eviction**: Automatic cleanup with configurable session lifetime

### 🚀 **High-Performance Vector Search**
- **HNSW indexing** for sub-millisecond semantic search across 100K+ vectors
- Concurrent query execution with sub-2ms P50 latency
- Automatic index building with configurable parameters (M, efConstruction)
- Support for 1-5M entities per session with efficient memory management

### ⚡ **Low-Latency Binary Protocol**
- Custom Protobuf-based protocol (codec 0x01) for minimal overhead
- 3x faster than JSON, 50% smaller payload size
- Frame-based message format with length prefixing
- Support for batch operations and streaming responses

### 🐍 **Python SDK with Automatic Extraction**
- **GraphRAG-style workflow**: Index documents with one function call
- **OpenAI GPT-4 integration**: Automatic entity and relationship extraction
- **Pluggable architecture**: Swap chunkers, extractors, or embedders
- **Type-safe API**: Full type hints and dataclass-based responses
- **Error handling**: Automatic retries with exponential backoff

## Quick Start

### Install via Binary

```bash
# Install via script
curl -fsSL https://gibram.io/install.sh | sh

# Run server
gibram-server --insecure
```

Server runs on port **6161** by default.

### Install via Docker

```bash
# Run server
docker run -p 6161:6161 gibramio/gibram:latest

# With custom config
docker-compose up -d
```

### Python SDK

```bash
pip install gibram
```

**Basic Usage:**

```python
from gibram import GibRAMIndexer

# Initialize indexer
indexer = GibRAMIndexer(
    session_id="my-project",
    host="localhost",
    port=6161,
    llm_api_key="sk-..."  # or set OPENAI_API_KEY env
)

# Index documents
stats = indexer.index_documents([
    "Python is a programming language created by Guido van Rossum.",
    "JavaScript was created by Brendan Eich at Netscape in 1995."
])

print(f"Entities: {stats.entities_extracted}")
print(f"Relationships: {stats.relationships_extracted}")

# Query
results = indexer.query("Who created JavaScript?", top_k=3)
for entity in results.entities:
    print(f"{entity.title}: {entity.score}")
```

**Custom Components:**

```python
from gibram import GibRAMIndexer
from gibram.chunkers import TokenChunker
from gibram.extractors import OpenAIExtractor
from gibram.embedders import OpenAIEmbedder

indexer = GibRAMIndexer(
    session_id="custom-project",
    chunker=TokenChunker(chunk_size=512, chunk_overlap=50),
    extractor=OpenAIExtractor(model="gpt-4o", api_key="..."),
    embedder=OpenAIEmbedder(model="text-embedding-3-small", api_key="...")
)
```

## License

MIT
