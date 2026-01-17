"""Integration test for GibRAM Python SDK v0.1.0."""

import os
import sys
import time


def check_environment():
    """Check prerequisites before running tests."""
    print("=== Environment Check ===\n")

    # Check OpenAI API key
    api_key = os.getenv("OPENAI_API_KEY")
    if not api_key:
        print("❌ OPENAI_API_KEY not set")
        print("   Export your API key: export OPENAI_API_KEY='sk-...'")
        return False
    else:
        print(f"✅ OpenAI API key found: {api_key[:10]}...")

    # Check server connection
    print("\nChecking GibRAM server connection...")
    try:
        import socket

        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(2)
        result = sock.connect_ex(("localhost", 6161))
        sock.close()

        if result == 0:
            print("✅ GibRAM server is running on localhost:6161")
        else:
            print("❌ GibRAM server not accessible on localhost:6161")
            print("   Start server: docker run -d -p 6161:6161 gibram:latest")
            return False
    except Exception as e:
        print(f"❌ Connection check failed: {e}")
        return False

    return True


def test_basic_workflow():
    """Test basic indexing and querying workflow."""
    print("\n=== Test 1: Basic Workflow ===\n")

    try:
        from gibram import GibRAMIndexer

        # Initialize indexer
        print("1. Initializing indexer...")
        indexer = GibRAMIndexer(
            session_id=f"test-{int(time.time())}",
            chunk_size=256,
            auto_detect_communities=True,
        )
        print("   ✅ Indexer initialized")

        # Test documents
        documents = [
            "Python is a high-level programming language created by Guido van Rossum in 1991.",
            "JavaScript was created by Brendan Eich in 1995 at Netscape.",
            "Rust is a systems programming language focused on safety and performance.",
        ]

        # Index documents
        print("\n2. Indexing documents...")
        stats = indexer.index_documents(documents, batch_size=3, show_progress=False)

        print(f"   ✅ Indexed {stats.documents_indexed} documents")
        print(f"   - Text units created: {stats.text_units_created}")
        print(f"   - Entities extracted: {stats.entities_extracted}")
        print(f"   - Relationships extracted: {stats.relationships_extracted}")
        print(f"   - Communities detected: {stats.communities_detected}")
        print(f"   - Indexing time: {stats.indexing_time_seconds:.2f}s")

        # Validate counts
        assert stats.documents_indexed == 3, "Should index 3 documents"
        assert stats.text_units_created > 0, "Should create text units"
        assert stats.entities_extracted > 0, "Should extract entities"
        print("   ✅ Stats validation passed")

        # Query
        print("\n3. Querying knowledge graph...")
        result = indexer.query("programming languages", top_k=5)

        print(f"   ✅ Query executed in {result.execution_time_ms:.2f}ms")
        print(f"   - Entities found: {len(result.entities)}")
        print(f"   - Text units found: {len(result.text_units)}")

        # Display top results
        if result.entities:
            print("\n   Top entities:")
            for entity in result.entities[:3]:
                print(f"     - {entity.title} ({entity.type}): score={entity.score:.3f}")

        # Validate results
        assert len(result.entities) > 0 or len(result.text_units) > 0, "Should return results"
        print("   ✅ Query validation passed")

        # Cleanup
        indexer.close()
        print("\n✅ Test 1 PASSED\n")
        return True

    except Exception as e:
        print(f"\n❌ Test 1 FAILED: {e}")
        import traceback

        traceback.print_exc()
        return False


def test_context_manager():
    """Test context manager usage."""
    print("\n=== Test 2: Context Manager ===\n")

    try:
        from gibram import GibRAMIndexer

        session_id = f"test-cm-{int(time.time())}"

        print("Testing context manager...")
        with GibRAMIndexer(session_id=session_id, chunk_size=128) as indexer:
            stats = indexer.index_documents(
                ["Test document for context manager."], show_progress=False
            )
            assert stats.documents_indexed == 1
            print("   ✅ Indexing inside context manager works")

        print("   ✅ Context manager cleanup successful")
        print("\n✅ Test 2 PASSED\n")
        return True

    except Exception as e:
        print(f"\n❌ Test 2 FAILED: {e}")
        import traceback

        traceback.print_exc()
        return False


