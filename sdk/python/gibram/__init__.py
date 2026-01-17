"""GibRAM Python SDK - GraphRAG-style knowledge graph indexing."""

__version__ = "0.1.0"

from .indexer import GibRAMIndexer
from .types import (
    IndexStats,
    QueryResult,
    ScoredEntity,
    ScoredTextUnit,
    ScoredCommunity,
    ExtractedEntity,
    ExtractedRelationship,
)
from .exceptions import (
    GibRAMError,
    ConnectionError,
    TimeoutError,
    ProtocolError,
    ServerError,
    NotFoundError,
    ValidationError,
    ExtractionError,
    EmbeddingError,
    ConfigurationError,
)

__all__ = [
    # Main API
    "GibRAMIndexer",
    # Return types
    "IndexStats",
    "QueryResult",
    "ScoredEntity",
    "ScoredTextUnit",
    "ScoredCommunity",
    # For advanced users
    "ExtractedEntity",
    "ExtractedRelationship",
    # Exceptions
    "GibRAMError",
    "ConnectionError",
    "TimeoutError",
    "ProtocolError",
    "ServerError",
    "NotFoundError",
    "ValidationError",
    "ExtractionError",
    "EmbeddingError",
    "ConfigurationError",
]
