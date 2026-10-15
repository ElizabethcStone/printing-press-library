// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored (costco-sameday-which-index): the curated capability index
// for `which`, which SKILL.md advertises. The generated index was empty, so
// `which` always failed. which_index_test.go checks each command exists.

package cli

func init() {
	if len(whichIndex) == 0 {
		whichIndex = costcoWhichIndex
	}
}

var costcoWhichIndex = []whichEntry{
	{Command: "auth login", Group: "auth", Description: "sign in through a browser window or import a session cookie", WhyItMatters: "every other command needs a signed-in Same-Day session"},
	{Command: "auth status", Group: "auth", Description: "check whether the saved session is valid, guest or expired, and when it expires"},
	{Command: "doctor", Group: "auth", Description: "diagnose setup, session health and expiry"},
	{Command: "session context", Group: "session", Description: "show the resolved shop, zone, ZIP and active cart ids"},
	{Command: "search", Group: "products", Description: "search products by name with prices and availability"},
	{Command: "products autosuggestions", Group: "products", Description: "search-box suggestions for a partial product name"},
	{Command: "products searchresultsplacements", Group: "products", Description: "product search results with item details"},
	{Command: "cart show", Group: "cart", Description: "list cart items with quantity, unit price and line total", WhyItMatters: "see what is in the cart and what it costs before checkout"},
	{Command: "cart updatecartitemsmutation", Group: "cart", Description: "add, change or remove cart items"},
	{Command: "slots etas", Group: "slots", Description: "earliest delivery or pickup time and fee for a ZIP", WhyItMatters: "answers whether Same-Day is available at a ZIP"},
	{Command: "slots windows", Group: "slots", Description: "list delivery windows for an open checkout"},
	{Command: "slots pick", Group: "slots", Description: "choose a delivery window for an open checkout"},
	{Command: "address list", Group: "account", Description: "saved delivery addresses (masked)"},
	{Command: "order preview", Group: "order", Description: "preview checkout totals: subtotal, discounts, tax, tip, total and card hold", WhyItMatters: "shows what will be charged before placing the order"},
	{Command: "order tip", Group: "order", Description: "change the tip on an open checkout"},
	{Command: "order place", Group: "order", Description: "place the order (charges the card; needs --yes and --confirm-charge)"},
}
