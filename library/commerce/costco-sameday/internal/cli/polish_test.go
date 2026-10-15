// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Tests for the docs/polish changes: browser detection, Safari cookie hint,
// imported cookie expiry, live-search routing for the products commands,
// cart show, the checkout totals summary and the booked window. Local stubs
// and fake values only.

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBrowserCandidatesCoverChromiumFamily(t *testing.T) {
	env := map[string]string{"LocalAppData": `C:\Users\fake\AppData\Local`, "ProgramFiles": `C:\Program Files`}
	mac := browserCandidates("darwin", "/Users/fake", func(string) string { return "" })
	win := browserCandidates("windows", "", func(k string) string { return env[k] })
	has := func(list []browserCandidate, name, frag string) bool {
		for _, c := range list {
			if c.Name == name && strings.Contains(c.Path, frag) {
				return true
			}
		}
		return false
	}
	for _, want := range []struct{ name, frag string }{
		{"Brave", "/Applications/Brave Browser.app/"},
		{"Microsoft Edge", "/Applications/Microsoft Edge.app/"},
		{"Arc", "/Applications/Arc.app/Contents/MacOS/Arc"},
		{"Vivaldi", "/Applications/Vivaldi.app/"},
		{"Google Chrome", "/Users/fake/Applications/Google Chrome.app/"},
		{"Brave", "/Users/fake/Applications/Brave Browser.app/"},
	} {
		if !has(mac, want.name, want.frag) {
			t.Errorf("darwin candidates missing %s at %s", want.name, want.frag)
		}
	}
	if !has(win, "Microsoft Edge", `Microsoft\Edge\Application\msedge.exe`) || !has(win, "Brave", `BraveSoftware\Brave-Browser`) {
		t.Errorf("windows candidates incomplete: %+v", win)
	}
	if mac[0].Name != "Google Chrome" {
		t.Errorf("Chrome should stay first, got %s", mac[0].Name)
	}
}

