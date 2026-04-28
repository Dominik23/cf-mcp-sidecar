# MCP Sidecar (Go)

Zero-dependency sidecar for CF MCP Hub auto-registration. Single binary, works with any app (Python, Java, Node, Go, etc.).

## Features

- **Zero runtime dependencies** - single static binary
- **Reads `mcp-manifest.json`** - sends capabilities directly to Hub (no `/mcp/capabilities` endpoint needed in your app)
- **Auto-registration** - registers on startup
- **Heartbeats** - every 30s
- **Auto-recovery** - re-registers if Hub restarts

## Your App Only Needs

1. **`/health` endpoint** - returns 200 when ready
2. **`mcp-manifest.json`** - describes your API capabilities

That's it! No MCP code in your app.

## Installation

### Download Binary

```bash
# Linux (CF)
curl -L https://github.com/Dominik23/mcp-sidecar-go/releases/latest/download/mcp-sidecar-linux-amd64 -o mcp-sidecar
chmod +x mcp-sidecar

# macOS
curl -L https://github.com/Dominik23/mcp-sidecar-go/releases/latest/download/mcp-sidecar-darwin-amd64 -o mcp-sidecar
chmod +x mcp-sidecar
```

### Build from Source

```bash
# For CF (Linux)
GOOS=linux GOARCH=amd64 go build -o mcp-sidecar .

# For macOS
go build -o mcp-sidecar .
```

## Usage

### CF Sidecar (recommended)

**manifest.yml:**
```yaml
applications:
  - name: my-app
    memory: 256M
    command: java -jar app.jar
    health-check-type: http
    health-check-http-endpoint: /health
    sidecars:
      - name: mcp-registrar
        process_types: [web]
        memory: 16M
        command: ./mcp-sidecar
```

**mcp-manifest.json:**
```json
{
  "name": "my-app",
  "description": "My awesome API",
  "capabilities": [
    {
      "name": "list_items",
      "description": "Lists all items",
      "http": {"method": "GET", "path": "/api/items"}
    }
  ]
}
```

### Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `HUB_URL` | `https://mcp-hub-agent.cfapps.eu12.hana.ondemand.com` | MCP Hub URL |
| `APP_URL` | Auto-detect from `VCAP_APPLICATION` | This app's URL |

## How It Works

```
┌─────────────────────────────────────────────────────────┐
│  CF Container                                           │
│                                                         │
│  ┌─────────────────┐    ┌─────────────────┐            │
│  │   Your App      │    │   Go Sidecar    │            │
│  │   (any lang)    │    │   (16MB binary) │            │
│  │                 │    │                 │            │
│  │ /health ◄───────┼────┤ 1. Wait healthy │            │
│  │ /api/*          │    │ 2. Read manifest│            │
│  │                 │    │ 3. Register ────┼───────────►│ Hub
│  │                 │    │ 4. Heartbeat    │            │
│  └─────────────────┘    └─────────────────┘            │
└─────────────────────────────────────────────────────────┘
```

1. Sidecar starts alongside your app
2. Waits for `/health` to return 200
3. Reads `mcp-manifest.json`
4. Registers with Hub (sends capabilities directly)
5. Sends heartbeats every 30s
6. If heartbeat fails (404), re-registers

## License

MIT
