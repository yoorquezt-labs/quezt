// Package mcp provides a client for the YoorQuezt MCP MEV server.
// It calls MCP tools via HTTP and returns structured results that
// AI providers can use to answer questions with live data.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// Client communicates with the MCP MEV server over HTTP.
type Client struct {
	baseURL string
	client  *http.Client
	nextID  int64
}

// NewClient creates an MCP client. Default URL: http://localhost:3101.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:3101"
	}
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// ToolDef describes an MCP tool for AI system prompts.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AvailableTools returns the list of all 16 MCP tools that the AI can call.
func AvailableTools() []ToolDef {
	return []ToolDef{
		{"get_bundle", "Get bundle details and status by bundle ID. Params: {\"bundle_id\": \"...\"}"},
		{"list_bundles", "List recent bundles (optional: limit, status filter). Params: {\"limit\": 20, \"status\": \"pending|submitted|landed|failed\"}"},
		{"get_auction", "Get current auction state (block_number=0 for current). Params: {\"block_number\": 0}"},
		{"list_auctions", "List recent auctions. Params: {\"limit\": 10}"},
		{"get_relay_stats", "Relay performance stats (omit relay_id for all). Params: {\"relay_id\": \"...\"}"},
		{"get_mempool_snapshot", "Current mempool state. Params: {}"},
		{"get_ofa_stats", "Order Flow Auction stats. Params: {\"time_range\": \"1h|24h|7d\"}"},
		{"get_sandwich_log", "Recent sandwich attack events. Params: {\"limit\": 20}"},
		{"get_profit_history", "MEV profit by time/strategy. Params: {\"time_range\": \"1h|24h|7d|30d\", \"strategy\": \"arb|sandwich|liquidation|all\"}"},
		{"get_engine_health", "Engine health (uptime, components, errors). Params: {}"},
		{"explain_error", "Explain MEV error code. Params: {\"error_code\": \"BUNDLE_REVERTED\", \"context\": \"...\"}"},
		{"get_block_builder_stats", "Block builder performance. Params: {\"time_range\": \"1h|24h|7d\"}"},
		{"get_solver_stats", "Intent solver performance. Params: {}"},
		{"get_chain_status", "Per-chain MEV status (omit chain_id for all). Params: {\"chain_id\": 1}"},
		{"get_simulation_cache", "Simulation cache stats (hit rate, size). Params: {}"},
		{"order_funnel_stats", "Order flow funnel analytics. Params: {\"time_range\": \"1h|24h|7d\"}"},
	}
}

// jsonrpcRequest is a JSON-RPC 2.0 request for the MCP protocol.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	ID int64 `json:"id"`
}

// CallTool executes an MCP tool by name with the given arguments.
// Returns the result as a JSON string suitable for injection into AI context.
func (c *Client) CallTool(ctx context.Context, toolName string, args json.RawMessage) (string, error) {
	id := atomic.AddInt64(&c.nextID, 1)

	// Build MCP tools/call request
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]any{
			"name":      toolName,
			"arguments": json.RawMessage(args),
		},
		ID: id,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/message", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("mcp call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if rpcResp.Error != nil {
		return "", fmt.Errorf("mcp error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// Extract text content from MCP result
	var mcpResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(rpcResp.Result, &mcpResult); err != nil {
		// If it doesn't match MCP format, return raw
		return string(rpcResp.Result), nil
	}

	if mcpResult.IsError {
		if len(mcpResult.Content) > 0 {
			return "", fmt.Errorf("tool error: %s", mcpResult.Content[0].Text)
		}
		return "", fmt.Errorf("tool error (no details)")
	}

	if len(mcpResult.Content) > 0 {
		return mcpResult.Content[0].Text, nil
	}

	return string(rpcResp.Result), nil
}

// Healthy checks if the MCP server is reachable.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
