// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package pgmcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LedgerEntry records one credit-spending call this CLI executed. It never
// stores the access key or full responses: only the tool, the identifying
// arguments and the IDs/URLs the server returned.
type LedgerEntry struct {
	At         time.Time         `json:"at"`
	Tool       string            `json:"tool"`
	CreditsMin int               `json:"credits_min"`
	CreditsMax int               `json:"credits_max"`
	PerPin     bool              `json:"per_pin,omitempty"`
	Keys       map[string]string `json:"keys,omitempty"`
	Result     map[string]string `json:"result,omitempty"`
	Command    string            `json:"command,omitempty"`
	// Status is empty for confirmed spends and StatusUnconfirmed when the
	// call failed ambiguously (timeout, transport error, 5xx) and may have
	// been accepted anyway.
	Status string `json:"status,omitempty"`
}

// schedulingTools are the spends that put a pin in the queue.
var schedulingTools = map[string]bool{"generate_and_schedule_pin": true, "generate_and_schedule_ai_pin": true, "schedule_pin": true}

// StatusUnconfirmed marks a spend that may or may not have happened.
const StatusUnconfirmed = "unconfirmed"

// ledgerKeyArgs are the arguments worth remembering for skip/audit decisions.
var ledgerKeyArgs = []string{"url", "postUrl", "mediaURL", "title", "pinterestProfile", "boardId", "automationId", "productId", "template", "designProfileName"}

// ledgerResultKeys are response fields copied into the entry when present.
var ledgerResultKeys = []string{"id", "pinId", "imageUrl", "imageURL", "mediaUrl", "batchId", "scheduledTime", "status"}

// NewLedgerEntry builds an entry for a successful executed call.
func NewLedgerEntry(t Tool, args map[string]any, result json.RawMessage, command string, now time.Time) LedgerEntry {
	e := LedgerEntry{At: now.UTC(), Tool: t.Name, CreditsMin: t.Cost.Min, CreditsMax: t.Cost.Max, PerPin: t.Cost.PerPin, Command: command}
	for _, k := range ledgerKeyArgs {
		if s, ok := args[k].(string); ok && s != "" {
			if e.Keys == nil {
				e.Keys = map[string]string{}
			}
			e.Keys[k] = s
		}
	}
	if obj := Object(result); obj != nil {
		for _, k := range ledgerResultKeys {
			if s := Str(obj, k); s != "" {
				if e.Result == nil {
					e.Result = map[string]string{}
				}
				e.Result[k] = s
			}
		}
	}
	return e
}

// AppendLedger appends e to the JSONL ledger at path, creating it 0600.
func AppendLedger(path string, e LedgerEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	line, err := json.Marshal(e)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadLedger loads every entry. A missing file is an empty ledger. Malformed
// lines are counted and skipped so one bad write cannot hide the rest.
func ReadLedger(path string) ([]LedgerEntry, int, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []LedgerEntry{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	entries := make([]LedgerEntry, 0)
	bad := 0
	// ReadString has no line-length cap, so one oversized entry cannot hide
	// every later spend from the skip logic.
	r := bufio.NewReader(f)
	for {
		raw, rerr := r.ReadString('\n')
		if rerr != nil && rerr != io.EOF {
			return entries, bad, rerr
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			if rerr == io.EOF {
				break
			}
			continue
		}
		var e LedgerEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			bad++
			if rerr == io.EOF {
				break
			}
			continue
		}
		entries = append(entries, e)
		if rerr == io.EOF {
			break
		}
	}
	return entries, bad, nil
}

// LedgeredURLs returns the normalized destination/post URLs already spent on.
// Only scheduling tools count: an image-only generation spent credits but
// pinned nothing, so its post still needs a pin.
func LedgeredURLs(entries []LedgerEntry) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		if !schedulingTools[e.Tool] {
			continue
		}
		for _, k := range []string{"url", "postUrl"} {
			if u := NormalizeURL(e.Keys[k]); u != "" {
				out[u] = true
			}
		}
	}
	return out
}
