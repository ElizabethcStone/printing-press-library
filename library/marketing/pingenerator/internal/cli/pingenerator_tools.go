// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live
// Hand-authored: every Pin Generator MCP tool as a typed command, plus the
// offline tool catalog. The generator cannot emit these because the whole
// surface is JSON-RPC tools/call on one /mcp path.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/client"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/pgmcp"
	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/platform"
)

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newPGToolsCatalogCmd(flags))
		pgGuardPassthrough(root, flags)
	})
}

// pgGuardPassthrough wraps the generated raw `mcp` passthrough so it cannot
// spend credits behind the typed commands' --execute guard and ledger. The
// raw endpoint is also hidden from the MCP server (spec mcp.endpoint_tools),
// so agents only reach it through this command.
func pgGuardPassthrough(root *cobra.Command, flags *rootFlags) {
	cmd, _, err := root.Find([]string{"mcp"})
	if err != nil || cmd.Name() != "mcp" || cmd.RunE == nil {
		return
	}
	var allowUnknown bool
	cmd.Flags().BoolVar(&allowUnknown, "allow-unknown-tool", false, "Send tools/call for a tool this CLI does not know (its credit cost is unknown)")
	cmd.Long += "\n\ntools/call for a credit-spending tool is refused here; use the typed command with --execute so the spend is previewed and recorded in the ledger. Unknown tool names need --allow-unknown-tool."
	inner := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if err := pgPassthroughCheck(c, flags, allowUnknown); err != nil {
			return err
		}
		return inner(c, args)
	}
}

func pgPassthroughCheck(cmd *cobra.Command, flags *rootFlags, allowUnknown bool) error {
	if dryRunOK(flags) {
		return nil
	}
	method, _ := cmd.Flags().GetString("method")
	if method != "tools/call" {
		return nil
	}
	raw, _ := cmd.Flags().GetString("params")
	// Decode as a map: struct decoding matches keys case-insensitively, so
	// {"name":"spend","Name":"free"} would show the guard one tool while the
	// passthrough forwards both keys to the server.
	var params map[string]any
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return usageErr(fmt.Errorf("--params must be a JSON object: %v", err))
	}
	for k := range params {
		if k != "name" && strings.EqualFold(k, "name") {
			return usageErr(fmt.Errorf("--params has an ambiguous %q key; use exactly one \"name\"", k))
		}
	}
	name, _ := params["name"].(string)
	if name == "" {
		return usageErr(fmt.Errorf("tools/call needs --params with a tool name, e.g. '{\"name\":\"list_boards\",\"arguments\":{}}'"))
	}
	t, known := pgmcp.Lookup(name)
	switch {
	case known && t.Cost.Spends():
		return usageErr(fmt.Errorf("%s spends credits (%s); use 'pingenerator-pp-cli %s --execute' so the spend is previewed first and recorded in the ledger", t.Name, t.Cost.Note(), t.Cmd))
	case !known && !allowUnknown:
		return usageErr(fmt.Errorf("unknown tool %q may spend credits; check 'pingenerator-pp-cli tools' or pass --allow-unknown-tool to send it anyway", name))
	}
	return nil
}

// pgLedgerPath is where executed credit spends are recorded.
func pgLedgerPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("PINGENERATOR_LEDGER")); p != "" {
		return p, nil
	}
	dir, err := cliutil.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ledger.jsonl"), nil
}

// pgErr maps MCP-layer and transport failures onto the CLI's typed exits:
// auth 4, rate limit 7, everything else (including no credits) 5.
func pgErr(err error) error {
	if err == nil {
		return nil
	}
	var me *pgmcp.Error
	if errors.As(err, &me) {
		switch me.Kind {
		case pgmcp.KindAuth:
			return authErr(fmt.Errorf("%w\nhint: set PINGENERATOR_ACCESS_KEY to the API access key from pingenerator.com/profile", err))
		case pgmcp.KindCredits:
			return apiErr(fmt.Errorf("no credits remaining: %w\nhint: run 'pingenerator-pp-cli credit-runway' and top up in the Pin Generator app", err))
		case pgmcp.KindUnreadable:
			return apiErr(fmt.Errorf("%w\nhint: the reply could not be read; a paid call may still have gone through", err))
		case pgmcp.KindNotFound:
			return notFoundErr(fmt.Errorf("%w\nhint: check the ID or URL; list-* commands show what exists on this account", err))
		case pgmcp.KindRateLimit:
			return rateLimitErr(fmt.Errorf("%w\nhint: Pin Generator allows about 60 requests per minute per key", err))
		default:
			return apiErr(err)
		}
	}
	if strings.Contains(err.Error(), "HTTP 402") {
		return apiErr(fmt.Errorf("no credits remaining: %w", err))
	}
	return classifyAPIErrorOnly(err)
}

