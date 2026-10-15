// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored: `slots windows` (read-only delivery window list) and
// `slots pick` (choose a window; UpdateCheckout mutation, gated by --yes).
// Preserved across regenerate via registerNovelCommand and
// .printing-press-patches/costco-sameday-slots-window-picker.json.
//
// Shapes (sameday.costco.com public rspack chunk 93526, module
// CheckoutCheckoutStepTypesPricedServiceOptionsStep, 2026-10-10):
//   CheckoutPricedServiceOptionsForDateRange is operation:"query", run by the
//   checkout calendar with variables {checkoutSessionId, startDate:"yyyy-MM-dd"}.
//   Response: data.checkoutPricedServiceOptionsForDateRange.pricedServiceOptions
//     __typename CheckoutPricedServiceOptionsPresentableFormat |
//                CheckoutCheckoutErrorsInlineServiceOptionsError |
//                CheckoutCheckoutErrorsServiceOptionError
//     .presentableFormat.layeredOptions[].layeredOption
//       SingleSelectionOption: serviceOption {serviceOptionToken, viewSection}
//       ScheduleSelectionOption: dateGroups[].serviceOptionLines[] {...same}
//     line viewSection: postCheckoutDateString, postCheckoutTimeString,
//       postCheckoutWindowString, priceFormattedAttributesString.sections[].text
//   Picking a window is the UpdateCheckout mutation the step's updateCheckout
//   sends: {checkoutSessionId, checkoutUpdates:{serviceOptionUpdate:
//   {serviceOptionToken[, selectionMode]}}}.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/client"
	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/cliutil"
	"github.com/spf13/cobra"
)

const (
	windowsOperation       = "CheckoutPricedServiceOptionsForDateRange"
	updateCheckoutOp       = "UpdateCheckout"
	checkoutSessionEnvName = "COSTCO_SAMEDAY_CHECKOUT_SESSION_ID"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		for _, child := range root.Commands() {
			if child.Name() == "slots" {
				addNovelCommandIfAbsent(child, newSlotsWindowsCmd(flags))
				addNovelCommandIfAbsent(child, newSlotsPickCmd(flags))
			}
		}
	})
}

