package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yoorquezt-labs/yqmev/internal/mcp"
)

// buildMessages converts conversation history + current question into API messages.
func buildMessages(req AnalyzeRequest) []map[string]string {
	var messages []map[string]string
	for _, m := range req.History {
		messages = append(messages, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": req.Question,
	})
	return messages
}

// mevSystemPrompt generates the MEV-specific system prompt with live engine context.
func mevSystemPrompt(ctx MEVContext) string {
	var sb strings.Builder

	sb.WriteString("You are Q, an AI-powered MEV assistant running in the yqmev terminal dashboard. ")
	sb.WriteString("You are part of YoorQuezt Technologies — a decentralized MEV infrastructure protocol. ")
	sb.WriteString("You help users monitor MEV activity, analyze bundles, diagnose issues, and optimize strategies.\n\n")

	sb.WriteString("Your expertise includes:\n")
	sb.WriteString("- MEV extraction: sandwich attacks, arbitrage (binary, triangular), liquidations, backrunning\n")
	sb.WriteString("- Bundle construction and optimization (gas prediction, bid strategy, win rate)\n")
	sb.WriteString("- Order Flow Auctions (OFA) with 90% user rebates\n")
	sb.WriteString("- P2P mesh relay network (QUIC transport, gossip protocol)\n")
	sb.WriteString("- Intent solving and multi-hop routing across DEXes\n")
	sb.WriteString("- Multi-chain: Ethereum, Arbitrum, Base, Optimism, BSC, Solana\n")
	sb.WriteString("- Relay marketplace with reputation scoring\n\n")

	sb.WriteString("Be concise — responses appear in a terminal panel. Use short paragraphs. ")
	sb.WriteString("When suggesting commands, format them as code blocks using yqmev CLI. ")
	sb.WriteString("When analyzing bundles or MEV, lead with the key insight.\n\n")

	sb.WriteString("── Current MEV Engine State ──\n")

	if ctx.GatewayURL != "" {
		sb.WriteString(fmt.Sprintf("Gateway: %s\n", ctx.GatewayURL))
	}

	status := "disconnected"
	if ctx.Connected {
		status = "connected"
	}
	health := "unknown"
	if ctx.Healthy {
		health = "healthy"
	}
	sb.WriteString(fmt.Sprintf("Status: %s | Health: %s\n", status, health))

	sb.WriteString(fmt.Sprintf("Mempool: %d txs | Blocks built: %d | WS events: %d\n",
		ctx.PoolSize, ctx.BlockCount, ctx.WSEvents))

	if ctx.TopBid != "" {
		sb.WriteString(fmt.Sprintf("Top bid: %s | Last profit: %s\n", ctx.TopBid, ctx.LastProfit))
	}

	sb.WriteString(fmt.Sprintf("Bundles: %d | Relays: %d active / %d registered\n",
		ctx.BundleCount, ctx.ActiveRelays, ctx.RegisteredRelays))

	if ctx.TxsProtected > 0 || ctx.SandwichBlocked > 0 {
		sb.WriteString(fmt.Sprintf("OFA Protection: %d txs protected | %d sandwiches blocked | MEV captured: %s\n",
			ctx.TxsProtected, ctx.SandwichBlocked, ctx.MEVCaptured))
	}

	if ctx.SolverCount > 0 {
		sb.WriteString(fmt.Sprintf("Intent solvers: %d\n", ctx.SolverCount))
	}

	if ctx.SimCacheTotal > 0 {
		sb.WriteString(fmt.Sprintf("Simulation cache: %d total / %d valid\n", ctx.SimCacheTotal, ctx.SimCacheValid))
	}

	if len(ctx.RecentEvents) > 0 {
		sb.WriteString("\nRecent WS events:\n")
		limit := 10
		if len(ctx.RecentEvents) < limit {
			limit = len(ctx.RecentEvents)
		}
		for _, e := range ctx.RecentEvents[len(ctx.RecentEvents)-limit:] {
			sb.WriteString(fmt.Sprintf("  %s\n", e))
		}
	}

	// MCP tools available
	if ctx.MCPAvailable {
		sb.WriteString("\n── Available MCP Tools ──\n")
		sb.WriteString("You have access to live MEV engine tools. To call a tool, respond with a JSON block:\n")
		sb.WriteString("```tool\n{\"tool\": \"tool_name\", \"args\": {\"param\": \"value\"}}\n```\n")
		sb.WriteString("The tool result will be injected into the conversation and you can then summarize it.\n")
		sb.WriteString("IMPORTANT: Only use ONE tool call per response. Wait for the result before calling another.\n\n")

		tools := mcp.AvailableTools()
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("  • %s — %s\n", t.Name, t.Description))
		}
		sb.WriteString("\nPrefer using tools over guessing. When asked about live data, ALWAYS call the relevant tool first.\n")
	}

	return sb.String()
}

// ParseToolCall extracts a tool call from an AI response.
// Returns nil if no tool call is found.
func ParseToolCall(response string) *ToolCall {
	// Look for ```tool ... ``` blocks
	start := strings.Index(response, "```tool\n")
	if start == -1 {
		start = strings.Index(response, "```tool\r\n")
	}
	if start == -1 {
		return nil
	}
	start = strings.Index(response[start:], "\n") + start + 1
	end := strings.Index(response[start:], "```")
	if end == -1 {
		return nil
	}
	jsonStr := strings.TrimSpace(response[start : start+end])

	var call struct {
		Tool string          `json:"tool"`
		Args json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &call); err != nil {
		return nil
	}
	if call.Tool == "" {
		return nil
	}

	return &ToolCall{
		Name: call.Tool,
		Args: string(call.Args),
	}
}
