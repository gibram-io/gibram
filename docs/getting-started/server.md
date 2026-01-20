# Run GibRAM Server (v0.1.0)

Get GibRAM server running on your machine. This guide covers the fastest path to a working server.

## Prerequisites

- **Operating System**: Linux, macOS, or Windows (WSL)
- **Memory**: 1GB+ RAM recommended
- **Port**: 6161 must be available (default)

## Install Methods

### Option 1: Binary Install (Fastest)

Download and run the pre-built binary:

```bash
# Install via script
curl -fsSL https://gibram.io/install.sh | sh

# Verify installation
gibram-server --version
```

The binary will be installed to `/usr/local/bin/gibram-server`.

### Option 2: Docker (Recommended for Production)

```bash
# Run server
docker run -d \
  -p 6161:6161 \
  --name gibram \
  gibramio/gibram:latest
```

For persistent data:

```bash
docker run -d \
  -p 6161:6161 \
  -v gibram-data:/var/lib/gibram/data \
  --name gibram \
  gibramio/gibram:latest
```

### Option 3: Build from Source

Requires Go 1.24+:

```bash
git clone https://github.com/gibram-io/gibram.git
cd gibram
go build -o gibram-server ./cmd/server
./gibram-server --insecure
```

## Start the Server

### Development Mode (Insecure)

For local development, use insecure mode (no TLS, no auth):

```bash
gibram-server --insecure
```

Expected output:

```
INFO  GibRAM v0.1.0 starting...
INFO    Address:    :6161
INFO    Data dir:   ./data
INFO    Vector dim: 1536
INFO    Log level:  info
INFO    Protocol:   GibRAM Protocol v1 (proto3)
WARN  Running in INSECURE mode (no TLS, no auth)
INFO  GibRAM Protobuf Server listening on :6161
INFO    Authentication: disabled (insecure)
INFO    Max frame size: 67108864 bytes
INFO    Rate limit: 1000 req/s (burst: 100)
INFO    Metrics:    enabled
INFO    Memory:     monitoring enabled (max 1024MB)
```

**⚠️ CRITICAL**: `--insecure` mode disables all security. **NEVER use in production.**

### Verify Server is Running

Test with CLI client:

```bash
# In another terminal
gibram-cli -h localhost:6161 -insecure true

# Inside CLI, type:
gibram> PING
# Expected: PONG (XXXms)

gibram> INFO
# Expected: Server stats with 0 entities/documents
```

## Configuration

### Server Listens On

- **Address**: `0.0.0.0:6161` (all interfaces, port 6161)
- **Protocol**: GibRAM Protocol v1 (Protobuf over TCP)
- **Data Directory**: `./data` (default)
- **Vector Dimension**: 1536 (default, matches OpenAI text-embedding-3-small)

### Common Flags

```bash
gibram-server \
  --addr :6161 \                    # Bind address
  --data ./data \                   # Data directory
  --dim 1536 \                      # Vector dimension
  --log-level info \                # Log level (debug|info|warn|error)
  --session-cleanup-interval 60s    # Session TTL check interval
```

### Config File (Advanced)

Create `config.yaml`:

```yaml
server:
  addr: ":6161"
  data_dir: "./data"
  vector_dim: 1536

logging:
  level: "info"
  format: "text"
```

Run with config:

```bash
gibram-server --config config.yaml
```

**Precedence**: CLI flags override config file values.

## Production Setup

For production deployment:

1. **Use Config File**: See [Configuration Basics](../server/configuration-basics.md)
2. **Enable TLS**: Required for network security
3. **Enable Auth**: API key authentication
4. **Resource Limits**: Docker memory limits recommended

**DO NOT** use `--insecure` flag in production.

## Session Concept

GibRAM is **session-based**. Each session is an isolated namespace:

- **Session ID**: Required in all API calls (e.g., "my-project")
- **TTL**: Sessions auto-expire (default: no limit, but configurable)
- **Isolation**: Data in session "A" cannot be accessed by session "B"

Sessions are created automatically on first write. No explicit setup needed.

## Data Lifecycle

**By Default (In-Memory Ephemeral)**:
- Data lives only in RAM
- Server restart = all data lost
- Session TTL expires = data cleaned up

**Optional (Persistence Enabled)**:
- WAL (Write-Ahead Log) for durability
- Snapshot for fast restore
- See [Configuration Basics](../server/configuration-basics.md)

## Troubleshooting

### Server Won't Start

**Symptom**: Error "address already in use"

**Cause**: Port 6161 is taken

**Fix**:
```bash
# Check what's using port 6161
lsof -i :6161

# Use different port
gibram-server --addr :6162 --insecure
```

### Client Can't Connect

**Symptom**: "connection refused"

**Check**:
1. Server is running: `ps aux | grep gibram-server`
2. Port is correct: Server logs show "listening on :6161"
3. Firewall allows port 6161

### Out of Memory

**Symptom**: Server killed or slows down

**Cause**: Too much data in sessions without cleanup

**Fix**:
1. Set session TTL (see Configuration)
2. Manually delete sessions via protocol
3. Increase Docker memory limit
4. Monitor with metrics (see Observability)

## Next Steps

- **[Use Python SDK](python-sdk.md)** - Index your first documents
- **[Configuration Basics](../server/configuration-basics.md)** - TLS, auth, persistence
- **[Troubleshooting](../server/troubleshooting.md)** - Common issues
