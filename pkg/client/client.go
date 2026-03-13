// Package client provides a Go SDK for connecting to the YoorQuezt MEV gateway
// over WebSocket using JSON-RPC 2.0.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/yoorquezt-labs/yqmev/pkg/jsonrpc"
	"github.com/yoorquezt-labs/yqmev/pkg/types"
)

// Client connects to the MEV gateway WebSocket and provides a typed API
// for all JSON-RPC methods.
type Client struct {
	conn      *websocket.Conn
	mu        sync.Mutex
	nextID    int64
	pending   map[int64]chan *jsonrpc.Response
	subs      map[string]chan json.RawMessage
	subsMu    sync.RWMutex
	done      chan struct{}
	closeOnce sync.Once
}

// Config holds connection parameters for the gateway.
type Config struct {
	GatewayURL string // e.g. "ws://localhost:9099/ws"
	APIKey     string // bearer token for authentication
}

// Dial connects to the MEV gateway WebSocket endpoint.
func Dial(cfg Config) (*Client, error) {
	header := http.Header{}
	if cfg.APIKey != "" {
		header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	conn, _, err := websocket.DefaultDialer.Dial(cfg.GatewayURL, header)
	if err != nil {
		return nil, fmt.Errorf("dial gateway: %w", err)
	}

	c := &Client{
		conn:    conn,
		pending: make(map[int64]chan *jsonrpc.Response),
		subs:    make(map[string]chan json.RawMessage),
		done:    make(chan struct{}),
	}

	go c.readLoop()
	return c, nil
}

// Close gracefully shuts down the client.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		err = c.conn.Close()

		c.mu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()

		c.subsMu.Lock()
		for id, ch := range c.subs {
			close(ch)
			delete(c.subs, id)
		}
		c.subsMu.Unlock()
	})
	return err
}

// --- Bundle operations ---

func (c *Client) SendBundle(ctx context.Context, bundle types.BundleMessage) (map[string]interface{}, error) {
	raw, err := c.Call(ctx, jsonrpc.MethodSendBundle, bundle)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}
	return out, nil
}

func (c *Client) GetBundle(ctx context.Context, bundleID string) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodGetBundle, map[string]string{"bundle_id": bundleID})
}

func (c *Client) GetAuction(ctx context.Context) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodGetAuction, nil)
}

func (c *Client) SimulateBundle(ctx context.Context, bundle types.BundleMessage) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodSimulateBundle, bundle)
}

func (c *Client) SimulateTx(ctx context.Context, tx types.TransactionMessage) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodSimulateTx, tx)
}

// --- Protected transactions ---

func (c *Client) ProtectTx(ctx context.Context, tx types.ProtectedTransaction) (map[string]interface{}, error) {
	raw, err := c.Call(ctx, jsonrpc.MethodProtectTx, tx)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}
	return out, nil
}

func (c *Client) GetProtectStatus(ctx context.Context, txID string) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodGetProtectStatus, map[string]string{"tx_id": txID})
}

// --- Intents ---

func (c *Client) SubmitIntent(ctx context.Context, intent map[string]interface{}) (map[string]interface{}, error) {
	raw, err := c.Call(ctx, jsonrpc.MethodSubmitIntent, intent)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}
	return out, nil
}

func (c *Client) GetIntent(ctx context.Context, intentID string) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodGetIntent, map[string]string{"intent_id": intentID})
}

// --- Relay ---

func (c *Client) RelayRegister(ctx context.Context, params map[string]interface{}) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodRelayRegister, params)
}

func (c *Client) RelayList(ctx context.Context) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodRelayList, nil)
}

func (c *Client) RelayStats(ctx context.Context) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodRelayStats, nil)
}

// --- Store ---

func (c *Client) ListBundles(ctx context.Context) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodListBundles, nil)
}

func (c *Client) ListBlocks(ctx context.Context) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodListBlocks, nil)
}

// --- System ---

func (c *Client) Health(ctx context.Context) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodHealth, nil)
}

func (c *Client) OrderflowSummary(ctx context.Context) (json.RawMessage, error) {
	return c.Call(ctx, jsonrpc.MethodOrderflowSummary, nil)
}

// --- Subscriptions ---

func (c *Client) Subscribe(ctx context.Context, topic string) (string, <-chan json.RawMessage, error) {
	raw, err := c.Call(ctx, jsonrpc.MethodSubscribe, map[string]string{"topic": topic})
	if err != nil {
		return "", nil, err
	}

	var res struct {
		SubID string `json:"subscription"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", nil, fmt.Errorf("decode subscription result: %w", err)
	}

	ch := make(chan json.RawMessage, 64)
	c.subsMu.Lock()
	c.subs[res.SubID] = ch
	c.subsMu.Unlock()

	return res.SubID, ch, nil
}

func (c *Client) Unsubscribe(ctx context.Context, subID string) error {
	_, err := c.Call(ctx, jsonrpc.MethodUnsubscribe, map[string]string{"subscription": subID})

	c.subsMu.Lock()
	if ch, ok := c.subs[subID]; ok {
		close(ch)
		delete(c.subs, subID)
	}
	c.subsMu.Unlock()

	return err
}

// --- Low-level call ---

func (c *Client) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)

	req, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	ch := make(chan *jsonrpc.Response, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.mu.Lock()
	err = c.conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("write message: %w", err)
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("client closed")
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("client closed")
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// --- Internal ---

func (c *Client) readLoop() {
	defer func() {
		c.closeOnce.Do(func() { close(c.done) })

		c.mu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()

		c.subsMu.Lock()
		for id, ch := range c.subs {
			close(ch)
			delete(c.subs, id)
		}
		c.subsMu.Unlock()
	}()

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var resp jsonrpc.Response
		if err := json.Unmarshal(data, &resp); err == nil && resp.ID != nil {
			c.dispatchResponse(&resp)
			continue
		}

		var notif jsonrpc.Notification
		if err := json.Unmarshal(data, &notif); err == nil && notif.Method != "" {
			c.dispatchNotification(&notif)
			continue
		}

		c.dispatchLegacyEvent(data)
	}
}

func (c *Client) dispatchResponse(resp *jsonrpc.Response) {
	var id int64
	switch v := resp.ID.(type) {
	case float64:
		id = int64(v)
	case int64:
		id = v
	case json.Number:
		n, _ := v.Int64()
		id = n
	default:
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if ok {
		ch <- resp
	}
}

func (c *Client) dispatchNotification(notif *jsonrpc.Notification) {
	var event jsonrpc.SubscriptionEvent
	if err := json.Unmarshal(notif.Params, &event); err != nil {
		return
	}

	subID := event.Subscription
	if subID == "" {
		return
	}

	resultData, err := json.Marshal(event.Result)
	if err != nil {
		return
	}

	c.subsMu.RLock()
	ch, exists := c.subs[subID]
	c.subsMu.RUnlock()

	if exists {
		select {
		case ch <- json.RawMessage(resultData):
		default:
		}
	}
}

func (c *Client) dispatchLegacyEvent(data []byte) {
	c.subsMu.RLock()
	defer c.subsMu.RUnlock()

	for _, ch := range c.subs {
		select {
		case ch <- json.RawMessage(data):
		default:
		}
	}
}

func (c *Client) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
