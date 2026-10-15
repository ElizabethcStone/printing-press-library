// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored (costco-sameday-checkout-summary): a short totals summary
// printed first by `order preview` and `order place` (dry-run included).
// The numbers come from CheckoutDraftOrderInvoiceV2's totalLinesTrackingEvent
// (cents as strings); the item count from the checkout totals rows; the
// booked window from the last successful `slots pick` for the session.

package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/cliutil"
	"github.com/spf13/cobra"
)

type checkoutSummary struct {
	Items     *int   `json:"items,omitempty"`
	Subtotal  string `json:"subtotal,omitempty"`
	Discounts string `json:"discounts,omitempty"`
	Tax       string `json:"tax,omitempty"`
	Tip       string `json:"tip,omitempty"`
	Total     string `json:"total,omitempty"`
	CardHold  string `json:"card_hold,omitempty"`
	Card      string `json:"card,omitempty"`
	Window    string `json:"window,omitempty"`
	Store     string `json:"store,omitempty"`
	// NotInResponse names summary fields the responses did not carry.
	NotInResponse []string `json:"not_in_response,omitempty"`
	Unavailable   string   `json:"unavailable,omitempty"`
}

// buildCheckoutSummary reads the invoice (and optional CheckoutCmd) bodies.
func buildCheckoutSummary(invoice json.RawMessage, sessionID string, extra ...json.RawMessage) checkoutSummary {
	var s checkoutSummary
	var inv any
	_ = json.Unmarshal(invoice, &inv)
	if tl, ok := findKey(inv, "total_lines").(map[string]any); ok {
		cents := func(k string) (int64, bool) {
			v, ok := tl[k].(string)
			if !ok {
				if f, ok := tl[k].(float64); ok {
					return int64(f), true
				}
				return 0, false
			}
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
		set := func(dst *string, k string) {
			if n, ok := cents(k); ok {
				*dst = formatCents(n)
			}
		}
		set(&s.Subtotal, "subtotal")
		set(&s.Tax, "sales_tax_total")
		set(&s.Tip, "delivery_tip")
		set(&s.Total, "total")
		set(&s.CardHold, "auth_amount")
		var disc int64
		seen := false
		for _, k := range []string{"items_discount", "basket_discount", "credit_discount_applied"} {
			if n, ok := cents(k); ok {
				disc += n
				seen = true
			}
		}
		if seen {
			s.Discounts = formatCents(disc)
		}
	}
	if n, ok := itemCountFromTotals(inv); ok {
		s.Items = &n
	}
	bodies := []any{inv}
	for _, raw := range extra {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			bodies = append(bodies, v)
		}
	}
	for _, b := range bodies {
		if s.Card == "" {
			s.Card = findCardLabel(b, false)
		}
		if s.Store == "" {
			if r, ok := findKey(b, "retailer").(map[string]any); ok {
				s.Store, _ = r["name"].(string)
			}
		}
	}
	if s.Window == "" {
		s.Window = loadBookedWindow(sessionID)
	}
	for _, f := range []struct {
		name string
		set  bool
	}{{"items", s.Items != nil}, {"subtotal", s.Subtotal != ""}, {"discounts", s.Discounts != ""}, {"tax", s.Tax != ""}, {"tip", s.Tip != ""}, {"total", s.Total != ""}, {"card_hold", s.CardHold != ""}, {"card", s.Card != ""}, {"window", s.Window != ""}, {"store", s.Store != ""}} {
		if !f.set {
			s.NotInResponse = append(s.NotInResponse, f.name)
		}
	}
	return s
}

func findKey(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		if x, ok := t[key]; ok {
			return x
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if x := findKey(t[k], key); x != nil {
				return x
			}
		}
	case []any:
		for _, x := range t {
			if y := findKey(x, key); y != nil {
				return y
			}
		}
	}
	return nil
}

var itemsLabelRe = regexp.MustCompile(`^\s*(\d+)\s+items?\b`)

func itemCountFromTotals(v any) (int, bool) {
	rows, ok := findKey(v, "checkoutTotals").([]any)
	if !ok {
		return 0, false
	}
	for _, r := range rows {
		label, _ := json.Marshal(digAny(r, "labelFormattedAttributesString", "sections"))
		var secs []map[string]any
		_ = json.Unmarshal(label, &secs)
		for _, sec := range secs {
			if t, _ := sec["text"].(string); t != "" {
				if m := itemsLabelRe.FindStringSubmatch(t); m != nil {
					n, _ := strconv.Atoi(m[1])
					return n, true
				}
			}
		}
	}
	return 0, false
}

var (
	cardTextRe   = regexp.MustCompile(`(?i)\b(visa|mastercard|master card|amex|american express|discover|citi)\b\D{0,16}(\d{4})\b`)
	cardBrandKey = regexp.MustCompile(`(?i)^(card_?brand|brand|card_?type|network|card_?network)$`)
	cardLast4Key = regexp.MustCompile(`(?i)^(last_?four|last_?4|last_?four_?digits|card_?last_?four|card_?last_?4)$`)
)