func TestBrowserNameForPath(t *testing.T) {
	cases := map[string]string{
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser":   "Brave",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge": "Microsoft Edge",
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`:         "Microsoft Edge",
		"/Users/fake/Applications/Arc.app/Contents/MacOS/Arc":            "Arc",
		"/usr/bin/vivaldi-stable":                                        "Vivaldi",
		"/usr/bin/chromium-browser":                                      "Chromium",
		"/usr/bin/google-chrome":                                         "Google Chrome",
		"/opt/fake/unknown-bin":                                          "browser",
	}
	for p, want := range cases {
		if got := browserNameForPath(p); got != want {
			t.Errorf("browserNameForPath(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestFindBrowserBinaryFindsBraveOnPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PATH probe test is Linux-only (macOS/Windows probe install dirs first)")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "brave-browser")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv(browserLoginEnvBinary, "")
	got, err := findBrowserBinary("")
	if err != nil || got != bin {
		t.Fatalf("findBrowserBinary = %q, %v", got, err)
	}
	if browserNameForPath(got) != "Brave" {
		t.Fatalf("name = %s", browserNameForPath(got))
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := findBrowserBinary(""); err == nil || !strings.Contains(err.Error(), "Brave, Edge, Vivaldi and Arc") {
		t.Fatalf("not-found error = %v", err)
	}
}

func TestCurlWithoutCookieHintsSafari(t *testing.T) {
	isolatedAuthEnv(t)
	_, err := runAuthWithStdin(t, "curl 'https://sameday.costco.com/graphql' -H 'accept: */*' -H 'x-fake: 1'", "--cookies-file", "-")
	if err == nil {
		t.Fatal("cURL without cookies imported")
	}
	for _, want := range []string{"Safari", "Web Inspector > Storage > Cookies", "echo '__Host-instacart_sid=", "--cookies-file -"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestStripCookieAttributes(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	h, exp := stripCookieAttributes("__Host-instacart_sid=FAKE-SID; Expires=Tue, 01 Jan 2030 00:00:00 GMT; Path=/; Secure; HttpOnly; SameSite=Lax", now)
	if h != "__Host-instacart_sid=FAKE-SID" || !exp.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("got %q %v", h, exp)
	}
	h, exp = stripCookieAttributes("a=1; Max-Age=3600; b=2; Expires=Tue, 01 Jan 2030 00:00:00 GMT", now)
	if h != "a=1; b=2" || !exp.Equal(now.Add(time.Hour)) {
		t.Fatalf("max-age: %q %v", h, exp)
	}
	if h, exp = stripCookieAttributes("a=1; b=2", now); h != "a=1; b=2" || !exp.IsZero() {
		t.Fatalf("plain: %q %v", h, exp)
	}
}

func TestPastedCookieExpiryReachesAuthStatus(t *testing.T) {
	isolatedAuthEnv(t)
	out, err := runAuthWithStdin(t, "__Host-instacart_sid=FAKE-SID-VALUE; Expires=Tue, 01 Jan 2030 00:00:00 GMT\n", "--cookies-file", "-")
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Session cookie expires 20") || strings.Contains(out, "FAKE-SID-VALUE") {
		t.Fatalf("import output:\n%s", out)
	}
	status, err := runCLI(t, []string{"auth", "status"}, authCmds)
	if err != nil || strings.Contains(status, "Expires: unknown") || !strings.Contains(status, "Expires: 20") {
		t.Fatalf("status err=%v\n%s", err, status)
	}
	// Without an expiry the import says how to add one.
	isolatedAuthEnv(t)
	out, err = runAuthWithStdin(t, "__Host-instacart_sid=FAKE-SID-VALUE\n", "--cookies-file", "-")
	if err != nil || !strings.Contains(out, "Session expiry unknown") {
		t.Fatalf("no-expiry import: %v\n%s", err, out)
	}
}

const fakeGridReply = `{"data":{"searchResultsPlacements":{"placements":[{"content":{"__typename":"SearchContentManagementSearchItemGrid","items":[{"id":"items_fake-1","name":"Fake Milk","price":{"viewSection":{"priceString":"$3.49"}}}],"itemIds":["items_fake-1"]}}]}}}`

func TestProductsSearchCommandsUsePersistedPOST(t *testing.T) {
	rec := routedSameDay(t, withReplies(map[string]string{
		"Autosuggestions":         `{"data":{"retailerAutosuggestionsV2":{"suggestions":[{"text":"fake milk"}]}}}`,
		"SearchResultsPlacements": fakeGridReply,
	}))
	out, _, err := runRootSplit(t, "products", "autosuggestions", "--query", "mil", "--json")
	if err != nil || !strings.Contains(out, "fake milk") {
		t.Fatalf("autosuggestions: %v\n%s", err, out)
	}
	if rec.methods["Autosuggestions"] != "POST" || rec.query["Autosuggestions"] != "" {
		t.Fatalf("autosuggestions method=%s query=%q", rec.methods["Autosuggestions"], rec.query["Autosuggestions"])
	}
	if v := rec.vars["Autosuggestions"]; v["shopId"] != "shop-1" || v["query"] != "mil" || v["autosuggestionSessionId"] == "" {
		t.Fatalf("autosuggestions vars = %v", v)
	}
	out, _, err = runRootSplit(t, "products", "searchresultsplacements", "--query", "milk", "--json")
	if err != nil || !strings.Contains(out, "Fake Milk") || !strings.Contains(out, "$3.49") {
		t.Fatalf("searchresultsplacements: %v\n%s", err, out)
	}
	if rec.methods["SearchResultsPlacements"] != "POST" {
		t.Fatalf("searchresultsplacements method = %s", rec.methods["SearchResultsPlacements"])
	}
	if _, _, err := runRootSplit(t, "products", "autosuggestions"); err == nil || ExitCode(err) != 2 {
		t.Fatalf("missing --query err = %v", err)
	}
}

func TestCartShowPricesItems(t *testing.T) {
	rec := routedSameDay(t, withReplies(map[string]string{
		"UserCart": `{"data":{"userCart":{"itemCount":3,"cartItemCollection":{"cartItems":[` +
			`{"quantity":2,"quantityType":"each","basketProduct":{"id":"items_fake-1","name":"Fake Milk"}},` +
			`{"quantity":1.5,"quantityType":"weight","basketProduct":{"id":"items_fake-2","name":"Fake Steak"}},` +
			`{"quantity":1,"quantityType":"each","basketProduct":{"id":"items_fake-3","name":"Fake Unpriced"}}]}}}}`,
		"Items": `{"data":{"items":[` +
			`{"id":"items_fake-1","name":"Fake Milk","price":{"viewSection":{"priceString":"$3.49"}}},` +
			`{"id":"items_fake-2","name":"Fake Steak","price":{"viewSection":{"priceString":"$10.00/lb"}}}]}}`,
	}))
	out, _, err := runRootSplit(t, "cart", "show", "--json")
	if err != nil {
		t.Fatalf("cart show: %v\n%s", err, out)
	}
	if got := rec.vars["UserCart"]["id"]; got != "cart-1" {
		t.Fatalf("UserCart id = %v", got)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	view, _ := env["results"].(map[string]any)
	if view == nil {
		view = env
	}
	items, _ := view["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %v", view["items"])
	}
	milk, _ := items[0].(map[string]any)
	steak, _ := items[1].(map[string]any)
	if milk["unit_price"] != "$3.49" || milk["line_total"] != "$6.98" || milk["quantity"] != 2.0 {
		t.Fatalf("milk = %v", milk)
	}
	if steak["line_total"] != "$15.00" || steak["estimate"] != true {
		t.Fatalf("steak = %v", steak)
	}
	if view["estimated_subtotal"] != "$21.98" || !strings.Contains(view["note"].(string), "1 item(s) had no price") {
		t.Fatalf("subtotal/note = %v / %v", view["estimated_subtotal"], view["note"])
	}
	// `cart list` is an alias.
	if _, _, err := runRootSplit(t, "cart", "list", "--json"); err != nil {
		t.Fatalf("cart list: %v", err)
	}
}

func TestParsePriceCents(t *testing.T) {
	cases := []struct {
		in       string
		cents    int64
		multiBuy bool
	}{
		{"$3.49", 349, false}, {"$10.00/lb", 1000, false}, {"$1,299.99", 129999, false},
		{"2 for $5", 250, true}, {"3 for $10.00", 333, true}, {"$7", 700, false},
	}
	for _, c := range cases {
		got, mb, ok := parsePriceCents(c.in)
		if !ok || got != c.cents || mb != c.multiBuy {
			t.Errorf("parsePriceCents(%q) = %d %v %v, want %d %v", c.in, got, mb, ok, c.cents, c.multiBuy)
		}
	}
	if _, _, ok := parsePriceCents("see price in cart"); ok {
		t.Error("parsed a label with no amount")
	}
}

func TestCartShowMultiBuyIsPerUnit(t *testing.T) {
	routedSameDay(t, withReplies(map[string]string{
		"UserCart": `{"data":{"userCart":{"itemCount":2,"cartItemCollection":{"cartItems":[{"quantity":2,"quantityType":"each","basketProduct":{"id":"items_fake-9","name":"Fake Deal"}}]}}}}`,
		"Items":    `{"data":{"items":[{"id":"items_fake-9","name":"Fake Deal","price":{"viewSection":{"priceString":"2 for $5"}}}]}}`,
	}))
	out, _, err := runRootSplit(t, "cart", "show", "--json")
	if err != nil || !strings.Contains(out, `"line_total": "$5.00"`) || !strings.Contains(out, `"unit_price": "$2.50"`) || !strings.Contains(out, `"estimate": true`) {
		t.Fatalf("multi-buy: %v\n%s", err, out)
	}
}

func TestBookedWindowTokenPickClearsAndExpires(t *testing.T) {
	routedSameDay(t, sessionReplies)
	saveBookedWindow("sess-fake", "Fri 2-4pm (fake)")
	if got := loadBookedWindow("sess-fake"); got != "Fri 2-4pm (fake)" {
		t.Fatalf("load = %q", got)
	}
	saveBookedWindow("sess-fake", "") // a --token pick has no label
	if got := loadBookedWindow("sess-fake"); got != "" {
		t.Fatalf("old window survived an unlabeled pick: %q", got)
	}
	// Entries older than the TTL are ignored even when nothing prunes them.
	p, _ := bookedWindowPath("sess-old-fake")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	old, _ := json.Marshal(bookedWindowEntry{Window: "Mon 9-11am (fake)", At: time.Now().Add(-8 * 24 * time.Hour)})
	if err := os.WriteFile(p, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadBookedWindow("sess-old-fake"); got != "" {
		t.Fatalf("expired entry loaded: %q", got)
	}
	// Separate sessions live in separate files.
	saveBookedWindow("sess-a-fake", "A (fake)")
	saveBookedWindow("sess-b-fake", "B (fake)")
	if loadBookedWindow("sess-a-fake") != "A (fake)" || loadBookedWindow("sess-b-fake") != "B (fake)" {
		t.Fatal("sessions overwrote each other")
	}
}

const fakeInvoice = `{"data":{"checkoutDraftOrderInvoiceV2":{"checkoutTotalsPresenter":{` +
	`"viewSection":{"totalLinesTrackingEvent":{"properties":{"total_lines":{"subtotal":"12345","items_discount":"-500","basket_discount":"-100","credit_discount_applied":"-200","sales_tax_total":"987","delivery_tip":"400","total":"12932","auth_amount":"13500"}}},` +
	`"checkoutTotalsV2":{"checkoutTotals":[{"labelFormattedAttributesString":{"sections":[{"text":"7 items (loyalty applied)"}]}},{"labelFormattedAttributesString":{"sections":[{"text":"Est. tax"}]}}]}},` +
	`"invoicingPaymentMethodsApplied":{"items":[{"viewSection":{"displayFormattedAttributesString":{"sections":[{"text":"Visa ending in 0000"}]}}}]}}}}}`

func TestBuildCheckoutSummary(t *testing.T) {
	s := buildCheckoutSummary([]byte(fakeInvoice), "")
	if s.Items == nil || *s.Items != 7 || s.Subtotal != "$123.45" || s.Discounts != "-$8.00" || s.Tax != "$9.87" ||
		s.Tip != "$4.00" || s.Total != "$129.32" || s.CardHold != "$135.00" || s.Card != "Visa ****0000" {
		t.Fatalf("summary = %+v", s)
	}
	for _, f := range []string{"window", "store"} {
		if !strings.Contains(strings.Join(s.NotInResponse, ","), f) {
			t.Errorf("not_in_response should list %s: %v", f, s.NotInResponse)
		}
	}
	// A product brand outside payment data is not a card.
	if c := findCardLabel(map[string]any{"items": []any{map[string]any{"brand": "Visa", "lastFour": "1111"}}}, false); c != "" {
		t.Fatalf("card from non-payment data: %q", c)
	}
}

func TestOrderPreviewPrintsSummaryFirst(t *testing.T) {
	routedSameDay(t, withReplies(map[string]string{"CheckoutDraftOrderInvoiceV2": fakeInvoice}))
	out, _, err := runRootSplit(t, "order", "preview", "--checkout-session-id", "sess-fake", "--json")
	if err != nil {
		t.Fatalf("preview: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{\n  \"summary\": {") {
		t.Fatalf("summary not first:\n%.300s", out)
	}
	var env map[string]any
	if json.Unmarshal([]byte(out), &env) != nil || env["invoice"] == nil || env["would_charge"] != false {
		t.Fatalf("payload lost:\n%.500s", out)
	}
}

func TestOrderPlaceDryRunShowsTotalsAndNeverFinalizes(t *testing.T) {
	rec := routedSameDay(t, withReplies(map[string]string{"CheckoutDraftOrderInvoiceV2": fakeInvoice}))
	out, _, err := runRootSplit(t, "order", "place", "--checkout-session-id", "sess-fake", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("place --dry-run: %v\n%s", err, out)
	}
	if _, sent := rec.methods["FinalizeCheckout"]; sent {
		t.Fatal("FinalizeCheckout sent on --dry-run")
	}
	if rec.methods["CheckoutDraftOrderInvoiceV2"] != "GET" {
		t.Fatalf("invoice method = %q", rec.methods["CheckoutDraftOrderInvoiceV2"])
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{\n  \"summary\": {") || !strings.Contains(out, `"total": "$129.32"`) || !strings.Contains(out, `"card_hold": "$135.00"`) {
		t.Fatalf("dry-run output:\n%s", out)
	}
}

func TestOrderPlaceRefusalStaysOffline(t *testing.T) {
	rec := routedSameDay(t, withReplies(map[string]string{"CheckoutDraftOrderInvoiceV2": fakeInvoice}))
	if _, _, err := runRootSplit(t, "order", "place", "--checkout-session-id", "sess-fake", "--json"); err == nil {
		t.Fatal("place without consent succeeded")
	}
	if len(rec.methods) != 0 {
		t.Fatalf("refused place sent requests: %v", rec.methods)
	}
}

func TestSlotsPickBookedWindowFlowsToPreview(t *testing.T) {
	routedSameDay(t, withReplies(map[string]string{windowsOperation: fakeWindows, updateCheckoutOp: `{"data":{"updateCheckout":{"__typename":"X"}}}`, "CheckoutDraftOrderInvoiceV2": fakeInvoice}))
	out, errOut, err := runRootSplit(t, "slots", "pick", "--checkout-session-id", "sess-fake", "--index", "3", "--yes", "--json")
	if err != nil {
		t.Fatalf("pick: %v\n%s", err, out)
	}
	var pick map[string]any
	_ = json.Unmarshal([]byte(out), &pick)
	booked, _ := pick["booked_window"].(string)
	if booked == "" || !strings.Contains(errOut, "Booked window: "+booked) {
		t.Fatalf("booked window missing: %v\nstderr: %s", pick, errOut)
	}
	out, _, err = runRootSplit(t, "order", "preview", "--checkout-session-id", "sess-fake", "--json")
	if err != nil || !strings.Contains(out, `"window": "`+booked+`"`) {
		t.Fatalf("preview window: %v\n%.600s", err, out)
	}
	// Another session does not inherit it, and the state file holds no ids.
	out, _, _ = runRootSplit(t, "order", "preview", "--checkout-session-id", "sess-other-fake", "--json")
	if strings.Contains(out, `"window": "`) {
		t.Fatalf("window leaked across sessions:\n%.400s", out)
	}
	dir, _ := bookedWindowsDir()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		raw, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(e.Name()+string(raw), "sess-fake") {
			t.Fatalf("state stores the raw session id: %s %s", e.Name(), raw)
		}
	}
}
