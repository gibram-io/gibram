"""GibRAM type definitions."""

from dataclasses import dataclass, field
from typing import List, Optional, Dict, Any


@dataclass
class ExtractedEntity:
    """Entity extracted from text by LLM."""

    title: str
    type: str
    description: str


@dataclass
class ExtractedRelationship:
    """Relationship extracted from text by LLM."""

    source_title: str
    target_title: str
    relationship_type: str
    description: str
    weight: float = 1.0


@dataclass
class IndexStats:
    """Statistics from indexing operation."""

    documents_indexed: int = 0
    text_units_created: int = 0
    entities_extracted: int = 0
    relationships_extracted: int = 0
    communities_detected: int = 0
    indexing_time_seconds: float = 0.0


@dataclass
class ScoredEntity:
    """Entity with similarity score."""

    id: int
    title: str
    type: str
    description: str
    score: float


@dataclass
class ScoredTextUnit:
    """Text unit with similarity score."""

    id: int
    content: str
    document_id: int
    score: float


@dataclass
class ScoredCommunity:
    """Community with similarity score."""

    id: int
    title: str
    summary: str
    entity_count: int
    score: float


@dataclass
class QueryResult:
    """Query result container."""

    entities: List[ScoredEntity] = field(default_factory=list)
    text_units: List[ScoredTextUnit] = field(default_factory=list)
    communities: List[ScoredCommunity] = field(default_factory=list)
    execution_time_ms: float = 0.0
