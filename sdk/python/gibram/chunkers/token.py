"""Token-based text chunker with overlap."""

import re
from typing import List
from .base import BaseChunker


class TokenChunker(BaseChunker):
    """
    Simple token-based chunker with overlap.
    
    Chunks text by approximate token count (whitespace-based tokenization).
    Preserves context by overlapping chunks.
    """

    def __init__(self, chunk_size: int = 512, chunk_overlap: int = 50):
        """
        Initialize chunker.

        Args:
            chunk_size: Maximum tokens per chunk (approximate)
            chunk_overlap: Number of tokens to overlap between chunks
        """
        if chunk_size <= 0:
            raise ValueError("chunk_size must be positive")
        if chunk_overlap < 0:
            raise ValueError("chunk_overlap must be non-negative")
        if chunk_overlap >= chunk_size:
            raise ValueError("chunk_overlap must be less than chunk_size")

        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap

    def chunk(self, text: str) -> List[str]:
        """
        Chunk text with sliding window and overlap.

        Algorithm:
            1. Tokenize by whitespace (approximation of tokens)
            2. Apply sliding window with overlap
            3. Rejoin tokens into text chunks
            4. Filter empty chunks

        Args:
            text: Input text

        Returns:
            List of text chunks

        Examples:
            >>> chunker = TokenChunker(chunk_size=10, chunk_overlap=2)
            >>> text = "word " * 30
            >>> chunks = chunker.chunk(text)
            >>> len(chunks) >= 3  # Should have multiple chunks
            True
        """
        if not text or not text.strip():
            return []

        # Tokenize by whitespace (simple approximation)
        tokens = re.findall(r"\S+", text)

        if len(tokens) <= self.chunk_size:
            # Text fits in one chunk
            return [text.strip()]

        chunks = []
        start = 0

        while start < len(tokens):
            # Get chunk of tokens
            end = min(start + self.chunk_size, len(tokens))
            chunk_tokens = tokens[start:end]

            # Rejoin into text
            chunk_text = " ".join(chunk_tokens)
            chunks.append(chunk_text)

            # Move window forward
            if end >= len(tokens):
                break

            # Advance by (chunk_size - overlap) to create overlap
            start += self.chunk_size - self.chunk_overlap

        return chunks
