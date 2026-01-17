"""Text embedder base interface."""

from abc import ABC, abstractmethod
from typing import List


class BaseEmbedder(ABC):
    """Abstract base class for text embedders."""

    @abstractmethod
    def embed(self, texts: List[str]) -> List[List[float]]:
        """
        Generate embeddings for batch of texts.

        Args:
            texts: List of text strings

        Returns:
            List of embedding vectors
        """
        pass

    @abstractmethod
    def embed_single(self, text: str) -> List[float]:
        """
        Generate embedding for single text.

        Args:
            text: Input text

        Returns:
            Embedding vector
        """
        pass
