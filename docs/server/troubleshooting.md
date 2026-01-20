# Troubleshooting (v0.1.0)

Common issues and solutions when running GibRAM server.

## Server Issues

### Server Won't Start

#### Port Already in Use

**Symptom**:
```
Error: listen tcp :6161: bind: address already in use
```

**Cause**: Another process is using port 6161.

**Diagnosis**:
```bash
# Check what's using the port
lsof -i :6161

# Or on Linux
netstat -tlnp | grep 6161
```

**Solutions**:

1. **Stop the conflicting process**:
```bash
# Kill process using port 6161
kill -9 <PID>
```

2. **Use different port**:
```bash
gibram-server --addr :6162
```

3. **Update SDK to match**:
```python
indexer = GibRAMIndexer(
    session_id="my-project",
    port=6162  # Match server port
)
```

#### Invalid Configuration

**Symptom**:
```
Error: Failed to load config: yaml: unmarshal errors
```

**Cause**: Syntax error in `config.yaml`.

**Solutions**:

1. **Validate YAML syntax**:
```bash
# Install yamllint
pip install yamllint

# Check syntax
yamllint config.yaml
```

2. **Check indentation** (YAML is whitespace-sensitive):
```yaml
# ✗ Wrong (mixed tabs/spaces)
server:
	addr: ":6161"

# ✓ Correct (2 spaces)
server:
  addr: ":6161"
```

3. **Use example config**:
```bash
cp config.example.yaml config.yaml
```

#### Permission Denied

**Symptom**:
```
Error: failed to create data directory: permission denied
```

**Cause**: No write permission to data directory.

**Solutions**:

1. **Check permissions**:
```bash
ls -la ./data
```

2. **Fix permissions**:
```bash
# Create directory with correct permissions
mkdir -p ./data
chmod 755 ./data

# Or run as user with permissions
sudo chown -R $USER:$USER ./data
```

3. **Use different directory**:
```bash
gibram-server --data ~/gibram-data
```

#### TLS Certificate Issues

**Symptom**:
```
Error: failed to configure TLS: tls: failed to find any PEM data
```

**Cause**: Certificate file not found or invalid.

**Solutions**:

1. **Verify cert files exist**:
```bash
ls -la /etc/gibram/certs/
```

2. **Check file format** (must be PEM):
```bash
openssl x509 -in server.crt -text -noout
```

3. **Generate new cert**:
```bash
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout server.key \
  -out server.crt \
  -days 365 \
  -subj "/CN=localhost"
```

4. **Use auto-cert for development**:
```yaml
tls:
  auto_cert: true
```

### Server Crashes or Hangs

#### Out of Memory

**Symptom**:
- Server killed by OS
- Logs: "CRITICAL: Memory usage 2048MB / 2048MB"
- Docker: Container restarted

**Cause**: Too much data in sessions without cleanup.

**Diagnosis**:
```bash
# Check memory usage
free -h

# Check server logs
tail -f /var/log/gibram/gibram.log

# Check Docker stats
docker stats gibram
```

**Solutions**:

1. **Increase memory limit** (Docker):
```yaml
# docker-compose.yml
deploy:
  resources:
    limits:
      memory: 4G  # Increase from 2G
```

2. **Enable session TTL** (via protocol):
```python
# Set sessions to expire after 1 hour
# (SDK support coming in future version)
```

3. **Manual cleanup**:
```bash
# Delete expired sessions via CLI
gibram-cli -h localhost:6161

gibram> LIST_SESSIONS
# Check session IDs

gibram> DELETE_SESSION <session_id>
```

4. **Reduce data volume**:
- Index fewer documents
- Use larger chunk_size (fewer chunks)
- Delete unused sessions

#### Too Many Connections

**Symptom**:
```
Error: max sessions limit reached (10000)
```

**Cause**: DoS protection triggered.

**Solutions**:

1. **Check active sessions**:
```bash
gibram-cli> LIST_SESSIONS
```

2. **Clean up old sessions**:
```bash
# Delete specific session
gibram-cli> DELETE_SESSION old-session-id

# Or restart server (if ephemeral mode)
```

3. **Increase limit** (code change required):
```go
// In pkg/engine/engine.go
const MaxSessions = 50000  // Increase from 10000
```

## Client Connection Issues

### Connection Refused