// deliveryWindow is one selectable window. Token is never printed unless
// --show-tokens is passed.
type deliveryWindow struct {
	Index    int    `json:"index"`
	Option   string `json:"option,omitempty"`
	Date     string `json:"date,omitempty"`
	Time     string `json:"time,omitempty"`
	Window   string `json:"window,omitempty"`
	Price    string `json:"price,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Token    string `json:"service_option_token,omitempty"`
}

func checkoutSessionFrom(flag string) (string, string) {
	if v := strings.TrimSpace(flag); v != "" {
		return v, "flag"
	}
	if v := strings.TrimSpace(os.Getenv(checkoutSessionEnvName)); v != "" {
		return v, "env"
	}
	return "", ""
}

func windowsVars(sessionID, date string) map[string]any {
	return map[string]any{"checkoutSessionId": sessionID, "startDate": date}
}

func validWindowDate(d string) (string, error) {
	if d == "" {
		return time.Now().Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", d); err != nil {
		return "", usageErr(fmt.Errorf("--date must be YYYY-MM-DD (got %q)", d))
	}
	return d, nil
}

func newSlotsWindowsCmd(flags *rootFlags) *cobra.Command {
	var sessionID, date string
	var showTokens bool
	cmd := &cobra.Command{
		Use:   "windows",
		Short: "List delivery windows for an open checkout (read-only)",
		Long: strings.TrimSpace(`
Lists the delivery windows the checkout page offers, with the price or
savings label for each, using the read-only
CheckoutPricedServiceOptionsForDateRange query. Nothing is selected or
changed.

It needs an open checkout session (--checkout-session-id or
$` + checkoutSessionEnvName + `). --date picks the first day to list
(default today). Tokens are hidden unless --show-tokens; choose a window
with 'slots pick --index N --yes'.`),
		Example: `  costco-sameday-pp-cli slots windows --checkout-session-id <id>
  costco-sameday-pp-cli slots windows --checkout-session-id <id> --date 2026-10-11 --json`,
		Annotations: map[string]string{"mcp:read-only": "true", "pp:narrative": "slots.windows"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sid, _ := checkoutSessionFrom(sessionID)
			if sid == "" {
				return usageErr(fmt.Errorf("missing --checkout-session-id (or $%s): windows exist only for an open checkout", checkoutSessionEnvName))
			}
			d, err := validWindowDate(date)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			if c.IsDryRun() || cliutil.IsVerifyEnv() {
				return flags.printJSON(cmd, map[string]any{"dry_run": true, "would": "GET /graphql " + windowsOperation})
			}
			wins, heading, err := fetchWindows(cmd, c, sid, d)
			if err != nil {
				return err
			}
			if !showTokens {
				for i := range wins {
					wins[i].Token = ""
				}
			}
			out := map[string]any{"start_date": d, "windows": wins}
			if heading != "" {
				out["notice"] = heading
			}
			if len(wins) == 0 {
				out["note"] = "no delivery windows returned for this checkout and date"
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				w := cmd.OutOrStdout()
				if heading != "" {
					fmt.Fprintln(w, heading)
				}
				for _, win := range wins {
					label := win.Window
					if label == "" {
						label = strings.TrimSpace(win.Date + " " + win.Time)
					}
					line := fmt.Sprintf("%2d  %-18s %s", win.Index, win.Option, label)
					if win.Price != "" {
						line += "  (" + win.Price + ")"
					}
					if win.Disabled {
						line += "  [unavailable]"
					}
					if showTokens && win.Token != "" {
						line += "\n    token: " + win.Token
					}
					fmt.Fprintln(w, line)
				}
				if len(wins) == 0 {
					fmt.Fprintln(w, "no delivery windows returned for this checkout and date")
				}
				return nil
			}
			return flags.printJSON(cmd, out)
		},
	}
	cmd.Flags().StringVar(&sessionID, "checkout-session-id", "", "Open checkout session id (default $"+checkoutSessionEnvName+")")
	cmd.Flags().StringVar(&date, "date", "", "First day to list, YYYY-MM-DD (default today)")
	cmd.Flags().BoolVar(&showTokens, "show-tokens", false, "Include each window's serviceOptionToken (table and --json output)")
	return cmd
}

func fetchWindows(cmd *cobra.Command, c *client.Client, sid, date string) ([]deliveryWindow, string, error) {
	payload, err := c.GraphQLQuery(cmd.Context(), windowsOperation, windowsVars(sid, date))
	if err != nil {
		return nil, "", err
	}
	return parseDeliveryWindows(payload)
}

// parseDeliveryWindows flattens the priced service options into windows in
// page order. A service-option error typename becomes an error with its toast
// text.
func parseDeliveryWindows(payload any) ([]deliveryWindow, string, error) {
	pso, _ := digAny(payload, "data", "checkoutPricedServiceOptionsForDateRange", "pricedServiceOptions").(map[string]any)
	if pso == nil {
		return nil, "", fmt.Errorf("%s returned no priced service options (is the checkout session still open?)", windowsOperation)
	}
	if tn, _ := pso["__typename"].(string); strings.Contains(tn, "Error") {
		msg := firstNonEmpty(strAt(pso, "viewSection", "toastDescriptionString"), strAt(pso, "viewSection", "titleString"), tn)
		return nil, "", fmt.Errorf("checkout returned a service option error: %s", msg)
	}
	var out []deliveryWindow
	add := func(option string, disabled bool, line map[string]any) {
		vs, _ := line["viewSection"].(map[string]any)
		w := deliveryWindow{
			Index:    len(out) + 1,
			Option:   option,
			Date:     strAt(vs, "postCheckoutDateString"),
			Time:     strAt(vs, "postCheckoutTimeString"),
			Window:   strAt(vs, "postCheckoutWindowString"),
			Price:    formattedText(vs["priceFormattedAttributesString"]),
			Disabled: disabled,
			Token:    strAt(line, "serviceOptionToken"),
		}
		if w.Time == "" {
			w.Time = formattedText(vs["windowFormattedAttributesString"])
		}
		if w.Window == "" && (w.Date != "" || w.Time != "") {
			w.Window = strings.TrimSpace(strings.Trim(w.Date+", "+w.Time, ", "))
		}
		out = append(out, w)
	}
	layered, _ := digAny(pso, "presentableFormat", "layeredOptions").([]any)
	for _, lo := range layered {
		opt, _ := digAny(lo, "layeredOption").(map[string]any)
		if opt == nil {
			continue
		}
		vs, _ := opt["viewSection"].(map[string]any)
		option := firstNonEmpty(formattedText(vs["lineOneFormattedAttributesString"]), formattedText(vs["headingFormattedAttributesString"]))
		disabled, _ := opt["disabled"].(bool)
		if so, ok := opt["serviceOption"].(map[string]any); ok {
			add(option, disabled, so)
		}
		groups, _ := opt["dateGroups"].([]any)
		for _, g := range groups {
			lines, _ := digAny(g, "serviceOptionLines").([]any)
			for _, l := range lines {
				if lm, ok := l.(map[string]any); ok {
					add(option, disabled, lm)
				}
			}
		}
	}
	return out, "", nil
}

func newSlotsPickCmd(flags *rootFlags) *cobra.Command {
	var sessionID, date, token string
	var index int
	cmd := &cobra.Command{
		Use:   "pick",
		Short: "Choose a delivery window for an open checkout (UpdateCheckout; needs --yes)",
		Long: strings.TrimSpace(`
Selects a delivery window on the open checkout with the UpdateCheckout
mutation (checkoutUpdates.serviceOptionUpdate), the same call the checkout
page makes when you tap a window. This changes the checkout session, so it
refuses without --yes; --dry-run shows what would be sent. It never places
the order.

Give --index N from 'slots windows' (the windows are listed again first so the
token is fresh) or --token from 'slots windows --show-tokens'.`),
		Example: `  costco-sameday-pp-cli slots pick --checkout-session-id <id> --index 2 --dry-run
  costco-sameday-pp-cli slots pick --checkout-session-id <id> --index 2 --yes`,
		Annotations: map[string]string{"pp:narrative": "slots.pick"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sid, _ := checkoutSessionFrom(sessionID)
			if sid == "" {
				return usageErr(fmt.Errorf("missing --checkout-session-id (or $%s)", checkoutSessionEnvName))
			}
			if (index > 0) == (strings.TrimSpace(token) != "") {
				return usageErr(fmt.Errorf("give exactly one of --index or --token"))
			}
			d, err := validWindowDate(date)
			if err != nil {
				return err
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			chosen := deliveryWindow{Token: strings.TrimSpace(token)}
			dry := dryRunOK(flags) || c.IsDryRun()
			if index > 0 && !cliutil.IsVerifyEnv() {
				// The window list is a read-only query, so it runs even on
				// --dry-run (only the UpdateCheckout send is skipped).
				c.DryRun = false
				wins, _, err := fetchWindows(cmd, c, sid, d)
				c.DryRun = dry
				if err != nil {
					return err
				}
				if index > len(wins) {
					return usageErr(fmt.Errorf("--index %d is out of range (%d windows)", index, len(wins)))
				}
				chosen = wins[index-1]
				if chosen.Disabled {
					return fmt.Errorf("window %d (%s) is unavailable", index, chosen.Window)
				}
				if chosen.Token == "" {
					return fmt.Errorf("window %d has no selection token", index)
				}
			}
			preview := chosen
			preview.Token = ""
			body := map[string]any{
				"operationName": updateCheckoutOp,
				"variables": map[string]any{
					"checkoutSessionId": sid,
					"checkoutUpdates": map[string]any{
						"serviceOptionUpdate": map[string]any{"serviceOptionToken": chosen.Token},
					},
				},
				"extensions": map[string]any{"persistedQuery": map[string]any{"version": 1}},
			}
			if dry {
				return printNarrativeJSON(cmd, flags, map[string]any{
					"dry_run":   true,
					"operation": updateCheckoutOp,
					"action":    "serviceOptionUpdate",
					"window":    preview,
					"would":     "POST /graphql UpdateCheckout serviceOptionUpdate (NOT sent; never places the order)",
				})
			}
			if !flags.yes {
				return fmt.Errorf("refusing to change the checkout's delivery window without --yes (use --dry-run to preview)")
			}
			ctx := client.WithDeclaredGraphQLOperation(cmd.Context(), updateCheckoutOp)
			data, status, err := c.PostWithParams(ctx, "/graphql", map[string]string{"operationName": updateCheckoutOp}, body)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			out, failure := pickResult(data, status, preview)
			booked := ""
			if failure == "" {
				// Remembered for order preview / order place (checkout_summary.go).
				// --token picks carry no label: the earlier entry is cleared
				// rather than left looking like the booked window.
				booked = bookedWindowLabel(preview)
				if booked != "" {
					out["booked_window"] = booked
				}
				saveBookedWindow(sid, booked)
			}
			if perr := printNarrativeJSON(cmd, flags, out); perr != nil {
				return perr
			}
			if booked != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "Booked window: %s\n", booked)
			}
			if failure != "" {
				return apiErr(fmt.Errorf("%s did not change the delivery window: %s", updateCheckoutOp, failure))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "checkout-session-id", "", "Open checkout session id (default $"+checkoutSessionEnvName+")")
	cmd.Flags().IntVar(&index, "index", 0, "Window number from 'slots windows'")
	cmd.Flags().StringVar(&token, "token", "", "serviceOptionToken from 'slots windows --show-tokens'")
	cmd.Flags().StringVar(&date, "date", "", "First day used when listing windows for --index (default today)")
	return cmd
}

// pickResult classifies an UpdateCheckout response. The window counts as
// changed only when the body parses, carries no top-level GraphQL errors,
// and data.updateCheckout is present and not an *Error typename. Otherwise
// updated is false and failure says why (the caller exits non-zero).
func pickResult(data []byte, status int, preview deliveryWindow) (map[string]any, string) {
	out := map[string]any{"operation": updateCheckoutOp, "action": "serviceOptionUpdate", "http_status": status, "window": preview, "updated": false}
	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil || resp == nil {
		return out, "response was not a JSON object"
	}
	if errs, ok := resp["errors"].([]any); ok && len(errs) > 0 {
		out["errors"] = errs
		var msgs []string
		for _, e := range errs {
			if m := strAt(e, "message"); m != "" {
				msgs = append(msgs, m)
			}
		}
		if len(msgs) == 0 {
			msgs = []string{fmt.Sprintf("%d GraphQL error(s)", len(errs))}
		}
		return out, strings.Join(msgs, "; ")
	}
	uc, ok := digAny(resp, "data", "updateCheckout").(map[string]any)
	if !ok || uc == nil {
		return out, "response had no data.updateCheckout"
	}
	// The checkout page treats __typename CheckoutCheckoutErrorResponse
	// (with errors[]) as a rejected update; match any *Error typename.
	if tn, _ := uc["__typename"].(string); strings.Contains(tn, "Error") {
		out["error_type"] = tn
		msg := firstNonEmpty(strAt(uc, "viewSection", "toastDescriptionString"), strAt(uc, "viewSection", "titleString"), strAt(uc, "message"))
		if inner, ok := uc["errors"].([]any); ok && len(inner) > 0 {
			out["errors"] = inner
			first := inner[0]
			msg = firstNonEmpty(msg, strAt(first, "message"), strAt(first, "viewSection", "toastDescriptionString"), strAt(first, "viewSection", "titleString"), strAt(first, "__typename"))
		}
		return out, firstNonEmpty(msg, tn)
	}
	out["updated"] = true
	return out, ""
}

func digAny(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func strAt(v any, keys ...string) string {
	s, _ := digAny(v, keys...).(string)
	return strings.TrimSpace(s)
}

// formattedText joins the text of a ViewFormattedAttributesString.
func formattedText(v any) string {
	secs, _ := digAny(v, "sections").([]any)
	var parts []string
	for _, s := range secs {
		if t := strAt(s, "text"); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
