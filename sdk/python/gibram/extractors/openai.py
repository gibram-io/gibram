"""OpenAI-based knowledge extraction."""

import json
import time
from typing import List, Tuple
from openai import OpenAI, RateLimitError, APIError
from .base import BaseExtractor
from ..types import ExtractedEntity, ExtractedRelationship
from ..exceptions import ExtractionError


class OpenAIExtractor(BaseExtractor):
    """
    Entity and relationship extraction using OpenAI GPT-4.
    
    Uses structured output (JSON mode) for reliable parsing.
    Implements retry logic for rate limits and transient errors.
    """

    # System prompt for extraction
    SYSTEM_PROMPT = """You are a knowledge graph extraction expert. 
Extract entities and relationships from the provided text.

Entities should have:
- title: The entity name (normalized, e.g., "Albert Einstein" not "einstein" or "Einstein, Albert")
- type: Category (person, organization, location, concept, event, technology, etc.)
- description: Brief description (1-2 sentences)

Relationships should have:
- source_title: Source entity title (must exactly match an extracted entity)
- target_title: Target entity title (must exactly match an extracted entity)
- relationship_type: Relationship type (e.g., "works_for", "located_in", "invented", "collaborated_with")
- description: Brief description of the relationship
- weight: Confidence score (0.0 to 1.0), default 1.0 for explicit relationships

Guidelines:
- Extract only factual entities and relationships explicitly mentioned in the text
- Avoid inference or speculation
- Normalize entity names (proper capitalization, full names)
- Use descriptive relationship types
- Ensure source_title and target_title exactly match extracted entity titles

Output JSON format:
{
  "entities": [
    {"title": "string", "type": "string", "description": "string"}
  ],
  "relationships": [
    {
      "source_title": "string",
      "target_title": "string", 
      "relationship_type": "string",
      "description": "string",
      "weight": float
    }
  ]
}"""

    def __init__(
        self,
        api_key: str,
        model: str = "gpt-4o",
        temperature: float = 0.0,
        max_retries: int = 3,
        retry_delay: float = 1.0,
    ):
        """
        Initialize OpenAI extractor.

        Args:
            api_key: OpenAI API key
            model: Model name (gpt-4o, gpt-4o-mini, gpt-4-turbo, etc.)
            temperature: LLM temperature (0.0 = deterministic, recommended for extraction)
            max_retries: Maximum retry attempts for rate limits
            retry_delay: Initial retry delay in seconds (exponential backoff)
        """
        self.client = OpenAI(api_key=api_key)
        self.model = model
        self.temperature = temperature
        self.max_retries = max_retries
        self.retry_delay = retry_delay

    def extract(
        self, text: str
    ) -> Tuple[List[ExtractedEntity], List[ExtractedRelationship]]:
        """
        Extract entities and relationships from text using GPT-4.

        Args:
            text: Input text chunk

        Returns:
            Tuple of (entities, relationships)

        Raises:
            ExtractionError: If extraction fails after retries
        """
        if not text or not text.strip():
            return [], []

        # Call OpenAI with retry logic
        for attempt in range(self.max_retries):
            try:
                response = self.client.chat.completions.create(
                    model=self.model,
                    temperature=self.temperature,
                    messages=[
                        {"role": "system", "content": self.SYSTEM_PROMPT},
                        {"role": "user", "content": f"Extract knowledge from this text:\n\n{text}"},
                    ],
                    response_format={"type": "json_object"},
                )

                # Parse JSON response
                content = response.choices[0].message.content
                data = json.loads(content)

                # Convert to typed objects
                entities = [
                    ExtractedEntity(
                        title=e["title"],
                        type=e["type"],
                        description=e.get("description", ""),
                    )
                    for e in data.get("entities", [])
                ]

                relationships = [
                    ExtractedRelationship(
                        source_title=r["source_title"],
                        target_title=r["target_title"],
                        relationship_type=r["relationship_type"],
                        description=r.get("description", ""),
                        weight=r.get("weight", 1.0),
                    )
                    for r in data.get("relationships", [])
                ]

                return entities, relationships

            except RateLimitError as e:
                if attempt == self.max_retries - 1:
                    raise ExtractionError(
                        f"OpenAI rate limit exceeded after {self.max_retries} retries: {e}"
                    ) from e
                
                # Exponential backoff
                delay = self.retry_delay * (2**attempt)
                time.sleep(delay)

            except APIError as e:
                if attempt == self.max_retries - 1:
                    raise ExtractionError(
                        f"OpenAI API error after {self.max_retries} retries: {e}"
                    ) from e
                
                delay = self.retry_delay * (2**attempt)
                time.sleep(delay)

            except json.JSONDecodeError as e:
                raise ExtractionError(f"Failed to parse OpenAI JSON response: {e}") from e

            except Exception as e:
                raise ExtractionError(f"Unexpected extraction error: {e}") from e

        # Should not reach here
        return [], []
