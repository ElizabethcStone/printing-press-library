package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/crocoblock/internal/client"
	"github.com/spf13/cobra"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var crocoSSHHostRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.@-]*$`)

func crocoShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func crocoSSH(ctx context.Context, host, key, command, input string) ([]byte, error) {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=12"}
	if key != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", key)
	}
	args = append(args, host, command)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = strings.NewReader(input)
	out, e := cmd.CombinedOutput()
	if e != nil {
		return nil, fmt.Errorf("SSH/WP-CLI command failed: %w (no automatic retry)", e)
	}
	return out, nil
}

func crocoReindexPHP(baseURL string) string {
	site := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/wp-json")
	// A JSON string is not a PHP string literal: PHP interpolates dollars.
	// Base64's fixed alphabet is safe inside this single-quoted expression.
	encoded := base64.StdEncoding.EncodeToString([]byte(site))
	return "<?php\n$expected = base64_decode('" + encoded + "');\n" + `if (rtrim(get_option('siteurl'), '/') !== rtrim($expected, '/')) { fwrite(STDERR,'Site mismatch'); exit(9); }
if (!function_exists('jet_smart_filters')) { fwrite(STDERR,'JetSmartFilters is not active'); exit(9); }
$admins=get_users(array('role'=>'administrator','number'=>1,'fields'=>'ID')); if (!$admins) { exit(9); } wp_set_current_user($admins[0]);
$ok=jet_smart_filters()->indexer->index_filters(); if (!$ok) { fwrite(STDERR,'Indexer returned failure'); exit(9); }
global $wpdb; $table=jet_smart_filters()->indexer->table_name;
$count=(int)$wpdb->get_var("SELECT COUNT(*) FROM ".esc_sql($table));
echo "PP_INDEX_RESULT=".wp_json_encode(array('success'=>true,'indexed_rows'=>$count,'site'=>get_option('siteurl')))."\n";
`
}

func crocoReindexCmd(f *rootFlags) *cobra.Command {
	var host, key, path string
	cmd := crocoCommand(f, "reindex", "Rebuild the native filter index over an explicitly selected SSH installation", false, func(ctx context.Context, c *client.Client, cmd *cobra.Command, args []string) (any, error) {
		if !crocoSSHHostRE.MatchString(host) || strings.HasPrefix(host, "-") {
			return nil, usageErr(fmt.Errorf("--ssh must name the authorized SSH host (optionally user@host)"))
		}
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n\x00") {
			return nil, usageErr(fmt.Errorf("--wp-path must be an absolute WordPress installation path"))
		}
		// Upload a nonexecutable PHP source file, avoiding host-specific shell parsing of PHP arguments.
		nonce := make([]byte, 12)
		if _, e := rand.Read(nonce); e != nil {
			return nil, e
		}
		remote := strings.TrimRight(path, "/") + "/wp-content/.pp-index-" + hex.EncodeToString(nonce) + ".txt"
		php := crocoReindexPHP(c.BaseURL)
		db, e := crocoOpenStore(ctx, "")
		if e != nil {
			return nil, apiErr(fmt.Errorf("prepare index journal: %w", e))
		}
		defer db.Close()
		if e = db.CrocoCheckSite(ctx, c.BaseURL); e != nil {
			return nil, e
		}
		events, e := db.CrocoEvents(ctx, 0)
		if e != nil {
			return nil, e
		}
		var through int64
		for _, v := range events {
			n, _ := crocoInt(v["id"])
			if n > through {
				through = n
			}
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = crocoSSH(cleanup, host, key, "rm -f -- "+crocoShellQuote(remote), "")
		}()
		if _, e := crocoSSH(ctx, host, key, "cat > "+crocoShellQuote(remote), php); e != nil {
			return nil, e
		}
		out, e := crocoSSH(ctx, host, key, "wp --path="+crocoShellQuote(path)+" eval-file "+crocoShellQuote(remote), "")
		if e != nil {
			return nil, apiErr(e)
		}
		marker := "PP_INDEX_RESULT="
		idx := strings.LastIndex(string(out), marker)
		if idx < 0 {
			return nil, apiErr(fmt.Errorf("index result missing; verify upstream before retrying"))
		}
		line := strings.Split(strings.TrimSpace(string(out[idx+len(marker):])), "\n")[0]
		var result map[string]any
		if e = json.Unmarshal([]byte(line), &result); e != nil || result["success"] != true {
			return nil, apiErr(fmt.Errorf("invalid index verification result"))
		}
		id, e := db.CrocoEvent(ctx, c.BaseURL, "reindex", "filters", nil, map[string]any{"verified": true, "count": result["indexed_rows"], "reindex_event_id": through})
		if e != nil {
			return nil, apiErr(fmt.Errorf("index rebuilt but receipt failed: %w", e))
		}
		result["journal_event_id"] = id
		result["covers_through_event"] = through
		return result, nil
	})
	cmd.Flags().StringVar(&host, "ssh", "", "SSH user@host for the authorized WordPress installation")
	cmd.Flags().StringVar(&key, "ssh-key", "", "Private key file for SSH, managed outside the CLI")
	cmd.Flags().StringVar(&path, "wp-path", "", "Absolute WordPress installation path on the SSH host")
	cmd.Annotations["mcp:read-flags"] = "ssh-key"
	cmd.Example = "  crocoblock-pp-cli filters reindex --ssh site@host --wp-path /sites/site --dry-run --agent"
	return cmd
}
