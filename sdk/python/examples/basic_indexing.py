"""Basic indexing and querying example for GibRAM."""

import os
from gibram import GibRAMIndexer

def main():
    # Set OpenAI API key (or use environment variable)
    api_key = os.getenv("OPENAI_API_KEY")
    if not api_key:
        print("Error: Set OPENAI_API_KEY environment variable")
        return

    # Sample documents about Einstein
    documents = [
        "Albert Einstein was born on March 14, 1879, in Ulm, in the Kingdom of Württemberg in the German Empire.",
        "Einstein developed the theory of relativity, one of the two pillars of modern physics.",
        "In 1905, Einstein published four groundbreaking papers during his annus mirabilis (miracle year).",
        "He received the 1921 Nobel Prize in Physics for his services to theoretical physics, especially for his discovery of the law of the photoelectric effect.",
        "Einstein emigrated to the United States in 1933 and became an American citizen in 1940.",
        "He worked at the Institute for Advanced Study in Princeton, New Jersey, until his death in 1955.",
    ]

    print("=== GibRAM Basic Indexing Example ===\n")

    # Initialize indexer
    print("Initializing GibRAM indexer...")
    with GibRAMIndexer(
        session_id="einstein-demo",
        llm_api_key=api_key,
        host="localhost",
        port=6161,
        chunk_size=256,  # Smaller chunks for this demo
        auto_detect_communities=True,
    ) as indexer:
        # Index documents
        print(f"\nIndexing {len(documents)} documents...")
        stats = indexer.index_documents(documents, batch_size=5, show_progress=True)

        # Print stats
        print("\n=== Indexing Statistics ===")
        print(f"Documents indexed: {stats.documents_indexed}")
        print(f"Text units created: {stats.text_units_created}")
        print(f"Entities extracted: {stats.entities_extracted}")
        print(f"Relationships extracted: {stats.relationships_extracted}")
        print(f"Communities detected: {stats.communities_detected}")
        print(f"Indexing time: {stats.indexing_time_seconds:.2f}s")

        # Query examples
        queries = [
            "Where was Einstein born?",
            "What did Einstein discover?",
            "Einstein's awards and recognition",
        ]

        print("\n=== Query Examples ===\n")
        for query in queries:
            print(f"Query: '{query}'")
            result = indexer.query(query, top_k=3, include_entities=True, include_text_units=True)

            print(f"  Entities found: {len(result.entities)}")
            for entity in result.entities[:3]:
                print(f"    - {entity.title} ({entity.type}): {entity.description[:80]}... [score: {entity.score:.3f}]")

            print(f"  Text units found: {len(result.text_units)}")
            for tu in result.text_units[:2]:
                content = tu.content.replace("\n", " ")[:100]
                print(f"    - {content}... [score: {tu.score:.3f}]")

            print(f"  Execution time: {result.execution_time_ms:.2f}ms\n")


if __name__ == "__main__":
    main()
