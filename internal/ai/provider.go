package ai

import "context"

// Provider is the interface that all AI backends must implement.
type Provider interface {
	Name() string
	Analyze(ctx context.Context, req AnalyzeRequest) (string, error)
	Stream(ctx context.Context, req AnalyzeRequest, out chan<- string) error
}

// ToolCall represents a tool call request from the AI.
type ToolCall struct {
	Name string `json:"name"`
	Args string `json:"args"` // JSON string
}

// ToolResult holds the result of executing an MCP tool.
type ToolResult struct {
	Name   string `json:"name"`
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
}

// AnalyzeRequest contains the context and question for AI analysis.
type AnalyzeRequest struct {
	Question   string
	MEVContext MEVContext
	History    []Message
}

// MEVContext provides MEV engine state to the AI.
type MEVContext struct {
	GatewayURL       string
	Connected        bool
	Healthy          bool
	PoolSize         int
	BlockCount       int
	WSEvents         int
	BundleCount      int
	TopBid           string
	LastProfit       string
	ActiveRelays     int
	RegisteredRelays int
	TxsProtected     int
	SandwichBlocked  int
	MEVCaptured      string
	PendingRebates   int
	SolverCount      int
	SimCacheTotal    int
	SimCacheValid    int
	OrderflowWei     string
	RecentEvents     []string
	MCPAvailable     bool
}

// Message represents a chat message.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}