// pgCall runs one tool through the generated client.
func pgCall(cmd *cobra.Command, flags *rootFlags, name string, args map[string]any) (json.RawMessage, error) {
	c, err := flags.newClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	payload, err := pgmcp.Call(ctx, c, name, args)
	if err != nil && pgRetryableInternal(name, err) {
		// One retry, inside the same deadline, for the vendor's transient
		// "Internal error while running this tool" on a free read.
		timer := time.NewTimer(pgInternalRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, pgErr(err)
		case <-timer.C:
		}
		payload, err = pgmcp.Call(ctx, c, name, args)
	}
	if err != nil {
		return nil, pgErr(err)
	}
	return payload, nil
}

const pgVendorInternalError = "Internal error while running this tool"

// pgInternalRetryDelay is the pause before pgCall's single internal-error retry.
var pgInternalRetryDelay = time.Second

// pgRetryableInternal reports whether err is the vendor's transient internal
// error from a known catalog tool that neither mutates nor spends. Paid,
// mutating and unknown tools, and every other failure, are never retried.
func pgRetryableInternal(name string, err error) bool {
	t, ok := pgmcp.Lookup(name)
	if !ok || t.Mutating || t.Cost.Spends() {
		return false
	}
	var me *pgmcp.Error
	return errors.As(err, &me) && me.Kind == pgmcp.KindGeneric &&
		me.Message == pgVendorInternalError
}

// pgPreview is what a dry run prints: the exact JSON-RPC body and its price.
type pgPreview struct {
	DryRun  bool          `json:"dry_run"`
	Success bool          `json:"success"`
	Action  string        `json:"action"`
	Tool    string        `json:"tool"`
	Credits string        `json:"credits"`
	Request pgmcp.Request `json:"request"`
	Would   string        `json:"would"`
}

func writePGPreview(cmd *cobra.Command, flags *rootFlags, t pgmcp.Tool, args map[string]any, action string) error {
	would := "send this request; nothing was sent"
	if t.Cost.Spends() {
		would = "spend " + t.Cost.Note() + " and post to Pinterest; re-run with --execute to send"
	} else if t.Mutating {
		would = "change account state; re-run without --dry-run to send"
	}
	// --select targets the real payload; a preview envelope would always miss.
	if flags != nil && flags.selectFields != "" {
		saved := flags.selectFields
		flags.selectFields = ""
		defer func() { flags.selectFields = saved }()
	}
	p := pgPreview{DryRun: true, Success: true, Action: action, Tool: t.Name, Credits: t.Cost.Note(), Request: pgmcp.BuildRequest(t.Name, args), Would: would}
	if !wantsHumanTable(cmd.OutOrStdout(), flags) {
		return printJSONFiltered(cmd.OutOrStdout(), p, flags)
	}
	b, _ := json.MarshalIndent(p.Request, "", "  ")
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	fmt.Fprintf(cmd.ErrOrStderr(), "\ndry run: would %s.\n", would)
	return nil
}

// pgRunTool is the single path every typed and novel command uses to fire a
// tool. Credit-spending tools stay a free preview unless execute is true;
// --dry-run always previews. Executed spends are appended to the ledger.
func pgRunTool(cmd *cobra.Command, flags *rootFlags, t pgmcp.Tool, args map[string]any, execute bool) (json.RawMessage, bool, error) {
	if dryRunOK(flags) || (t.Cost.Spends() && !execute) {
		return nil, true, writePGPreview(cmd, flags, t, args, cmd.Name())
	}
	if t.Cost.Spends() && cliutil.IsAnyHarness() {
		return nil, true, writeHarnessRefusal(cmd.OutOrStdout(), flags, "spend Pin Generator credits")
	}
	if t.Cost.Spends() && !flags.timeoutExplicit && flags.timeout < pgSpendTimeout {
		// AI generation is slow; a client-side timeout after the server
		// accepted the call would hide a real spend.
		flags.timeout = pgSpendTimeout
	}
	payload, err := pgCall(cmd, flags, t.Name, args)
	if err != nil {
		if t.Cost.Spends() && pgAmbiguousSpend(err) {
			pgLedgerAppend(cmd, t, args, nil, pgmcp.StatusUnconfirmed)
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s may have spent credits before failing; recorded as unconfirmed in the ledger. Check 'list-scheduled-pins' before retrying.\n", t.Name)
		}
		return nil, false, err
	}
	if t.Cost.Spends() {
		pgLedgerAppend(cmd, t, args, payload, "")
	}
	return payload, false, nil
}

