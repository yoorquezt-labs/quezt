<h1 align="center">yqmev</h1>

<p align="center">
  <em>MEV terminal dashboard by <a href="https://yoorquezt.io">YoorQuezt</a></em>
</p>

<p align="center">
  <a href="https://github.com/yoorquezt-labs/yqmev"><img src="https://img.shields.io/badge/go-1.24+-00ADD8?style=flat-square&logo=go" alt="Go"></a>
  <a href="https://goreportcard.com/report/github.com/yoorquezt-labs/yqmev"><img src="https://goreportcard.com/badge/github.com/yoorquezt-labs/yqmev?style=flat-square" alt="Go Report"></a>
  <a href="https://github.com/yoorquezt-labs/yqmev/blob/master/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=flat-square" alt="License"></a>
</p>

<p align="center">
  <a href="#install">Install</a> &middot;
  <a href="#quick-start">Quick Start</a> &middot;
  <a href="#tabs">Tabs</a> &middot;
  <a href="#q-ai-assistant">Q AI</a> &middot;
  <a href="#sdk">SDK</a> &middot;
  <a href="#development">Development</a>
</p>

---

A terminal UI for monitoring, analyzing, and interacting with YoorQuezt MEV infrastructure. Think [k9s](https://k9s.io) for MEV.

```
yqmev --gateway ws://your-gateway:9099/ws
```

## Install

```bash
# npm (recommended)
npm install -g yqmev

# homebrew
brew install yoorquezt-labs/tap/yqmev

# one-line installer
curl -fsSL https://yqmev.dev/install | sh

# go install
go install github.com/yoorquezt-labs/yqmev/cmd/yqmev@latest

# from source
git clone https://github.com/yoorquezt-labs/yqmev.git
cd yqmev && make install
```

## Quick Start

```bash
# 1. Connect to a running MEV gateway
yqmev --gateway ws://localhost:9099/ws

# 2. With authentication
yqmev --gateway ws://mev.example.com:9099/ws --api-key YOUR_KEY

# 3. Enable AI assistant (pick a provider)
yqmev --gateway ws://localhost:9099/ws --ai-provider claude --ai-key sk-ant-...
yqmev --gateway ws://localhost:9099/ws --ai-provider openai --ai-key sk-...
yqmev --gateway ws://localhost:9099/ws --ai-provider ollama --ai-model llama3.1
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--gateway` | `ws://localhost:9099/ws` | MEV gateway WebSocket URL |
| `--api-key` | | Bearer token for gateway auth |
| `--ai-provider` | `claude` | AI provider: `claude`, `openai`, `ollama` |
| `--ai-key` | | API key for Claude or OpenAI |
| `--ai-model` | provider default | Override AI model name |
| `--ai-url` | provider default | Override AI base URL (useful for Ollama) |

## Tabs

| Key | Tab | Description |
|-----|-----|-------------|
| `1` | **Dashboard** | Live overview — health, mempool, blocks, bundles, relays |
| `2` | **Bundles** | Browse and inspect MEV bundles in the auction pool |
| `3` | **Auction** | Real-time sealed-bid auction feed via WebSocket |
| `4` | **Blocks** | Built blocks with profit breakdown |
| `5` | **Relay** | Relay marketplace — registration, reputation, stats |
| `6` | **OFA Protect** | Order Flow Auction — protected txs, sandwich detection, rebates |
| `7` | **Intents** | Intent submission and solver resolution |
| `8` | **Orderflow** | Orderflow payment tracking and analytics |
| `9` | **System** | Simulation cache, auth stats, gateway diagnostics |
| `0` | **Q AI** | AI chat assistant with live MEV engine context |
| `-` | **Logs** | Real-time WebSocket event stream |
| `=` | **Help** | Keyboard shortcuts and navigation guide |
| `\` | **Settings** | Gateway URL, API key, AI provider configuration |

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Next / previous tab |
| `1`-`9`, `0`, `-`, `=`, `\` | Jump to tab |
| `r` | Refresh current view |
| `q` / `Ctrl+C` | Quit |
| `/` | Filter (in list views) |
| `Enter` | Select / submit |
| `Esc` | Back / cancel |
| `j` / `k` | Scroll down / up |

## Q AI Assistant

The Q AI tab provides an AI-powered MEV assistant with **live engine context**. It automatically injects your current mempool size, bundle count, relay status, OFA protection stats, and recent WebSocket events into the AI prompt — no copy-pasting needed.

**Streaming**: Responses appear token-by-token in real time using SSE streaming (Claude and OpenAI) with the BubbleTea recursive cmd pattern.

**Providers**:

| Provider | Model | API Key Required | Notes |
|----------|-------|------------------|-------|
| Claude | claude-sonnet-4 | Yes | Best MEV analysis, default |
| OpenAI | gpt-4o | Yes | Fast, good general analysis |
| Ollama | llama3.1 | No | Fully local, private |

**Example questions**:
```
> What's the current state of the auction pool?
> Why did my last bundle revert?
> How can I improve my bid strategy?
> Analyze the recent sandwich attacks in the OFA feed
> What arb opportunities exist between the top relay bids?
```

## SDK

The `pkg/` packages can be imported as a Go SDK in your own projects:

```go
import (
    "github.com/yoorquezt-labs/yqmev/pkg/client"
    "github.com/yoorquezt-labs/yqmev/pkg/jsonrpc"
    "github.com/yoorquezt-labs/yqmev/pkg/types"
)

// Connect to the MEV gateway
c, err := client.Dial(client.Config{
    GatewayURL: "ws://localhost:9099/ws",
    APIKey:     "your-key",
})
defer c.Close()

// Submit a bundle
result, err := c.SendBundle(ctx, types.BundleMessage{
    BundleID:     "bundle-001",
    Transactions: txs,
    BidWei:       "1000000000000000",
})

// Subscribe to auction events
subID, events, err := c.Subscribe(ctx, jsonrpc.TopicAuction)
for event := range events {
    fmt.Println("Auction event:", string(event))
}
```

### Available SDK Methods

| Category | Methods |
|----------|---------|
| **Bundles** | `SendBundle`, `GetBundle`, `GetAuction`, `SimulateBundle`, `SimulateTx` |
| **Protection** | `ProtectTx`, `GetProtectStatus` |
| **Intents** | `SubmitIntent`, `GetIntent` |
| **Relay** | `RelayRegister`, `RelayList`, `RelayStats` |
| **Store** | `ListBundles`, `ListBlocks` |
| **System** | `Health`, `OrderflowSummary` |
| **Subscriptions** | `Subscribe`, `Unsubscribe` |

## Architecture

```
                    ┌─────────────────────────┐
                    │     yqmev (terminal)     │
                    │  BubbleTea + Lipgloss    │
                    │  13 tabs, AI assistant   │
                    └────────────┬────────────┘
                                 │
                    WebSocket JSON-RPC 2.0
                                 │
                    ┌────────────▼────────────┐
                    │   yqmev gateway (WS)    │
                    │   27 RPC methods        │
                    │   5 subscription topics  │
                    └────────────┬────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              │                  │                   │
    ┌─────────▼──────┐ ┌────────▼────────┐ ┌───────▼────────┐
    │  MEV Engine     │ │  Relay Market   │ │  OFA Protect   │
    │  Auction, Arb   │ │  Multi-relay    │ │  Sandwich det  │
    │  Block building │ │  Reputation     │ │  90% rebates   │
    └────────────────┘ └─────────────────┘ └────────────────┘
```

`yqmev` is a **pure client** — it connects to the YoorQuezt MEV gateway over WebSocket and renders data in the terminal. No private engine code is included in the binary.

## Development

### Prerequisites

- Go 1.24+
- [GoReleaser](https://goreleaser.com) (for releases)

### Build & Run

```bash
# clone
git clone https://github.com/yoorquezt-labs/yqmev.git
cd yqmev

# build (use GOWORK=off if inside a Go workspace)
GOWORK=off make build
./bin/yqmev --gateway ws://localhost:9099/ws

# install to $GOPATH/bin
GOWORK=off make install

# run tests
GOWORK=off make test

# lint
make lint
```

### Release

Releases are automated via GitHub Actions + GoReleaser. To cut a release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

This will:
1. Build binaries for linux/darwin/windows (amd64 + arm64)
2. Create a GitHub Release with checksums
3. Update the Homebrew tap formula
4. Publish the npm package

### Test a release locally

```bash
GOWORK=off make snapshot   # builds everything without publishing
ls dist/
```

### Project Structure

```
yqmev/
├── cmd/yqmev/           # TUI entry point (3000+ lines, 13 tabs)
├── internal/ai/         # AI providers (Claude, OpenAI, Ollama)
├── pkg/
│   ├── client/          # WebSocket JSON-RPC 2.0 SDK
│   ├── jsonrpc/         # Protocol types, methods, error codes
│   └── types/           # MEV domain types (bundles, txs, blocks)
├── npm/                 # npm package wrapper
├── scripts/             # Install scripts
├── .goreleaser.yaml     # Cross-platform build config
└── .github/workflows/   # CI/CD
```

## Related Projects

| Project | Description |
|---------|-------------|
| [yoorquezt-mesh](https://github.com/yoorquezt-labs/yoorquezt-mesh) | P2P relay network (QUIC, gossip, block building) |
| [yoorquezt-sdk-mev-ts](https://github.com/yoorquezt-labs/yoorquezt-sdk-mev-ts) | TypeScript MEV SDK |
| [yoorquezt-mev-sdk-python](https://github.com/yoorquezt-labs/yoorquezt-sdk-mev-py) | Python MEV SDK |
| [q8s](https://github.com/yoorquezt-labs/q8s) | Kubernetes TUI dashboard, powered by Q AI |

## License

[MIT](LICENSE)
