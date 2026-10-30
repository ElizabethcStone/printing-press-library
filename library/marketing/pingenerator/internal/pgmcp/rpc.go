// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package pgmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Path is the MCP endpoint path, joined to the configured base URL
// (https://europe-west3-pingenerator-43a15.cloudfunctions.net/highMemLimitApi).
const Path = "/mcp"

// AcceptHeader is what MCP Streamable HTTP servers expect from clients. Pin
// Generator answers tools/call with plain application/json, but a server may
// frame the reply as a single SSE event.
const AcceptHeader = "application/json, text/event-stream"

// ReplaySafeHeader mirrors client.ReplaySafeHeader: an internal per-request
// marker (never sent) that tells the generated client whether an ambiguous
// failure may be retried. Paid or unknown tools send "false".
const ReplaySafeHeader = "X-Printing-Press-Replay-Safe"

// Poster is the slice of the generated internal/client the MCP layer needs.
// The generated client applies the Bearer access key, base URL overrides,
// timeouts, retries and rate limiting.
type Poster interface {
	PostWithHeaders(ctx context.Context, path string, body any, headers map[string]string) (json.RawMessage, int, error)
}

// Request is the exact JSON-RPC body a tool call sends.
type Request struct {
	JSONRPC string     `json:"jsonrpc"`
	ID      int        `json:"id"`
	Method  string     `json:"method"`
	Params  CallParams `json:"params"`
}

// CallParams are tools/call parameters.
type CallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// BuildRequest returns the JSON-RPC tools/call body for name with args.
// A nil args map becomes {} because the server expects an arguments object.
func BuildRequest(name string, args map[string]any) Request {
	if args == nil {
		args = map[string]any{}
	}
	return Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: CallParams{Name: name, Arguments: args}}
}

// Kind classifies a tool failure so the CLI can map it to a typed exit code.
type Kind int

const (
	KindGeneric   Kind = iota
	KindAuth           // missing/invalid access key
	KindCredits        // no credits remaining
	KindRateLimit      // too many requests
	KindNotFound       // the named resource or page does not exist
	// KindUnreadable: the call returned but the reply could not be read
	// (malformed JSON, no result). Unlike an explicit rejection, a paid call
	// may have been accepted, so callers treat it as an ambiguous spend.
	KindUnreadable
)

// Error is a classified tool failure reported inside a 200 JSON-RPC reply.
type Error struct {
	Kind    Kind
	Tool    string
	Message string
}

func (e *Error) Error() string {
	if e.Tool == "" {
		return e.Message
	}
	return e.Tool + ": " + e.Message
}

// Call invokes one tool and returns its unwrapped JSON payload.
func Call(ctx context.Context, p Poster, name string, args map[string]any) (json.RawMessage, error) {
	headers := map[string]string{"Accept": AcceptHeader}
	// Only known read tools may be retried after an ambiguous failure; a
	// resent tools/call to a paid or unknown tool could charge twice.
	if t, ok := Lookup(name); !ok || t.Mutating {
		headers[ReplaySafeHeader] = "false"
	}
	raw, _, err := p.PostWithHeaders(ctx, Path, BuildRequest(name, args), headers)
	if err != nil {
		return nil, err
	}
	return Unwrap(name, raw)
}

// Unwrap decodes a tools/call reply. The server shapes success as
//
//	{"result":{"content":[{"type":"text","text":"<JSON payload string>"}]}}
//
// and reports failures either as a top-level JSON-RPC "error" object or as
// result.isError with the message in the content text.
func Unwrap(tool string, raw []byte) (json.RawMessage, error) {
	payload := StripSSE(raw)
	var env struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result *struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, &Error{Kind: KindUnreadable, Tool: tool, Message: "unreadable MCP response: " + clip(payload, 300)}
	}
	if env.Error != nil {
		return nil, Classify(tool, env.Error.Message)
	}
	if env.Result == nil {
		return nil, &Error{Kind: KindUnreadable, Tool: tool, Message: "MCP response has no result or error"}
	}
	text := ""
	for _, c := range env.Result.Content {
		if c.Text != "" {
			text = c.Text
			break
		}
	}
	if env.Result.IsError {
		return nil, Classify(tool, strings.TrimSpace(text))
	}
	if text == "" {
		// Non-text content: hand back the whole result object.
		res, _ := json.Marshal(env.Result)
		return res, nil
	}
	trimmed := strings.TrimSpace(text)
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed), nil
	}
	wrapped, _ := json.Marshal(map[string]string{"text": text})
	return wrapped, nil
}

// Classify maps a server error message to a Kind using the phrases Pin
// Generator returns.
func Classify(tool, msg string) *Error {
	lower := strings.ToLower(msg)
	kind := KindGeneric
	switch {
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests"):
		kind = KindRateLimit
	case strings.Contains(lower, "credit"):
		kind = KindCredits
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "user not found") ||
		strings.Contains(lower, "access key") || strings.Contains(lower, "invalid token"):
		kind = KindAuth
	case strings.Contains(lower, "not found") || strings.Contains(lower, "404") ||
		strings.Contains(lower, "could not read url") || strings.Contains(lower, "does not exist"):
		kind = KindNotFound
	}
	if msg == "" {
		msg = "tool reported an error with no message"
	}
	return &Error{Kind: kind, Tool: tool, Message: msg}
}

// StripSSE pulls the JSON out of a single-event text/event-stream body. A plain
// JSON body is returned unchanged.
func StripSSE(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(s, "data:") && !strings.HasPrefix(s, "event:") && !strings.Contains(s, "\ndata:") {
		return raw
	}
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			b.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if b.Len() == 0 {
		return raw
	}
	return []byte(b.String())
}

func clip(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// BuildArgs converts typed flag values into tools/call arguments. vals maps
// MCP argument name to the parsed value; only arguments the caller set are
// present. ImageLayers values expand to image1..imageN.
func BuildArgs(t Tool, vals map[string]any) (map[string]any, error) {
	args := map[string]any{}
	for _, p := range t.Params {
		v, ok := vals[p.API]
		if !ok {
			continue
		}
		switch p.Type {
		case ImageLayers:
			layers, _ := v.([]string)
			if len(layers) > 10 {
				return nil, fmt.Errorf("--image accepts at most 10 layers, got %d", len(layers))
			}
			for i, u := range layers {
				args[fmt.Sprintf("image%d", i+1)] = u
			}
		case RawJSON:
			s, _ := v.(string)
			var parsed any
			if err := json.Unmarshal([]byte(s), &parsed); err != nil {
				return nil, fmt.Errorf("--%s must be valid JSON: %v", p.Flag(), err)
			}
			args[p.API] = parsed
		default:
			if err := checkEnum(p, v); err != nil {
				return nil, err
			}
			args[p.API] = v
		}
	}
	return args, nil
}

// MissingRequired lists required flags absent from vals.
func MissingRequired(t Tool, vals map[string]any) []string {
	var missing []string
	for _, p := range t.Params {
		if !p.Required {
			continue
		}
		if _, ok := vals[p.API]; !ok {
			missing = append(missing, p.Flag())
		}
	}
	return missing
}

func checkEnum(p Param, v any) error {
	if len(p.Enum) == 0 {
		return nil
	}
	s := fmt.Sprint(v)
	for _, e := range p.Enum {
		if e == s {
			return nil
		}
	}
	return fmt.Errorf("--%s must be one of: %s", p.Flag(), strings.Join(p.Enum, ", "))
}
