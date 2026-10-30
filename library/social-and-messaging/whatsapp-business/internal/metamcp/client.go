package metamcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/social-and-messaging/whatsapp-business/internal/config"
)

type Client struct {
	HTTP                              *http.Client
	endpoint, auth, session, protocol string
	limiter                           *cliutil.AdaptiveLimiter
	sequence                          int
	mu                                sync.Mutex
}
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message) }

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Meta MCP HTTP %d", e.Status) }
func New(ctx context.Context, cfg *config.Config, native bool, timeout time.Duration, rate float64) (*Client, error) {
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/whatsapp_business_tools"
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil {
		return nil, fmt.Errorf("invalid MCP endpoint")
	}
	if endpoint != Endpoint && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, fmt.Errorf("MCP endpoint must be the official Meta resource or a loopback fixture")
	}
	auth, err := ResolveAuth(ctx, cfg, native)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if rate <= 0 {
		rate = 2
	}
	return &Client{HTTP: &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}, endpoint: endpoint, auth: auth, protocol: "2025-03-26", limiter: cliutil.NewAdaptiveLimiter(rate)}, nil
}
func (c *Client) Initialize(ctx context.Context) error {
	raw, err := c.request(ctx, "initialize", map[string]any{"protocolVersion": c.ProtocolVersion(), "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "whatsapp-business-pp-cli", "version": "0.1.0"}}, true, false)
	if err != nil {
		return err
	}
	var v struct {
		Protocol string `json:"protocolVersion"`
	}
	if err = json.Unmarshal(raw, &v); err != nil || v.Protocol == "" {
		return fmt.Errorf("MCP initialize did not return protocolVersion")
	}
	c.mu.Lock()
	c.protocol = v.Protocol
	c.mu.Unlock()
	_, err = c.request(ctx, "notifications/initialized", nil, true, true)
	return err
}

// EndpointURL identifies the actual resource used for capture provenance.
func (c *Client) EndpointURL() string { return c.endpoint }

// ProtocolVersion returns the version actually negotiated for this session.
func (c *Client) ProtocolVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.protocol
}
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	all := make([]Tool, 0)
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 20; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.request(ctx, "tools/list", params, true, false)
		if err != nil {
			return nil, err
		}
		var v struct {
			Tools  []Tool `json:"tools"`
			Cursor string `json:"nextCursor"`
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("invalid tools/list response")
		}
		all = append(all, v.Tools...)
		if v.Cursor == "" {
			return all, nil
		}
		if seen[v.Cursor] {
			return nil, fmt.Errorf("tools/list repeated its continuation cursor")
		}
		seen[v.Cursor] = true
		cursor = v.Cursor
	}
	return nil, fmt.Errorf("tools/list exceeded 20-page safety bound")
}
func (c *Client) Call(ctx context.Context, tool Tool, args map[string]any) (any, error) {
	if err := Validate(tool, args); err != nil {
		return nil, err
	}
	secrets := sensitiveValues(args)
	raw, err := c.request(ctx, "tools/call", map[string]any{"name": tool.Name, "arguments": args}, tool.Annotations.ReadOnly, false, secrets...)
	if err != nil {
		return nil, err
	}
	var v struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured any  `json:"structuredContent"`
		IsError    bool `json:"isError"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&v) != nil {
		return nil, fmt.Errorf("invalid tools/call result")
	}
	if v.IsError {
		msg := "tool returned an error"
		for _, x := range v.Content {
			if x.Type == "text" {
				msg = c.safe(x.Text, secrets...)
				break
			}
		}
		return nil, &RPCError{Code: -32000, Message: msg}
	}
	if v.Structured != nil {
		return Normalize(v.Structured), nil
	}
	for _, x := range v.Content {
		if x.Type == "text" {
			var value any
			decoder := json.NewDecoder(strings.NewReader(x.Text))
			decoder.UseNumber()
			if decoder.Decode(&value) == nil {
				return Normalize(value), nil
			}
		}
	}
	return nil, fmt.Errorf("tool returned no structured JSON result; inspect upstream catalog")
}

// Normalize decodes the documented JSON-inside-result envelope, preserving all
// other fields. It never converts unknown or absent state into an empty success.
func Normalize(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if text, ok := m["result"].(string); ok {
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if decoder.Decode(&decoded) == nil {
			if len(m) == 1 {
				return decoded
			}
			copy := map[string]any{}
			for k, v := range m {
				copy[k] = v
			}
			copy["result"] = decoded
			return copy
		}
	}
	return v
}
func (c *Client) safe(s string, secrets ...string) string {
	if c.auth != "" {
		s = strings.ReplaceAll(s, strings.TrimPrefix(c.auth, "Bearer "), "[REDACTED]")
	}
	for _, value := range secrets {
		s = strings.ReplaceAll(s, value, "[REDACTED]")
	}
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return s
}
func (c *Client) request(ctx context.Context, method string, params any, read, notification bool, secrets ...string) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	id := c.sequence
	body := map[string]any{"jsonrpc": "2.0", "method": method}
	if !notification {
		body["id"] = id
	}
	if params != nil {
		body["params"] = params
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	attempts := 1
	if read {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err = c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", c.auth)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", c.protocol)
		if c.session != "" {
			req.Header.Set("Mcp-Session-Id", c.session)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("MCP transport failed; request may have reached server: %w", err)
		}
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			wait := cliutil.RetryAfter(resp)
			if wait <= 0 {
				wait = time.Duration(attempt+1) * time.Second
			}
			_ = resp.Body.Close() // Body close cannot change the already classified or parsed reply.
			c.limiter.OnRateLimit()
			if resp.StatusCode == 429 && attempt == attempts-1 {
				return nil, &cliutil.RateLimitError{URL: c.endpoint, RetryAfter: wait}
			}
			if attempt < attempts-1 && wait <= 5*time.Second {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
					continue
				}
			}
			if resp.StatusCode == 429 {
				return nil, &cliutil.RateLimitError{URL: c.endpoint, RetryAfter: wait}
			}
			return nil, &HTTPError{Status: resp.StatusCode}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close() // Body close cannot change the already classified or parsed reply.
			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				return nil, &AuthError{fmt.Sprintf("Meta MCP HTTP %d; check OAuth grant, business/app roles and accepted terms", resp.StatusCode)}
			}
			return nil, &HTTPError{Status: resp.StatusCode}
		}
		c.limiter.OnSuccess()
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.session = sid
		}
		if notification {
			_ = resp.Body.Close() // Body close cannot change the already classified or parsed reply.
			return json.RawMessage(`{}`), nil
		}
		var raw json.RawMessage
		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			raw, err = readEvent(resp.Body, id)
		} else {
			raw, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
			if len(raw) > 8<<20 {
				err = fmt.Errorf("MCP response exceeds 8 MiB")
			}
		}
		_ = resp.Body.Close() // Body close cannot change the already classified or parsed reply.
		if err != nil {
			return nil, err
		}
		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  json.RawMessage `json:"result"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			return nil, fmt.Errorf("MCP returned invalid JSON")
		}
		if envelope.Error != nil {
			return nil, &RPCError{Code: envelope.Error.Code, Message: c.safe(envelope.Error.Message, secrets...)}
		}
		if string(envelope.ID) != strconv.Itoa(id) || envelope.JSONRPC != "2.0" || len(envelope.Result) == 0 {
			return nil, fmt.Errorf("MCP response has mismatched id or missing result")
		}
		return envelope.Result, nil
	}
	return nil, fmt.Errorf("MCP retry budget exhausted")
}
func readEvent(r io.Reader, id int) (json.RawMessage, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, 16<<20))
	scanner.Buffer(make([]byte, 4096), 8<<20)
	var lines []string
	consume := func() (json.RawMessage, bool) {
		if len(lines) == 0 {
			return nil, false
		}
		raw := []byte(strings.Join(lines, "\n"))
		lines = nil
		var v struct {
			ID    json.RawMessage `json:"id"`
			Error any             `json:"error"`
		}
		if json.Unmarshal(raw, &v) == nil && (string(v.ID) == strconv.Itoa(id) || v.Error != nil) {
			return raw, true
		}
		return nil, false
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if raw, ok := consume(); ok {
				return raw, nil
			}
		} else if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if raw, ok := consume(); ok {
		return raw, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("MCP event stream ended without matching response")
}