// pgSpendLocked makes one paid call for guardURL while holding the ledger's
// cross-process lock (cliutil.WithFileLock: process mutex plus OS file lock),
// so overlapping runs cannot both pay for the same post or product. Under the
// lock it re-reads the ledger, skips a URL already ledgered (unless force),
// makes the call, and ledgers the result before releasing. A ledger that
// cannot be read fails closed: nothing is spent.
func pgSpendLocked(cmd *cobra.Command, flags *rootFlags, t pgmcp.Tool, args map[string]any, guardURL string, force bool) (payload json.RawMessage, previewed, skipped bool, err error) {
	path, err := pgLedgerPath()
	if err != nil {
		return nil, false, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, false, err
	}
	spend := func() error {
		if !force && guardURL != "" {
			entries, _, rerr := pgmcp.ReadLedger(path)
			if rerr != nil {
				return fmt.Errorf("reading the spend ledger before paying (nothing was spent): %w", rerr)
			}
			if pgmcp.LedgeredURLs(entries)[pgmcp.NormalizeURL(guardURL)] {
				skipped = true
				return nil
			}
		}
		var cerr error
		payload, previewed, cerr = pgRunTool(cmd, flags, t, args, true)
		return cerr
	}
	ctx, cancel := boundCtx(cmd.Context(), flags)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		acquired, err := cliutil.TryWithFileLock(path, func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return spend()
		})
		if acquired || err != nil {
			return payload, previewed, skipped, err
		}
		select {
		case <-ctx.Done():
			return nil, false, false, ctx.Err()
		case <-tick.C:
		}
	}
}

// pgSpendTimeout is the floor for credit-spending calls (the prior CLI's 120s).
const pgSpendTimeout = 120 * time.Second

// pgAmbiguousSpend reports whether a failed spend may still have been
// accepted: a deadline or cancellation, a transport failure after the
// connection was made, or a 5xx. Tool rejections, 4xx, 429, dial and DNS
// failures, and refusals raised before sending mean nothing was spent.
func pgAmbiguousSpend(err error) bool {
	var me *pgmcp.Error
	var rl *platform.RateLimitedError
	var ae *client.APIError
	var op *net.OpError
	var dns *net.DNSError
	var ue *url.Error
	switch {
	case errors.As(err, &me):
		// An unreadable reply came back after the server took the call;
		// only an explicit JSON-RPC or tool rejection proves no spend.
		return me.Kind == pgmcp.KindUnreadable
	case errors.As(err, &rl):
		return false
	case errors.As(err, &ae):
		return ae.StatusCode >= 500
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return true
	case errors.As(err, &dns), errors.As(err, &op) && op.Op == "dial":
		return false
	case errors.Is(err, client.ErrPlaintextCredential), errors.Is(err, client.ErrPlaceholderCredential):
		return false
	case errors.As(err, &ue), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET):
		return true
	default:
		return false
	}
}

func pgLedgerAppend(cmd *cobra.Command, t pgmcp.Tool, args map[string]any, payload json.RawMessage, status string) {
	path, err := pgLedgerPath()
	if err == nil {
		e := pgmcp.NewLedgerEntry(t, args, payload, cmd.CommandPath(), time.Now())
		e.Status = status
		err = pgmcp.AppendLedger(path, e)
	}
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: ledger write failed: %v\n", err)
	}
}

// pgReadValues returns the tool arguments the caller actually set, keyed by
// MCP argument name. Flags are declared statically per command in
// pingenerator_tools_static.go so verify-skill can see them.
func pgReadValues(cmd *cobra.Command, t pgmcp.Tool) (map[string]any, error) {
	vals := map[string]any{}
	fs := cmd.Flags()
	for _, p := range t.Params {
		name := p.Flag()
		if fs.Lookup(name) == nil {
			return nil, fmt.Errorf("internal: %s has no --%s flag; regenerate pingenerator_tools_static.go", t.Cmd, name)
		}
		if !fs.Changed(name) {
			continue
		}
		var v any
		var err error
		switch p.Type {
		case pgmcp.String, pgmcp.RawJSON:
			v, err = fs.GetString(name)
		case pgmcp.Bool:
			v, err = fs.GetBool(name)
		case pgmcp.Int:
			v, err = fs.GetInt(name)
		case pgmcp.Array, pgmcp.ImageLayers:
			v, err = fs.GetStringArray(name)
		}
		if err != nil {
			return nil, err
		}
		if s, ok := v.(string); ok {
			if env := pgToolFixtures[t.Name].flagEnv[name]; env != "" {
				v = pgFixtureValue(s, env)
			}
		}
		vals[p.API] = v
	}
	return vals, nil
}

