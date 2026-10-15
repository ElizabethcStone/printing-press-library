// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored (costco-sameday-cart-show): `cart show` (alias `cart list`)
// lists cart items with quantity, unit price and line total. UserCart has
// names and quantities but no prices, so item prices come from the same
// Items hydration the live search uses. Read-only.

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/client"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		if cart := findChild(root, "cart"); cart != nil {
			addNovelCommandIfAbsent(cart, newCartShowCmd(flags))
		}
	})
}

type cartLine struct {
	Name         string  `json:"name"`
	ItemID       string  `json:"item_id"`
	Quantity     float64 `json:"quantity"`
	QuantityType string  `json:"quantity_type,omitempty"`
	UnitPrice    string  `json:"unit_price,omitempty"`
	LineTotal    string  `json:"line_total,omitempty"`
	PriceLabel   string  `json:"price_label,omitempty"`
	Estimate     bool    `json:"estimate,omitempty"`
	Available    *bool   `json:"available,omitempty"`
}

type cartView struct {
	CartID            string     `json:"cart_id"`
	ItemCount         int        `json:"item_count"`
	Items             []cartLine `json:"items"`
	EstimatedSubtotal string     `json:"estimated_subtotal"`
	Note              string     `json:"note,omitempty"`
}

func newCartShowCmd(flags *rootFlags) *cobra.Command {
	var cartID string
	cmd := &cobra.Command{
		Use:     "show",
		Aliases: []string{"list"},
		Short:   "List cart items with quantity, unit price and line total (read-only)",
		Long: "Reads UserCart for the active cart (from the session context, or --cart-id) and\n" +
			"prices each item with the Items query. The subtotal is an estimate from the\n" +
			"shelf prices; 'order preview' shows the checkout totals with discounts and tax.\n" +
			"Weighted items (priced per lb) are marked estimate.",
		Example: `  costco-sameday-pp-cli cart show
  costco-sameday-pp-cli cart show --json`,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			if c.IsDryRun() {
				return printNarrativeJSON(cmd, flags, map[string]any{"dry_run": true, "would": "GET /graphql UserCart, then Items (read-only)"})
			}
			view, err := loadCartView(cmd.Context(), c, cartID)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				printCartTable(cmd, view)
				return nil
			}
			return flags.printJSON(cmd, view)
		},
	}
	cmd.Flags().StringVar(&cartID, "cart-id", "", "Cart id (default: the active cart from the session context)")
	return cmd
}