// findCardLabel returns "Brand ****1234" from payment-related parts of a
// response. inPayment limits matching to subtrees under a payment key so a
// product brand name is never mistaken for a card.
func findCardLabel(v any, inPayment bool) string {
	switch t := v.(type) {
	case map[string]any:
		if inPayment {
			var brand, last4 string
			for k, x := range t {
				if s, ok := x.(string); ok && s != "" {
					if cardBrandKey.MatchString(k) {
						brand = s
					}
					if cardLast4Key.MatchString(k) && len(s) == 4 {
						last4 = s
					}
				}
			}
			if last4 != "" {
				return strings.TrimSpace(brand + " ****" + last4)
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lk := strings.ToLower(k)
			if got := findCardLabel(t[k], inPayment || strings.Contains(lk, "payment") || strings.Contains(lk, "instrument")); got != "" {
				return got
			}
		}
	case []any:
		for _, x := range t {
			if got := findCardLabel(x, inPayment); got != "" {
				return got
			}
		}
	case string:
		if inPayment {
			if m := cardTextRe.FindStringSubmatch(t); m != nil {
				return m[1] + " ****" + m[2]
			}
		}
	}
	return ""
}

// fetchCheckoutSummary does the read-only invoice GET for a summary. It uses
// a client with dry-run off: the invoice is a read, and `order place
// --dry-run` should still show what would be charged.
func fetchCheckoutSummary(ctx context.Context, flags *rootFlags, sessionID string) checkoutSummary {
	if cliutil.IsVerifyEnv() {
		return checkoutSummary{Unavailable: "verify mode"}
	}
	readFlags := *flags
	readFlags.dryRun = false
	c, err := readFlags.newClient()
	if err != nil {
		return checkoutSummary{Unavailable: err.Error()}
	}
	data, err := c.Get(ctx, "/graphql", map[string]string{
		"operationName":     "CheckoutDraftOrderInvoiceV2",
		"checkoutSessionId": sessionID,
		"variables":         mustJSON(map[string]any{"checkoutSessionId": sessionID}),
	})
	if err != nil {
		return checkoutSummary{Unavailable: "invoice: " + err.Error(), Window: loadBookedWindow(sessionID)}
	}
	return buildCheckoutSummary(data, sessionID)
}

// printSummaryFirst prints payload with "summary" as its first key. On a
// terminal without --json a few plain lines come first too.
func printSummaryFirst(cmd *cobra.Command, flags *rootFlags, s checkoutSummary, payload map[string]any) error {
	if !(flags.asJSON || flags.agent) && isTerminal(cmd.OutOrStdout()) {
		writeSummaryLines(cmd, s)
	}
	sb, err := json.Marshal(s)
	if err != nil {
		return err
	}
	rest, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	joined := append([]byte(`{"summary":`), sb...)
	if len(rest) > 2 {
		joined = append(joined, ',')
		joined = append(joined, rest[1:]...)
	} else {
		joined = append(joined, '}')
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, joined, "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err = cmd.OutOrStdout().Write(buf.Bytes())
	return err
}

func writeSummaryLines(cmd *cobra.Command, s checkoutSummary) {
	w := cmd.OutOrStdout()
	line := func(label, v string) {
		if v != "" {
			fmt.Fprintf(w, "  %-10s %s\n", label, v)
		}
	}
	fmt.Fprintln(w, "Checkout summary")
	if s.Items != nil {
		line("Items", strconv.Itoa(*s.Items))
	}
	line("Subtotal", s.Subtotal)
	line("Discounts", s.Discounts)
	line("Tax", s.Tax)
	line("Tip", s.Tip)
	line("Total", s.Total)
	line("Card hold", s.CardHold)
	line("Card", s.Card)
	line("Window", s.Window)
	line("Store", s.Store)
	if s.Unavailable != "" {
		line("Note", "totals unavailable: "+s.Unavailable)
	}
	fmt.Fprintln(w)
}

// --- booked window memory (set by `slots pick`) ---
//
// One small file per checkout session (named by a hash of the session id),
// written to a temp file and renamed, so overlapping picks for different
// sessions never overwrite each other and readers never see a partial file.

const bookedWindowTTL = 7 * 24 * time.Hour

type bookedWindowEntry struct {
	Window string    `json:"window"`
	At     time.Time `json:"at"`
}

func bookedWindowsDir() (string, error) {
	dir, err := cliutil.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "booked-windows"), nil
}

// sessionKey hashes the checkout session id so the state holds no ids.
func sessionKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

func bookedWindowPath(sessionID string) (string, error) {
	dir, err := bookedWindowsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionKey(sessionID)+".json"), nil
}

// saveBookedWindow records window for the session; an empty window clears
// any earlier entry (a pick whose label is unknown must not leave the old
// window behind). Entries older than bookedWindowTTL are pruned.
func saveBookedWindow(sessionID, window string) {
	if sessionID == "" {
		return
	}
	p, err := bookedWindowPath(sessionID)
	if err != nil {
		return
	}
	pruneBookedWindows(filepath.Dir(p))
	if window == "" {
		_ = os.Remove(p)
		return
	}
	b, err := json.Marshal(bookedWindowEntry{Window: window, At: time.Now()})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".booked-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(tmp.Name(), 0o600) != nil || os.Rename(tmp.Name(), p) != nil {
		_ = os.Remove(tmp.Name())
	}
}

func pruneBookedWindows(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-bookedWindowTTL)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// loadBookedWindow returns the session's booked window, or "" when none is
// recorded or the entry is older than bookedWindowTTL.
func loadBookedWindow(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	p, err := bookedWindowPath(sessionID)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var e bookedWindowEntry
	if json.Unmarshal(b, &e) != nil || time.Since(e.At) > bookedWindowTTL {
		return ""
	}
	return e.Window
}

// bookedWindowLabel is the human label for a picked window.
func bookedWindowLabel(w deliveryWindow) string {
	label := strings.TrimSpace(strings.Join(nonEmpty(w.Date, w.Time), " "))
	if label == "" {
		label = w.Window
	}
	if label != "" && w.Option != "" && !strings.Contains(label, w.Option) {
		label = w.Option + ", " + label
	}
	return label
}

func nonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}
