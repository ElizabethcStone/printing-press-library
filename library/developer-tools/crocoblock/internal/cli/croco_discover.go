package cli

import (
	"context"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
	"sort"
	"strings"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, f *rootFlags) {
		cmd := crocoDeclaredCommand(f, &cobra.Command{Use: "discover", Short: "Inspect installed JetPlugins, REST capabilities and permission gaps"}, true, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
			v, e := crocoRequest(ctx, c, "GET", "/", nil, nil, true)
			if e != nil {
				return nil, e
			}
			index, e := crocoMap(v)
			if e != nil {
				return nil, e
			}
			routes, e := crocoMap(index["routes"])
			if e != nil {
				return nil, e
			}
			jetRoutes := []string{}
			for p := range routes {
				if strings.HasPrefix(p, "/jet-") {
					jetRoutes = append(jetRoutes, p)
				}
			}
			sort.Strings(jetRoutes)
			gaps := []any{}
			versions, e := crocoPluginVersions(ctx, c)
			if e != nil {
				gaps = append(gaps, map[string]any{"resource": "plugins", "reason": e.Error()})
				versions = map[string]string{}
			}
			caps := map[string]any{}
			for name, path := range map[string]string{"native_mcp": "/jet-engine/v1/mcp", "forms": "/wp/v2/jet-form-builder", "records": "/jet-form-builder/v1/records/fetch-page", "bookings": "/jet-booking/v2/bookings-list", "appointments": "/jet-engine/v2/appointments-list", "filters": "/jet-smart-filters-api/v1/filters"} {
				_, exists := routes[path]
				caps[name] = map[string]any{"registered": exists, "permission": "not_probed"}
			}
			return map[string]any{"site": c.BaseURL, "versions": versions, "capabilities": caps, "jet_routes": jetRoutes, "gaps": gaps, "note": "Registered routes do not guarantee per-operation permission; commands enforce the actual response."}, nil
		})
		cmd.Example = "  crocoblock-pp-cli discover --agent"
		crocoAttach(root, "", cmd)
	})
}