def test_error_handling():
    """Test error handling."""
    print("\n=== Test 3: Error Handling ===\n")

    try:
        from gibram import GibRAMIndexer, ConfigurationError

        # Test missing session_id
        print("1. Testing missing session_id...")
        try:
            indexer = GibRAMIndexer(session_id="")
            print("   ❌ Should have raised ConfigurationError")
            return False
        except ConfigurationError:
            print("   ✅ ConfigurationError raised correctly")

        # Test missing API key
        print("\n2. Testing missing API key...")
        old_key = os.getenv("OPENAI_API_KEY")
        os.environ.pop("OPENAI_API_KEY", None)

        try:
            indexer = GibRAMIndexer(session_id="test-error")
            print("   ❌ Should have raised ConfigurationError")
            return False
        except ConfigurationError as e:
            print(f"   ✅ ConfigurationError raised: {e}")
        finally:
            if old_key:
                os.environ["OPENAI_API_KEY"] = old_key

        print("\n✅ Test 3 PASSED\n")
        return True

    except Exception as e:
        print(f"\n❌ Test 3 FAILED: {e}")
        import traceback

        traceback.print_exc()
        return False


def test_query_modes():
    """Test different query modes and options."""
    print("\n=== Test 4: Query Modes ===\n")

    try:
        from gibram import GibRAMIndexer

        session_id = f"test-query-{int(time.time())}"

        with GibRAMIndexer(session_id=session_id, chunk_size=200) as indexer:
            # Index test data
            indexer.index_documents(
                ["Machine learning is a subset of artificial intelligence."],
                show_progress=False,
            )

            # Test entity-only query
            print("1. Testing entity-only query...")
            result = indexer.query(
                "machine learning",
                include_entities=True,
                include_text_units=False,
                include_communities=False,
            )
            assert len(result.text_units) == 0, "Should not return text units"
            print("   ✅ Entity-only query works")

            # Test text-unit-only query
            print("\n2. Testing text-unit-only query...")
            result = indexer.query(
                "artificial intelligence",
                include_entities=False,
                include_text_units=True,
                include_communities=False,
            )
            assert len(result.entities) == 0, "Should not return entities"
            print("   ✅ Text-unit-only query works")

        print("\n✅ Test 4 PASSED\n")
        return True

    except Exception as e:
        print(f"\n❌ Test 4 FAILED: {e}")
        import traceback

        traceback.print_exc()
        return False


def main():
    """Run all integration tests."""
    print("=" * 60)
    print("GibRAM Python SDK v0.1.0 Integration Tests")
    print("=" * 60 + "\n")

    # Check environment
    if not check_environment():
        print("\n❌ Environment check failed. Fix issues and try again.")
        sys.exit(1)

    # Run tests
    tests = [
        ("Basic Workflow", test_basic_workflow),
        ("Context Manager", test_context_manager),
        ("Error Handling", test_error_handling),
        ("Query Modes", test_query_modes),
    ]

    results = []
    for name, test_func in tests:
        try:
            passed = test_func()
            results.append((name, passed))
        except Exception as e:
            print(f"\n❌ {name} crashed: {e}")
            results.append((name, False))

    # Summary
    print("=" * 60)
    print("Test Summary")
    print("=" * 60 + "\n")

    passed_count = sum(1 for _, passed in results if passed)
    total_count = len(results)

    for name, passed in results:
        status = "✅ PASSED" if passed else "❌ FAILED"
        print(f"{status}: {name}")

    print(f"\n{passed_count}/{total_count} tests passed")

    if passed_count == total_count:
        print("\n🎉 All tests passed!")
        sys.exit(0)
    else:
        print(f"\n❌ {total_count - passed_count} test(s) failed")
        sys.exit(1)


if __name__ == "__main__":
    main()
