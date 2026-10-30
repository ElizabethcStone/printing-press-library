// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
)

const pgStaticToolsFile = "pingenerator_tools_static.go"

// renderPGStaticTools renders one constructor per catalog tool with literal
// Use and flag declarations. The literals let verify-skill and readers see
// each command's surface without running the binary; pgmcp.Tools stays the
// single source of truth.
func renderPGStaticTools() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.\n")
	b.WriteString("// pp:data-source live\n")
	b.WriteString("// Rendered from internal/pgmcp.Tools by TestPGStaticToolsUpToDate; do not edit by hand.\n")
	b.WriteString("// Refresh with: PG_UPDATE_STATIC=1 go test ./internal/cli -run TestPGStaticToolsUpToDate\n\n")
	b.WriteString("package cli\n\nimport \"github.com/spf13/cobra\"\n\n")
	b.WriteString("func init() {\n\tregisterNovelCommand(func(root *cobra.Command, flags *rootFlags) {\n")
	for _, t := range pgmcp.Tools {
		fmt.Fprintf(&b, "\t\taddNovelCommandIfAbsent(root, %s(flags))\n", pgCtorName(t))
	}
	b.WriteString("\t})\n}\n")
	for _, t := range pgmcp.Tools {
		fmt.Fprintf(&b, "\nfunc %s(flags *rootFlags) *cobra.Command {\n", pgCtorName(t))
		fmt.Fprintf(&b, "\tcmd := &cobra.Command{Use: %q}\n", t.Cmd)
		for _, p := range t.Params {
			desc := pgFlagDesc(p)
			v := pgVarName(p.Flag())
			switch p.Type {
			case pgmcp.String, pgmcp.RawJSON:
				fmt.Fprintf(&b, "\tvar %s string\n\tcmd.Flags().StringVar(&%s, %q, \"\", %q)\n", v, v, p.Flag(), desc)
			case pgmcp.Bool:
				fmt.Fprintf(&b, "\tvar %s bool\n\tcmd.Flags().BoolVar(&%s, %q, false, %q)\n", v, v, p.Flag(), desc)
			case pgmcp.Int:
				fmt.Fprintf(&b, "\tvar %s int\n\tcmd.Flags().IntVar(&%s, %q, 0, %q)\n", v, v, p.Flag(), desc)
			case pgmcp.Array, pgmcp.ImageLayers:
				fmt.Fprintf(&b, "\tvar %s []string\n\tcmd.Flags().StringArrayVar(&%s, %q, nil, %q)\n", v, v, p.Flag(), desc)
			}
		}
		if t.Cost.Spends() {
			fmt.Fprintf(&b, "\tvar execute bool\n\tcmd.Flags().BoolVar(&execute, \"execute\", false, %q)\n", "Actually run the call and spend credits ("+t.Cost.Note()+")")
		}
		fmt.Fprintf(&b, "\treturn pgWireToolCmd(flags, %q, cmd)\n}\n", t.Name)
	}
	return format.Source(b.Bytes())
}

// pgVarName turns a kebab flag into the local variable its declaration binds.
func pgVarName(flag string) string {
	parts := strings.Split(flag, "-")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return "flag" + strings.ToUpper(parts[0][:1]) + parts[0][1:] + strings.Join(parts[1:], "")
}

func pgCtorName(t pgmcp.Tool) string {
	var b strings.Builder
	b.WriteString("newPGTool")
	for _, part := range strings.Split(t.Name, "_") {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String() + "Cmd"
}

func TestPGStaticToolsUpToDate(t *testing.T) {
	want, err := renderPGStaticTools()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PG_UPDATE_STATIC") == "1" {
		if err := os.WriteFile(pgStaticToolsFile, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(pgStaticToolsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; run: PG_UPDATE_STATIC=1 go test ./internal/cli -run TestPGStaticToolsUpToDate", pgStaticToolsFile)
	}
	root := RootCmd()
	for _, tl := range pgmcp.Tools {
		if c, _, err := root.Find([]string{tl.Cmd}); err != nil || c.Annotations["pp:mcp-tool"] != tl.Name {
			t.Fatalf("%s is not registered from the static file", tl.Cmd)
		}
	}
}
