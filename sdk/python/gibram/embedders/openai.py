"""OpenAI-based text embedding."""

import time
from typing import List
from openai import OpenAI, RateLimitError, APIError
from .base import BaseEmbedder
from ..exceptions import EmbeddingError


class OpenAIEmbedder(BaseEmbedder):
    """
    Text embedding using OpenAI Embedding API.
    
    Supports:
    - text-embedding-3-small (default, cost-effective, 1536 dimensions)
    - text-embedding-3-large (higher quality, 3072 dimensions)
    - text-embedding-ada-002 (legacy, 1536 dimensions)
    
    Implements retry logic and batch processing.
    """

    # OpenAI batch size limit
    MAX_BATCH_SIZE = 2048

    def __init__(
        self,
        api_key: str,
        model: str = "text-embedding-3-small",
        dimensions: int = 1536,
        max_retries: int = 3,
        retry_delay: float = 1.0,
    ):
        """
        Initialize OpenAI embedder.

        Args:
            api_key: OpenAI API key
            model: Embedding model name
            dimensions: Output dimension (must match server vector_dim config)
            max_retries: Maximum retry attempts for rate limits
            retry_delay: Initial retry delay in seconds (exponential backoff)
        """
        self.client = OpenAI(api_key=api_key)
        self.model = model
        self.dimensions = dimensions
        self.max_retries = max_retries
        self.retry_delay = retry_delay

    def embed(self, texts: List[str]) -> List[List[float]]:
        """
        Generate embeddings for batch of texts.

        Args:
            texts: List of text strings (max 2048 per batch)

        Returns:
            List of embedding vectors

        Raises:
            EmbeddingError: If embedding generation fails
            ValueError: If batch size exceeds limit
        """
        if not texts:
            return []

        if len(texts) > self.MAX_BATCH_SIZE:
            raise ValueError(
                f"Batch size {len(texts)} exceeds OpenAI limit {self.MAX_BATCH_SIZE}. "
                f"Split into smaller batches."
            )

        # Filter empty texts
        filtered_texts = [t if t.strip() else " " for t in texts]

        # Call OpenAI with retry logic
        for attempt in range(self.max_retries):
            try:
                response = self.client.embeddings.create(
                    model=self.model,
                    input=filtered_texts,
                    dimensions=self.dimensions,
                )

                # Extract embeddings in order
                embeddings = [data.embedding for data in response.data]
                return embeddings

            except RateLimitError as e:
                if attempt == self.max_retries - 1:
                    raise EmbeddingError(
                        f"OpenAI rate limit exceeded after {self.max_retries} retries: {e}"
                    ) from e

                # Exponential backoff
                delay = self.retry_delay * (2**attempt)
                time.sleep(delay)

            except APIError as e:
                if attempt == self.max_retries - 1:
                    raise EmbeddingError(
                        f"OpenAI API error after {self.max_retries} retries: {e}"
                    ) from e

                delay = self.retry_delay * (2**attempt)
                time.sleep(delay)

            except Exception as e:
                raise EmbeddingError(f"Unexpected embedding error: {e}") from e

        # Should not reach here
        return []

    def embed_single(self, text: str) -> List[float]:
        """
        Generate embedding for single text.

        Args:
            text: Input text

        Returns:
            Embedding vector

        Raises:
            EmbeddingError: If embedding generation fails
        """
        embeddings = self.embed([text])
        return embeddings[0] if embeddings else []
