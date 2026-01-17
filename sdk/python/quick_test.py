#!/usr/bin/env python3
"""
Quick test untuk GibRAM SDK - requires OpenAI API key.

Usage:
    export OPENAI_API_KEY="sk-..."
    python quick_test.py
"""

import os
import sys

def main():
    print("=" * 60)
    print("GibRAM Python SDK v0.1.0 - Quick Test")
    print("=" * 60 + "\n")

    # Check API key
    api_key = os.getenv("OPENAI_API_KEY")
    if not api_key:
        print("❌ OPENAI_API_KEY not set")
        print("\nSet your API key:")
        print("  export OPENAI_API_KEY='sk-...'")
        print("\nOr pass it in code:")
        print("  indexer = GibRAMIndexer(session_id='...', llm_api_key='sk-...')")
        sys.exit(1)

    print(f"✅ OpenAI API key found: {api_key[:10]}...\n")

    # Import SDK
    try:
        from gibram import GibRAMIndexer
        print("✅ GibRAM SDK imported\n")
    except ImportError as e:
        print(f"❌ Failed to import GibRAM SDK: {e}")
        print("\nInstall SDK:")
        print("  cd sdk/python && pip install -e .")
        sys.exit(1)

    # Test indexing
    print("Testing indexing with OpenAI GPT-4...")
    print("-" * 60)

    documents = [
        "Python is a high-level programming language created by Guido van Rossum.",
        "JavaScript was created by Brendan Eich at Netscape in 1995.",
    ]

    try:
        with GibRAMIndexer(
            session_id=f"quick-test-{os.getpid()}",
            chunk_size=200,
            auto_detect_communities=True,
        ) as indexer:
            print(f"\n📄 Indexing {len(documents)} documents...")
            stats = indexer.index_documents(documents, batch_size=2, show_progress=True)

            print("\n" + "=" * 60)
            print("Results:")
            print("=" * 60)
            print(f"  Documents indexed: {stats.documents_indexed}")
            print(f"  Text units: {stats.text_units_created}")
            print(f"  Entities extracted: {stats.entities_extracted}")
            print(f"  Relationships: {stats.relationships_extracted}")
            print(f"  Communities: {stats.communities_detected}")
            print(f"  Time: {stats.indexing_time_seconds:.2f}s")

            # Query
            print("\n" + "=" * 60)
            print("Querying: 'programming languages'")
            print("=" * 60)
            
            result = indexer.query("programming languages", top_k=5)
            
            print(f"\n✅ Found {len(result.entities)} entities, {len(result.text_units)} text units")
            print(f"   Query time: {result.execution_time_ms:.2f}ms")

            if result.entities:
                print("\nTop entities:")
                for i, entity in enumerate(result.entities[:5], 1):
                    print(f"  {i}. {entity.title} ({entity.type})")
                    print(f"     {entity.description[:80]}...")
                    print(f"     Score: {entity.score:.3f}\n")

            if result.text_units:
                print("\nTop text units:")
                for i, tu in enumerate(result.text_units[:3], 1):
                    content = tu.content.replace("\n", " ")[:100]
                    print(f"  {i}. {content}... [score: {tu.score:.3f}]")

        print("\n" + "=" * 60)
        print("🎉 All tests PASSED!")
        print("=" * 60)

    except Exception as e:
        print(f"\n❌ Test failed: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)


if __name__ == "__main__":
    main()
