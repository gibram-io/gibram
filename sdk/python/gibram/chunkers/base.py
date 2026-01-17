"""Text chunking base interface."""

from abc import ABC, abstractmethod
from typing import List


class BaseChunker(ABC):
    """Abstract base class for text chunkers."""

    @abstractmethod
    def chunk(self, text: str) -> List[str]:
        """
        Split text into chunks.

        Args:
            text: Input text to chunk

        Returns:
            List of text chunks
        """
        pass