**Symptom** (Python):
```python
ConnectionRefusedError: [Errno 111] Connection refused
```

**Symptom** (Go):
```
failed to connect: dial tcp 127.0.0.1:6161: connect: connection refused
```

**Cause**: Server not running or wrong host/port.

**Solutions**:

1. **Check server is running**:
```bash
ps aux | grep gibram-server

# Expected output:
# user  12345  0.0  0.1  gibram-server --insecure
```

2. **Verify port**:
```bash
# Server logs should show:
# INFO  GibRAM Protobuf Server listening on :6161
```

3. **Test with CLI**:
```bash
gibram-cli -h localhost:6161 -insecure true

gibram> PING
# Should return: PONG
```

4. **Check firewall**:
```bash
# Allow port 6161
sudo ufw allow 6161/tcp
```

5. **Docker network**:
```bash
# If server in Docker, use host.docker.internal
# Or run SDK in same network
docker network create gibram-network
```

### TLS Handshake Failed

**Symptom**:
```
tls: handshake failure: remote error: tls: bad certificate
```

**Cause**: Certificate validation failed.

**Solutions**:

1. **Development: Use insecure mode**:
```bash
gibram-server --insecure
```

```python
# Client: no TLS config needed
indexer = GibRAMIndexer(session_id="test")
```

2. **Skip cert verification** (Go client):
```go
config := client.DefaultPoolConfig()
config.TLSEnabled = true
config.TLSSkipVerify = true  // Dev only!
```

3. **Production: Use valid certificate**:
- CA-signed certificate
- Hostname matches cert CN
- Certificate not expired

### Authentication Failed

**Symptom**:
```
Error: unauthorized
```

**Cause**: Missing or invalid API key.

**Solutions**:

1. **Verify server requires auth**:
```bash
# Server logs should show:
# INFO  Authentication: enabled
```

2. **Check API key matches config**:
```yaml
# config.yaml
auth:
  keys:
    - id: "app"
      key: "your-key-here"
      permissions: ["write"]
```

3. **Use correct key in client** (Go):
```go
config := client.DefaultPoolConfig()
config.APIKey = "your-key-here"
```

4. **Development: Disable auth**:
```bash
gibram-server --insecure  # Disables both TLS and auth
```

## Data Issues

### Dimension Mismatch

**Symptom**:
```
Error: dimension mismatch: expected 1536, got 768
```

**Cause**: Server `vector_dim` ≠ embedding dimension.

**Where it happens**:
- When adding TextUnit with embedding
- When adding Entity with embedding
- When adding Community with embedding

**Solutions**:

1. **Check server dimension**:
```bash
gibram-cli> INFO
# Look for: VectorDim: 1536
```

2. **Match SDK to server**:
```python
# If server uses 1536
indexer = GibRAMIndexer(
    embedding_dimensions=1536  # Default, matches server
)
```

3. **Change server dimension** (requires restart + re-index):
```bash
gibram-server --dim 768
```

4. **Use compatible embedding model**:
```python
# Server: vector_dim = 1536
# SDK: Use OpenAI text-embedding-3-small (1536 dims)
indexer = GibRAMIndexer(
    embedding_model="text-embedding-3-small",
    embedding_dimensions=1536
)
```

**⚠️ WARNING**: Changing `vector_dim` requires re-indexing all data.

### Session Not Found

**Symptom**:
```
Error: session not found
```

**Cause**: Session expired or never created.

**Solutions**:

1. **Check session exists**:
```bash
gibram-cli> LIST_SESSIONS
```

2. **Session TTL expired**:
- Session auto-deleted after TTL
- Re-index data in new session

3. **Server restarted** (ephemeral mode):
- All sessions lost on restart
- Re-index data

4. **Wrong session ID**:
```python
# Check session_id spelling
indexer = GibRAMIndexer(session_id="my-project")  # Must match exactly
```

### Duplicate Entity Error

**Symptom**:
```
Error: entity with title "EINSTEIN" already exists
```

**Cause**: Entity with same title already in session.

**Behavior**: SDK handles this automatically by:
1. Checking if entity exists by title
2. Reusing existing entity ID
3. Linking to text units

**If you see this error**:
- You're using low-level client directly
- Check for existing entity before adding

## SDK Issues

### OpenAI API Errors

#### Rate Limit

**Symptom**:
```
openai.RateLimitError: Rate limit exceeded
```

