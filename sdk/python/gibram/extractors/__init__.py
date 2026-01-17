"""Knowledge extractors for entity and relationship extraction."""

from .base import BaseExtractor
from .openai import OpenAIExtractor

__all__ = ["BaseExtractor", "OpenAIExtractor"]
