"""Knowledge extractor base interface."""

from abc import ABC, abstractmethod
from typing import List, Tuple
from ..types import ExtractedEntity, ExtractedRelationship


class BaseExtractor(ABC):
    """Abstract base class for knowledge extractors."""

    @abstractmethod
    def extract(
        self, text: str
    ) -> Tuple[List[ExtractedEntity], List[ExtractedRelationship]]:
        """
        Extract entities and relationships from text.

        Args:
            text: Input text chunk

        Returns:
            Tuple of (entities, relationships)
        """
        pass
