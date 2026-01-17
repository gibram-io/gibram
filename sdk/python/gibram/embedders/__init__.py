"""Text embedders for vector generation."""

from .base import BaseEmbedder
from .openai import OpenAIEmbedder

__all__ = ["BaseEmbedder", "OpenAIEmbedder"]