**Solutions**:

1. **Reduce batch size**:
```python
stats = indexer.index_documents(
    documents,
    batch_size=5  # Slower but less likely to hit rate limit
)
```

2. **Add delay between batches** (custom implementation needed)

3. **Upgrade OpenAI plan** for higher rate limits

#### Invalid API Key

**Symptom**:
```
openai.AuthenticationError: Incorrect API key provided
```

**Solutions**:

1. **Check API key**:
```bash
echo $OPENAI_API_KEY
```

2. **Verify key is valid** on OpenAI dashboard

3. **Pass key explicitly**:
```python
indexer = GibRAMIndexer(
    llm_api_key="sk-...",
    session_id="test"
)
```

#### Quota Exceeded

**Symptom**:
```
openai.RateLimitError: You exceeded your current quota
```

**Solutions**:

1. **Check quota** on OpenAI billing dashboard
2. **Add payment method** or **upgrade plan**

### Extraction Failed

**Symptom**:
```
Warning: Extraction failed for chunk: ...
```

**Cause**: LLM returned invalid JSON or timed out.

**Impact**: Chunk is skipped (indexing continues for other chunks).

**Solutions**:

1. **Check OpenAI service status**
2. **Retry indexing** (transient errors)
3. **Check chunk content** (very long/malformed text can cause issues)

## Performance Issues

### Slow Indexing

**Symptom**: Indexing takes > 10s per document.

**Causes**:

1. **LLM API latency** (main bottleneck)
2. **Large documents** → many chunks
3. **Slow network** to OpenAI API

**Solutions**:

1. **Use faster model**:
```python
indexer = GibRAMIndexer(
    llm_model="gpt-4o-mini"  # Faster than gpt-4o
)
```

2. **Increase chunk size** (fewer LLM calls):
```python
indexer = GibRAMIndexer(
    chunk_size=1024  # Larger chunks = fewer API calls
)
```

3. **Disable community detection**:
```python
indexer = GibRAMIndexer(
    auto_detect_communities=False
)
```

### Slow Queries

**Symptom**: Query takes > 1s.

**Causes**:

1. **Large result set** (high top_k)
2. **Many entities** in session
3. **Deep graph traversal**

**Solutions**:

1. **Reduce top_k**:
```python
result = indexer.query("query", top_k=5)  # Instead of 50
```

2. **Limit result types**:
```python
result = indexer.query(
    "query",
    include_entities=True,
    include_text_units=False,  # Skip if not needed
    include_communities=False
)
```

## Docker Issues

### Container Exits Immediately

**Symptom**:
```bash
docker ps
# gibram container not listed
```

**Solutions**:

1. **Check logs**:
```bash
docker logs gibram
```

2. **Run interactively** to see error:
```bash
docker run -it --rm \
  -p 6161:6161 \
  gibramio/gibram:latest
```

3. **Common causes**:
- Invalid config mounted
- Port conflict (6161 already used on host)
- Memory limit too low

### Can't Connect from Host

**Symptom**: Python SDK on host can't reach Docker container.

**Solutions**:

1. **Check port mapping**:
```bash
docker ps
# Should show: 0.0.0.0:6161->6161/tcp
```

2. **Use correct host**:
```python
# From host machine
indexer = GibRAMIndexer(
    host="localhost",  # Or "127.0.0.1"
    port=6161
)
```

3. **Check Docker network**:
```bash
docker inspect gibram
# Look for "Ports" section
```

## Getting Help

### Gather Information

When reporting issues, include:

1. **GibRAM version**:
```bash
gibram-server --version
```

2. **Server logs**:
```bash
# Last 100 lines
tail -n 100 /var/log/gibram/gibram.log

# Or Docker logs
docker logs gibram --tail 100
```

3. **Configuration** (sanitized):
```yaml
# config.yaml (remove sensitive keys)
```

4. **Reproduction steps**:
- Minimal code example
- Expected vs actual behavior

### Resources

- **GitHub Issues**: [github.com/gibram-io/gibram/issues](https://github.com/gibram-io/gibram/issues)
- **Documentation**: This site
- **Examples**: `examples/` directory in repo

## Next Steps

- **[Configuration Basics](configuration-basics.md)** - Prevent common issues
- **[Python SDK Quickstart](../sdks/python/quickstart.md)** - SDK setup
