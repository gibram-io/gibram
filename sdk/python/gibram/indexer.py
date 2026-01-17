"""GibRAM Indexer - GraphRAG-style knowledge graph indexing."""

import os
import time
import uuid
from typing import List, Union, Dict, Any, Optional, Literal
from tqdm import tqdm

from ._client import _Client
from .types import (
    IndexStats,
    QueryResult,
    ScoredEntity,
    ScoredTextUnit,
    ScoredCommunity,
    ExtractedEntity,
    ExtractedRelationship,
)
from .exceptions import ConfigurationError, GibRAMError
from .chunkers import BaseChunker, TokenChunker
from .extractors import BaseExtractor, OpenAIExtractor
from .embedders import BaseEmbedder, OpenAIEmbedder


class GibRAMIndexer:
    """
    GraphRAG-style indexer for GibRAM knowledge graph.
    
    Automatically handles:
    - Text chunking
    - Entity & relationship extraction (via LLM)
    - Embedding generation
    - Graph storage
    - Community detection
    
    Example:
        >>> indexer = GibRAMIndexer(session_id="my-project")
        >>> stats = indexer.index_documents(["Einstein was born in 1879..."])
        >>> result = indexer.query("Einstein's theories")
    """

    def __init__(
        self,
        session_id: str,
        host: str = "localhost",
        port: int = 6161,
        # LLM Configuration
        llm_provider: Literal["openai"] = "openai",
        llm_api_key: Optional[str] = None,
        llm_model: Optional[str] = None,
        # Embedding Configuration
        embedding_provider: Literal["openai"] = "openai",
        embedding_api_key: Optional[str] = None,
        embedding_model: str = "text-embedding-3-small",
        embedding_dimensions: int = 1536,
        # Chunking Configuration
        chunk_size: int = 512,
        chunk_overlap: int = 50,
        # Community Detection
        auto_detect_communities: bool = True,
        community_resolution: float = 1.0,
        # Advanced (custom implementations)
        extractor: Optional[BaseExtractor] = None,
        embedder: Optional[BaseEmbedder] = None,
        chunker: Optional[BaseChunker] = None,
    ):
        """
        Initialize GibRAM indexer.

        Args:
            session_id: Unique session identifier (required - isolates data per project)
            host: GibRAM server host
            port: GibRAM server port
            
            llm_provider: LLM provider for extraction ("openai")
            llm_api_key: API key (auto-detect from OPENAI_API_KEY if None)
            llm_model: Model name (default: gpt-4o for openai)
            
            embedding_provider: Embedding provider
            embedding_api_key: API key (default: same as llm_api_key)
            embedding_model: Embedding model name
            embedding_dimensions: Vector dimensions (must match server config)
            
            chunk_size: Max tokens per chunk
            chunk_overlap: Overlap tokens between chunks
            
            auto_detect_communities: Auto-run community detection after indexing
            community_resolution: Leiden resolution parameter (higher = more granular)
            
            extractor: Custom extractor (overrides llm_provider)
            embedder: Custom embedder (overrides embedding_provider)
            chunker: Custom chunker (overrides default)
            
        Raises:
            ConfigurationError: If required configuration is missing
        """
        if not session_id:
            raise ConfigurationError("session_id is required")

        self.session_id = session_id
        self.host = host
        self.port = port
        self.auto_detect_communities = auto_detect_communities
        self.community_resolution = community_resolution

        # Initialize client
        self._client = _Client(host, port, session_id)

        # Initialize chunker
        if chunker:
            self._chunker = chunker
        else:
            self._chunker = TokenChunker(chunk_size=chunk_size, chunk_overlap=chunk_overlap)

        # Initialize extractor
        if extractor:
            self._extractor = extractor
        else:
            # Auto-detect API key
            api_key = llm_api_key or os.getenv("OPENAI_API_KEY")
            if not api_key:
                raise ConfigurationError(
                    "OpenAI API key required. Set OPENAI_API_KEY environment variable "
                    "or pass llm_api_key parameter."
                )

            model = llm_model or "gpt-4o"

            if llm_provider == "openai":
                self._extractor = OpenAIExtractor(api_key=api_key, model=model)
            else:
                raise ConfigurationError(f"Unsupported LLM provider: {llm_provider}")

        # Initialize embedder
        if embedder:
            self._embedder = embedder
        else:
            # Use same API key as extractor if not specified
            embed_key = embedding_api_key or llm_api_key or os.getenv("OPENAI_API_KEY")
            if not embed_key:
                raise ConfigurationError(
                    "OpenAI API key required for embeddings. Set OPENAI_API_KEY or "
                    "pass embedding_api_key parameter."
                )

            if embedding_provider == "openai":
                self._embedder = OpenAIEmbedder(
                    api_key=embed_key,
                    model=embedding_model,
                    dimensions=embedding_dimensions,
                )
            else:
                raise ConfigurationError(
                    f"Unsupported embedding provider: {embedding_provider}"
                )

        # Stats tracking
        self._stats = IndexStats()

    def index_documents(
        self,
        documents: Union[List[str], List[Dict[str, Any]]],
        batch_size: int = 10,
        show_progress: bool = True,
    ) -> IndexStats:
        """
        Index documents into knowledge graph.

        Pipeline:
            1. Chunk documents → TextUnits
            2. Extract entities & relationships (LLM call per chunk)
            3. Generate embeddings (batch API call)
            4. Store in graph (batch write)
            5. Link entities to text units
            6. Detect communities (if auto_detect_communities=True)

        Args:
            documents: List of strings or dicts with {"id": ..., "text": ..., "metadata": ...}
            batch_size: Batch size for LLM calls & embeddings
            show_progress: Show tqdm progress bar

        Returns:
            IndexStats with counts

        Examples:
            >>> stats = indexer.index_documents([
            ...     "Einstein was born in 1879.",
            ...     "He developed relativity theory."
            ... ])
            >>> print(f"Indexed {stats.entities_extracted} entities")
        """
        start_time = time.time()
        stats = IndexStats()

        # Normalize documents to dict format
        normalized_docs = []
        for doc in documents:
            if isinstance(doc, str):
                normalized_docs.append({
                    "id": str(uuid.uuid4()),
                    "text": doc,
                    "metadata": {},
                })
            elif isinstance(doc, dict):
                if "text" not in doc:
                    raise ValueError("Document dict must have 'text' key")
                normalized_docs.append({
                    "id": doc.get("id", str(uuid.uuid4())),
                    "text": doc["text"],
                    "metadata": doc.get("metadata", {}),
                })
            else:
                raise ValueError("Documents must be strings or dicts")

        stats.documents_indexed = len(normalized_docs)

        # Connect to server
        self._client.connect()

        # Process each document
        all_chunks = []
        all_entities = []
        all_relationships = []
        entity_title_to_ids = {}  # Map entity titles to their IDs

        for doc in tqdm(normalized_docs, desc="Processing documents", disable=not show_progress):
            # Add document to server
            doc_id = self._client.add_document(external_id=doc["id"], filename=doc["id"])

            # Chunk text
            chunks = self._chunker.chunk(doc["text"])
            stats.text_units_created += len(chunks)

            # Process chunks in batches for extraction
            for i in range(0, len(chunks), batch_size):
                batch_chunks = chunks[i : i + batch_size]

                # Extract entities & relationships from each chunk
                for chunk_idx, chunk in enumerate(
                    tqdm(
                        batch_chunks,
                        desc="Extracting knowledge",
                        disable=not show_progress,
                        leave=False,
                    )
                ):
                    try:
                        entities, relationships = self._extractor.extract(chunk)
                        all_chunks.append(
                            {
                                "doc_id": doc_id,
                                "text": chunk,
                                "entities": entities,
                                "relationships": relationships,
                            }
                        )
                        all_entities.extend(entities)
                        all_relationships.extend(relationships)
                    except Exception as e:
                        # Log error but continue (don't fail entire indexing)
                        if show_progress:
                            tqdm.write(f"Warning: Extraction failed for chunk: {e}")

        # Generate embeddings for all chunks and entities
        if show_progress:
            print("Generating embeddings...")

        # Chunk embeddings
        chunk_texts = [c["text"] for c in all_chunks]
        chunk_embeddings = []
        if chunk_texts:
            for i in range(0, len(chunk_texts), batch_size):
                batch = chunk_texts[i : i + batch_size]
                embeddings = self._embedder.embed(batch)
                chunk_embeddings.extend(embeddings)

        # Entity embeddings (deduplicate by title)
        unique_entities = {}
        for entity in all_entities:
            if entity.title not in unique_entities:
                unique_entities[entity.title] = entity

        entity_texts = [f"{e.title}: {e.description}" for e in unique_entities.values()]
        entity_embeddings = []
        if entity_texts:
            for i in range(0, len(entity_texts), batch_size):
                batch = entity_texts[i : i + batch_size]
                embeddings = self._embedder.embed(batch)
                entity_embeddings.extend(embeddings)

        # Store entities in graph
        if show_progress:
            print("Storing entities...")

        for entity, embedding in zip(unique_entities.values(), entity_embeddings):
            try:
                entity_id = self._client.add_entity(
                    external_id=f"entity-{uuid.uuid4()}",
                    title=entity.title,
                    entity_type=entity.type,
                    description=entity.description,
                    embedding=embedding,
                )
                entity_title_to_ids[entity.title] = entity_id
                stats.entities_extracted += 1
            except Exception as e:
                if show_progress:
                    tqdm.write(f"Warning: Failed to store entity '{entity.title}': {e}")

        # Store text units in graph and link to entities
        if show_progress:
            print("Storing text units...")

        for chunk_data, embedding in zip(all_chunks, chunk_embeddings):
            try:
                textunit_id = self._client.add_text_unit(
                    external_id=f"tu-{uuid.uuid4()}",
                    document_id=chunk_data["doc_id"],
                    content=chunk_data["text"],
                    embedding=embedding,
                    token_count=len(chunk_data["text"].split()),
                )

                # Link to entities mentioned in this chunk
                for entity in chunk_data["entities"]:
                    if entity.title in entity_title_to_ids:
                        entity_id = entity_title_to_ids[entity.title]
                        try:
                            self._client.link_text_unit_entity(textunit_id, entity_id)
                        except Exception as e:
                            if show_progress:
                                tqdm.write(f"Warning: Failed to link entity: {e}")

            except Exception as e:
                if show_progress:
                    tqdm.write(f"Warning: Failed to store text unit: {e}")

        # Store relationships
        if show_progress:
            print("Storing relationships...")

        for relationship in all_relationships:
            # Check if both source and target entities exist
            source_id = entity_title_to_ids.get(relationship.source_title)
            target_id = entity_title_to_ids.get(relationship.target_title)

            if source_id and target_id:
                try:
                    self._client.add_relationship(
                        external_id=f"rel-{uuid.uuid4()}",
                        source_id=source_id,
                        target_id=target_id,
                        rel_type=relationship.relationship_type,
                        description=relationship.description,
                        weight=relationship.weight,
                    )
                    stats.relationships_extracted += 1
                except Exception as e:
                    if show_progress:
                        tqdm.write(f"Warning: Failed to store relationship: {e}")

        # Detect communities
        if self.auto_detect_communities and stats.entities_extracted > 0:
            if show_progress:
                print("Detecting communities...")

            try:
                community_count = self._client.compute_communities(self.community_resolution)
                stats.communities_detected = community_count
            except Exception as e:
                if show_progress:
                    tqdm.write(f"Warning: Community detection failed: {e}")

        stats.indexing_time_seconds = time.time() - start_time
        self._stats = stats
        return stats

    def query(
        self,
        query: str,
        mode: Literal["local", "global", "hybrid"] = "local",
        top_k: int = 10,
        include_entities: bool = True,
        include_text_units: bool = True,
        include_communities: bool = False,
    ) -> QueryResult:
        """
        Query knowledge graph.

        Args:
            query: Natural language query
            mode: Query mode (v0.1.0 only supports "local")
            top_k: Number of results
            include_entities: Include entity results
            include_text_units: Include text unit results
            include_communities: Include community results

        Returns:
            QueryResult with entities, text_units, communities

        Examples:
            >>> result = indexer.query("Einstein's theories", top_k=5)
            >>> for entity in result.entities:
            ...     print(f"{entity.title}: {entity.score:.3f}")
        """
        if mode != "local":
            raise NotImplementedError("Only 'local' mode is supported in v0.1.0")

        # Generate query embedding
        query_embedding = self._embedder.embed_single(query)

        # Determine search types
        search_types = []
        if include_entities:
            search_types.append("entity")
        if include_text_units:
            search_types.append("textunit")
        if include_communities:
            search_types.append("community")

        if not search_types:
            return QueryResult()

        # Execute query
        raw_result = self._client.query(query_embedding, search_types, top_k)

        # Parse results
        entities = [
            ScoredEntity(
                id=e["entity"]["id"],
                title=e["entity"]["title"],
                type=e["entity"]["type"],
                description=e["entity"]["description"],
                score=e["similarity"],
            )
            for e in raw_result.get("entities", [])
        ]

        text_units = [
            ScoredTextUnit(
                id=tu["textunit"]["id"],
                content=tu["textunit"]["content"],
                document_id=tu["textunit"]["document_id"],
                score=tu["similarity"],
            )
            for tu in raw_result.get("textunits", [])
        ]

        communities = [
            ScoredCommunity(
                id=c["community"]["id"],
                title=c["community"]["title"],
                summary=c["community"]["summary"],
                entity_count=len(c["community"]["entity_ids"]),
                score=c["similarity"],
            )
            for c in raw_result.get("communities", [])
        ]

        return QueryResult(
            entities=entities,
            text_units=text_units,
            communities=communities,
            execution_time_ms=raw_result.get("execution_time_ms", 0.0),
        )

    def get_stats(self) -> IndexStats:
        """Get current index statistics."""
        return self._stats

    def close(self):
        """Close connection to server."""
        self._client.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