// pgFlagDesc is the help text for one tool argument's flag.
func pgFlagDesc(p pgmcp.Param) string {
	desc := p.Desc
	if p.Required {
		desc = "(required) " + desc
	}
	if len(p.Enum) > 0 {
		desc += " One of: " + strings.Join(p.Enum, ", ") + "."
	}
	return desc
}

func hasRequired(t pgmcp.Tool) bool {
	for _, p := range t.Params {
		if p.Required {
			return true
		}
	}
	return false
}

// pgWireToolCmd finishes a statically declared tool command: help text,
// annotations and the shared RunE.
func pgWireToolCmd(flags *rootFlags, name string, cmd *cobra.Command) *cobra.Command {
	t, ok := pgmcp.Lookup(name)
	if !ok {
		panic("pingenerator: unknown tool " + name)
	}
	short := t.Summary
	if t.Cost.Spends() {
		short += " (" + t.Cost.Note() + "; dry run unless --execute)"
		cmd.Long = short + "\n\nWithout --execute this prints the exact JSON-RPC request and spends nothing. " +
			"With --execute it spends " + t.Cost.Note() + ", posts to Pinterest, and records the spend in the local ledger (see 'ledger')."
	}
	cmd.Short, cmd.Example = short, t.Example
	cmd.Annotations = map[string]string{"pp:data-source": "live", "pp:mcp-tool": t.Name}
	if !t.Mutating {
		cmd.Annotations["mcp:read-only"] = "true"
	}
	if t.HappyArgs != "" {
		cmd.Annotations["pp:happy-args"] = t.HappyArgs
		if fx, ok := pgToolFixtures[t.Name]; ok {
			envs := make([]string, 0, len(fx.flagEnv))
			for _, e := range fx.flagEnv {
				envs = append(envs, e)
			}
			cmd.Annotations["pp:happy-args"] = pgDogfoodHappyArgs(t.HappyArgs, fx.args, envs...)
		}
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return usageErr(fmt.Errorf("%s takes no positional arguments; use flags (see --help)", t.Cmd))
		}
		if hasRequired(t) && cmd.Flags().NFlag() == 0 {
			return cmd.Help()
		}
		vals, err := pgReadValues(cmd, t)
		if err != nil {
			return err
		}
		toolArgs, err := pgmcp.BuildArgs(t, vals)
		if err != nil {
			return usageErr(err)
		}
		execute := false
		if t.Cost.Spends() {
			execute, _ = cmd.Flags().GetBool("execute")
		}
		if !dryRunOK(flags) && !(t.Cost.Spends() && !execute) {
			if missing := pgmcp.MissingRequired(t, vals); len(missing) > 0 && !requiredInputWaived() {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("missing required flag(s): --%s", strings.Join(missing, ", --")))
			}
		}
		payload, previewed, err := pgRunTool(cmd, flags, t, toolArgs, execute)
		if err != nil || previewed {
			return err
		}
		return printOutputWithFlags(cmd.OutOrStdout(), payload, flags)
	}
	return cmd
}

type pgCatalogRow struct {
	Command  string `json:"command"`
	Tool     string `json:"tool"`
	Summary  string `json:"summary"`
	Credits  string `json:"credits"`
	Mutating bool   `json:"mutating"`
}

func newPGToolsCatalogCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:         "tools",
		Short:       "List every Pin Generator MCP tool this CLI exposes, with credit costs (offline, free).",
		Example:     "  pingenerator-pp-cli tools --agent",
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "computed"},
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := make([]pgCatalogRow, 0, len(pgmcp.Tools))
			for _, t := range pgmcp.Tools {
				rows = append(rows, pgCatalogRow{Command: t.Cmd, Tool: t.Name, Summary: t.Summary, Credits: t.Cost.Note(), Mutating: t.Mutating})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Command < rows[j].Command })
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%-30s [%s]\n    %s\n", r.Command, r.Credits, r.Summary)
			}
			return nil
		},
	}
}
