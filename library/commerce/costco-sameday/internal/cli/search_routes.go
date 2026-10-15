// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored (costco-sameday-search-routes): the generated
// `products autosuggestions` and `products searchresultsplacements` send a
// GET with the query text, which the server rejects with
// PersistedQueryNotSupported. Both are routed through the persisted-query
// POST the live `search` command uses (session_context.go).

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/client"
	"github.com/mvanhorn/printing-press-library/library/commerce/costco-sameday/internal/cliutil"
	"github.com/spf13/cobra"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		products := findChild(root, "products")
		if products == nil {
			return
		}
		if cmd := findChild(products, "searchresultsplacements"); cmd != nil {
			routeSearchResultsPlacements(cmd, flags)
		}
		if cmd := findChild(products, "autosuggestions"); cmd != nil {
			routeAutosuggestions(cmd, flags)
		}
	})
}

func findChild(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func routeSearchResultsPlacements(cmd *cobra.Command, flags *rootFlags) {
	cmd.Short = "Product search: SearchResultsPlacements as a persisted-query POST, with item details"
	cmd.Long = "Runs the same persisted-query POST as `search` (SearchResultsPlacements, then Items hydration).\n" +
		"Shop, zone and ZIP come from the session context. Only --query and --limit are used;\n" +
		"the other generated variable flags are kept for compatibility and ignored."
	cmd.Annotations["pp:method"] = "POST"
	if cmd.Flags().Lookup("limit") == nil {
		cmd.Flags().Int("limit", 50, "Maximum items to return")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		query, _ := cmd.Flags().GetString("query")
		if strings.TrimSpace(query) == "" {
			return usageErr(fmt.Errorf("--query is required"))
		}
		limit, _ := cmd.Flags().GetInt("limit")
		c, err := flags.newClient()
		if err != nil {
			return err
		}
		results, err := liveProductSearch(cmd.Context(), c, query, limit)
		if err != nil {
			return classifyAPIError(cmd.OutOrStdout(), err, flags)
		}
		return outputSearchResults(cmd, flags, results, limit, DataProvenance{Source: "live"})
	}
}

func routeAutosuggestions(cmd *cobra.Command, flags *rootFlags) {
	cmd.Short = "Search-box suggestions for a partial query (persisted-query POST)"
	cmd.Long = "Runs Autosuggestions as a persisted-query POST (the web app's search box).\n" +
		"--shop-id comes from the session context when omitted; --autosuggestion-session-id is generated when omitted."
	cmd.Annotations["pp:method"] = "POST"
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		query, _ := cmd.Flags().GetString("query")
		if strings.TrimSpace(query) == "" {
			return usageErr(fmt.Errorf("--query is required"))
		}
		shopID, _ := cmd.Flags().GetString("shop-id")
		sessionID, _ := cmd.Flags().GetString("autosuggestion-session-id")
		if sessionID == "" {
			sessionID = client.NewPageViewID()
		}
		c, err := flags.newClient()
		if err != nil {
			return err
		}
		if shopID == "" && !c.IsDryRun() && !cliutil.IsVerifyEnv() {
			sc, err := c.ResolveSessionContext(cmd.Context(), client.SessionNeeds{Shop: true})
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			shopID = sc.ShopID
		}
		body := map[string]any{"operationName": "Autosuggestions", "variables": map[string]any{
			"shopId": shopID, "query": query, "autosuggestionSessionId": sessionID,
		}}
		data, _, err := c.PostQueryWithParams(cmd.Context(), "/graphql", nil, body)
		if err != nil {
			return classifyAPIError(cmd.OutOrStdout(), err, flags)
		}
		if f := detectPartialFailure(data); f != nil {
			return apiErr(fmt.Errorf("Autosuggestions failed: %s", f.Message))
		}
		var payload any
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}
		return flags.printJSON(cmd, payload)
	}
}