func loadCartView(ctx context.Context, c *client.Client, cartID string) (cartView, error) {
	sc, err := c.ResolveSessionContext(ctx, client.SessionNeeds{Shop: true, Zone: true, Location: true, Cart: cartID == ""})
	if err != nil {
		return cartView{}, err
	}
	if cartID == "" {
		cartID = sc.CartID
	}
	if cartID == "" {
		return cartView{}, fmt.Errorf("no active cart found; pass --cart-id")
	}
	data, err := c.Get(ctx, "/graphql", map[string]string{"operationName": "UserCart", "variables": mustJSON(map[string]any{"id": cartID})})
	if err != nil {
		return cartView{}, err
	}
	if f := detectPartialFailure(data); f != nil {
		return cartView{}, fmt.Errorf("UserCart failed: %s", f.Message)
	}
	var payload struct {
		Data struct {
			UserCart struct {
				ItemCount          int `json:"itemCount"`
				CartItemCollection struct {
					CartItems []struct {
						Quantity      float64 `json:"quantity"`
						QuantityType  string  `json:"quantityType"`
						BasketProduct struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"basketProduct"`
					} `json:"cartItems"`
				} `json:"cartItemCollection"`
			} `json:"userCart"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return cartView{}, fmt.Errorf("UserCart: decoding response: %w", err)
	}
	view := cartView{CartID: cartID, ItemCount: payload.Data.UserCart.ItemCount, Items: []cartLine{}}
	var ids []string
	for _, it := range payload.Data.UserCart.CartItemCollection.CartItems {
		view.Items = append(view.Items, cartLine{Name: it.BasketProduct.Name, ItemID: it.BasketProduct.ID, Quantity: it.Quantity, QuantityType: it.QuantityType})
		if it.BasketProduct.ID != "" {
			ids = append(ids, it.BasketProduct.ID)
		}
	}
	priced := map[string]map[string]any{}
	for start := 0; start < len(ids); start += searchItemsBatch {
		end := min(start+searchItemsBatch, len(ids))
		items, err := fetchSearchItems(ctx, c, ids[start:end], sc)
		if err != nil {
			view.Note = "prices unavailable: " + err.Error()
			break
		}
		for _, it := range items {
			if id, _ := it["id"].(string); id != "" {
				priced[id] = compactSearchItem(it)
			}
		}
	}
	var subtotal int64
	missing := 0
	for i := range view.Items {
		line := &view.Items[i]
		p := priced[line.ItemID]
		if p == nil {
			missing++
			continue
		}
		if line.Name == "" {
			line.Name, _ = p["name"].(string)
		}
		if v, ok := p["available"].(bool); ok {
			line.Available = &v
		}
		label, _ := p["priceString"].(string)
		line.PriceLabel = label
		cents, multiBuy, ok := parsePriceCents(label)
		if !ok {
			missing++
			continue
		}
		total := int64(math.Round(float64(cents) * line.Quantity))
		line.UnitPrice, line.LineTotal = formatCents(cents), formatCents(total)
		line.Estimate = multiBuy || strings.Contains(strings.ToLower(label), "/lb") || strings.EqualFold(line.QuantityType, "weight")
		subtotal += total
	}
	view.EstimatedSubtotal = formatCents(subtotal)
	if missing > 0 && view.Note == "" {
		view.Note = fmt.Sprintf("%d item(s) had no price; the subtotal leaves them out", missing)
	}
	return view, nil
}

var (
	priceRe    = regexp.MustCompile(`\$\s*([0-9][0-9,]*)(?:\.([0-9]{2}))?`)
	multiBuyRe = regexp.MustCompile(`(?i)\b(\d+)\s*(?:for|/)\s*\$`)
)

// parsePriceCents reads the first dollar amount in a price label ("$12.99",
// "$4.99/lb", "$1,299.99"). A multi-buy label ("2 for $5") gives the
// per-unit price and multiBuy=true, since the deal price needs that count.
func parsePriceCents(label string) (cents int64, multiBuy bool, ok bool) {
	m := priceRe.FindStringSubmatch(label)
	if m == nil {
		return 0, false, false
	}
	dollars, err := strconv.ParseInt(strings.ReplaceAll(m[1], ",", ""), 10, 64)
	if err != nil {
		return 0, false, false
	}
	frac := int64(0)
	if m[2] != "" {
		frac, _ = strconv.ParseInt(m[2], 10, 64)
	}
	cents = dollars*100 + frac
	if mb := multiBuyRe.FindStringSubmatch(label); mb != nil {
		if n, err := strconv.ParseInt(mb[1], 10, 64); err == nil && n > 1 {
			return int64(math.Round(float64(cents) / float64(n))), true, true
		}
	}
	return cents, false, true
}

func formatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return fmt.Sprintf("%s$%d.%02d", sign, c/100, c%100)
}

func printCartTable(cmd *cobra.Command, v cartView) {
	w := cmd.OutOrStdout()
	if len(v.Items) == 0 {
		fmt.Fprintln(w, "Cart is empty.")
		return
	}
	fmt.Fprintf(w, "%-44s %6s %10s %10s\n", "ITEM", "QTY", "UNIT", "TOTAL")
	for _, l := range v.Items {
		name := l.Name
		if len(name) > 44 {
			name = name[:41] + "..."
		}
		unit, total := l.UnitPrice, l.LineTotal
		if unit == "" {
			unit, total = "?", "?"
		}
		if l.Estimate {
			total += "*"
		}
		fmt.Fprintf(w, "%-44s %6s %10s %10s\n", name, strconv.FormatFloat(l.Quantity, 'f', -1, 64), unit, total)
	}
	fmt.Fprintf(w, "%-44s %6s %10s %10s\n", "Estimated subtotal", "", "", v.EstimatedSubtotal)
	if v.Note != "" {
		fmt.Fprintln(w, "Note:", v.Note)
	}
	for _, l := range v.Items {
		if l.Estimate {
			fmt.Fprintln(w, "* estimate: priced by weight, or a multi-buy deal split per unit")
			break
		}
	}
}
