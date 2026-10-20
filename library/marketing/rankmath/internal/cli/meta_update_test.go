// Copyright 2026 ElizabethcStone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mvanhorn/printing-press-library/library/marketing/rankmath/internal/client"
	"github.com/mvanhorn/printing-press-library/library/marketing/rankmath/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/marketing/rankmath/internal/cliutil/testenv"
	"github.com/spf13/cobra"
)

// Rank Math returns 200 but saves nothing when these fields arrive as JSON strings.
func TestObjectBodyFieldsSentAsJSONObjects(t *testing.T) {
	tests := []struct {
		name  string
		cmd   func(*rootFlags) *cobra.Command
		args  []string
		field string
		array bool // field is an array of objects rather than one object
	}{
		{"meta update", newMetaUpdateCmd, []string{"--object-id", "4", "--object-type", "post", "--meta", `{"rank_math_title":"T"}`}, "meta", false},
		{"schema update", newSchemaUpdateCmd, []string{"--object-id", "4", "--object-type", "post", "--schemas", `{"new-1":{"@type":"Article"}}`}, "schemas", false},
		{"meta update-bulk", newMetaUpdateBulkCmd, []string{"--rows", `{"4":{"title":"T"}}`}, "rows", false},
		{"link-genius export", newLinkGeniusExportCmd, []string{"--type", "links", "--format", "csv", "--total", "1", "--columns", `[{"key":"url","label":"URL"}]`}, "columns", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.Isolate(t)
			t.Setenv(cliutil.VerifyEnvVar, "")
			t.Setenv(cliutil.DogfoodEnvVar, "")
			t.Setenv("RANKMATH_BASE_URL", "https://rankmath.test/wp-json")
			var body map[string]any
			oldHooks := clientHooks
			t.Cleanup(func() { clientHooks = oldHooks })
			registerClientHook(func(c *client.Client) error {
				c.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					raw, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(raw, &body)
					w := httptest.NewRecorder()
					w.WriteHeader(http.StatusOK)
					_, _ = w.WriteString(`{}`)
					return w.Result(), nil
				})}
				return nil
			})
			cmd := tt.cmd(&rootFlags{asJSON: true, timeout: time.Second, noCache: true})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			got := body[tt.field]
			if tt.array {
				list, ok := got.([]any)
				if !ok || len(list) == 0 {
					t.Fatalf("%s sent as %T, want non-empty array: %v", tt.field, got, body)
				}
				got = list[0]
			}
			if _, ok := got.(map[string]any); !ok {
				t.Fatalf("%s sent as %T, want object: %v", tt.field, body[tt.field], body)
			}
		})
	}
}
