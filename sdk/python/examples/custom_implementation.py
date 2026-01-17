"""Example of custom extractor and embedder implementations."""

import os
import re
from typing import List, Tuple
from gibram import GibRAMIndexer
from gibram.extractors import BaseExtractor
from gibram.embedders import BaseEmbedder
from gibram.types import ExtractedEntity, ExtractedRelationship


class SimpleRegexExtractor(BaseExtractor):
    """
    Example custom extractor using simple regex patterns.
    
    Note: This is a toy implementation for demonstration.
    Production use should use LLM-based extraction.
    """

    def extract(self, text: str) -> Tuple[List[ExtractedEntity], List[ExtractedRelationship]]:
        entities = []
        relationships = []

        # Extract person names (capitalized words)
        person_pattern = r"\b([A-Z][a-z]+ [A-Z][a-z]+)\b"
        persons = set(re.findall(person_pattern, text))

        for person in persons:
            entities.append(
                ExtractedEntity(
                    title=person,
                    type="Person",
                    description=f"Person mentioned: {person}",
                )
            )

        # Extract years
        year_pattern = r"\b(1[0-9]{3}|20[0-9]{2})\b"
        years = set(re.findall(year_pattern, text))

        for year in years:
            entities.append(
                ExtractedEntity(
                    title=year,
                    type="Year",
                    description=f"Year mentioned: {year}",
                )
            )

        # Create relationships between persons and years
        for person in persons:
            for year in years:
                if person in text and year in text:
                    relationships.append(
                        ExtractedRelationship(
                            source_title=person,
                            target_title=year,
                            relationship_type="MENTIONED_IN",
                            description=f"{person} mentioned in context of {year}",
                            weight=0.5,
                        )
                    )

        return entities, relationships


class DummyEmbedder(BaseEmbedder):
    """
    Example custom embedder using random vectors.
    
    Note: This is a toy implementation for demonstration.
    Production use should use proper embeddings (OpenAI, etc.).
    """

    def __init__(self, dimensions: int = 1536):
        self.dimensions = dimensions

    def embed(self, texts: List[str]) -> List[List[float]]:
        import hashlib
        import struct

        embeddings = []
        for text in texts:
            # Generate deterministic "embedding" from text hash
            hash_obj = hashlib.sha256(text.encode())
            hash_bytes = hash_obj.digest()

            # Convert hash to floats
            vec = []
            for i in range(0, min(len(hash_bytes), self.dimensions * 4), 4):
                chunk = hash_bytes[i : i + 4].ljust(4, b"\x00")
                value = struct.unpack("f", chunk)[0]
                vec.append(value)

            # Pad to desired dimensions
            while len(vec) < self.dimensions:
                vec.append(0.0)

            embeddings.append(vec[: self.dimensions])

        return embeddings

    def embed_single(self, text: str) -> List[float]:
        return self.embed([text])[0]


def main():
    """Demonstrate custom extractor and embedder."""
    
    # Warning message
    print("=== Custom Extractor/Embedder Example ===\n")
    print("WARNING: This example uses toy implementations for demonstration.")
    print("For production, use OpenAI-based extraction and embeddings.\n")

    # Sample documents
    documents = [
        "Albert Einstein was born in 1879.",
        "Marie Curie won Nobel Prize in 1903.",
        "Isaac Newton published Principia in 1687.",
    ]

    # Initialize with custom implementations
    print("Initializing with custom extractor and embedder...")
    with GibRAMIndexer(
        session_id="custom-demo",
        extractor=SimpleRegexExtractor(),
        embedder=DummyEmbedder(dimensions=1536),
        host="localhost",
        port=6161,
    ) as indexer:
        # Index documents
        print(f"\nIndexing {len(documents)} documents...")
        stats = indexer.index_documents(documents, show_progress=True)

        # Print stats
        print("\n=== Results ===")
        print(f"Entities extracted: {stats.entities_extracted}")
        print(f"Relationships extracted: {stats.relationships_extracted}")

        # Query
        print("\nQuerying for 'Einstein'...")
        result = indexer.query("Einstein", top_k=5)

        print(f"Entities found: {len(result.entities)}")
        for entity in result.entities:
            print(f"  - {entity.title} ({entity.type})")

        print(f"\nText units found: {len(result.text_units)}")
        for tu in result.text_units[:3]:
            print(f"  - {tu.content}")


if __name__ == "__main__":
    main()
