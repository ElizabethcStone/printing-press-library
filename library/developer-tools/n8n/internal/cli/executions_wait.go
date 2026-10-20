// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/developer-tools/n8n/internal/n8nwf"
	"github.com/spf13/cobra"
)

type executionStatus struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflowId"`
	Status     string `json:"status"`
	Finished   bool   `json:"finished"`
	StartedAt  string `json:"startedAt"`
	StoppedAt  string `json:"stoppedAt"`
}

func newNovelExecutionsWaitCmd(flags *rootFlags) *cobra.Command {
	var workflow, after, maxWait, interval string
	cmd := &cobra.Command{Use: "wait [executionId]", Short: "Block until an execution finishes and exit non-zero if it failed, with the failing node and message.",
		Long: "Use this command to block on one execution until it finishes and get its result as an exit code. Do NOT use it for an ongoing live feed of executions; use 'tail --resource executions --follow' instead.\n\nExit codes: 0 success, 1 the execution ended in error, crashed, or canceled, 6 --max-wait elapsed before it finished.",
		Example: strings.Trim(`
  n8n-pp-cli executions wait 1234 --max-wait 5m --agent
  n8n-pp-cli executions wait --workflow 0vB2cZqgHEmb3RKs --after 1m`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:typed-exit-codes": "0,1,6", "pp:happy-args": "executionId=1;--max-wait=10s", "pp:verified-by-proof": "executions-wait-lifecycle.md"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "executions wait")
			}
			if len(args) > 1 || (len(args) == 0) == (workflow == "") {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("provide exactly one executionId or --workflow <id>"))
			}
			// --max-wait bounds the polling loop; root --timeout still bounds each request
			// through the client, so boundCtx would wrongly cap the whole wait at --timeout.
			ctx := cmd.Context()
			waitDur, err := cliutil.ParseDurationLoose(maxWait)
			if err != nil || waitDur <= 0 {
				if err == nil {
					err = fmt.Errorf("must be positive")
				}
				return usageErr(fmt.Errorf("invalid --max-wait: %w", err))
			}
			pollDur, err := cliutil.ParseDurationLoose(interval)
			if err != nil || pollDur <= 0 {
				if err == nil {
					err = fmt.Errorf("must be positive")
				}
				return usageErr(fmt.Errorf("invalid --interval: %w", err))
			}
			if cliutil.IsDogfoodEnv() && waitDur > 20*time.Second {
				waitDur = 20 * time.Second
			}
			afterTime := time.Now().UTC().Add(-5 * time.Second)
			if after != "" {
				if t, e := time.Parse(time.RFC3339, after); e == nil {
					afterTime = t
				} else if d, e := cliutil.ParseDurationLoose(after); e == nil && d > 0 {
					afterTime = time.Now().UTC().Add(-d)
				} else {
					return usageErr(fmt.Errorf("--after must be RFC3339 or a positive duration"))
				}
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			c.NoCache = true
			started := time.Now()
			deadline := started.Add(waitDur)
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			var ex executionStatus
			// Requests inside the poll loop share the --max-wait deadline so a slow call cannot overrun it.
			pollCtx, cancelPoll := context.WithDeadline(ctx, deadline)
			defer cancelPoll()
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				var raw json.RawMessage
				if workflow == "" {
					raw, err = c.GetNoCache(pollCtx, "/executions/"+id, nil)
					if err != nil && pollCtx.Err() != nil && ctx.Err() == nil {
						break
					}
					if err != nil {
						if isNotFound(err) {
							return notFoundErr(fmt.Errorf("execution %s not found", id))
						}
						return classifyAPIError(cmd.OutOrStdout(), err, flags)
					}
					if err := json.Unmarshal(raw, &ex); err != nil {
						return fmt.Errorf("decode execution: %w", err)
					}
				} else {
					raw, err = c.GetNoCache(pollCtx, "/executions", map[string]string{"workflowId": workflow, "limit": "5"})
					if err != nil && pollCtx.Err() != nil && ctx.Err() == nil {
						break
					}
					if err != nil {
						return classifyAPIError(cmd.OutOrStdout(), err, flags)
					}
					var page struct {
						Data []json.RawMessage `json:"data"`
					}
					if err := json.Unmarshal(raw, &page); err != nil {
						return fmt.Errorf("decode executions: %w", err)
					}
					ex = executionStatus{}
					for _, body := range page.Data {
						var candidate executionStatus
						if err := json.Unmarshal(body, &candidate); err != nil {
							continue
						}
						t, e := time.Parse(time.RFC3339Nano, candidate.StartedAt)
						if e == nil && !t.Before(afterTime) && (ex.StartedAt == "" || candidate.StartedAt > ex.StartedAt) {
							ex = candidate
						}
					}
					id = ex.ID
				}
				if id != "" && (terminal(ex.Status) || ex.Finished) {
					break
				}
				if !time.Now().Before(deadline) {
					break
				}
				timer := time.NewTimer(min(pollDur, time.Until(deadline)))
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
			timedOut := !terminal(ex.Status) && !ex.Finished
			failedNode, message := "", ""
			if !timedOut && (ex.Status == "error" || ex.Status == "crashed" || ex.Status == "canceled") {
				body, e := c.GetNoCache(ctx, "/executions/"+id, map[string]string{"includeData": "true"})
				if e != nil {
					return classifyAPIError(cmd.OutOrStdout(), e, flags)
				}
				f := n8nwf.ExecutionFailure(body)
				failedNode, message = f.Node, f.Message
			}
			duration := any(nil)
			if a, e1 := time.Parse(time.RFC3339Nano, ex.StartedAt); e1 == nil {
				if b, e2 := time.Parse(time.RFC3339Nano, ex.StoppedAt); e2 == nil {
					duration = b.Sub(a).Milliseconds()
				}
			}
			out := map[string]any{"execution_id": id, "workflow_id": ex.WorkflowID, "status": ex.Status, "finished": ex.Finished, "started_at": ex.StartedAt, "stopped_at": ex.StoppedAt, "failed_node": failedNode, "error_message": message, "waited_ms": time.Since(started).Milliseconds(), "timed_out": timedOut, "found": id != ""}
			if duration != nil {
				out["duration_ms"] = duration
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				if err := printJSONFiltered(cmd.OutOrStdout(), out, flags); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "execution %s: %s", id, ex.Status)
				if failedNode != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " at %s: %s", failedNode, message)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if timedOut {
				if id == "" {
					return &cliError{code: 6, err: fmt.Errorf("no execution of workflow %s started after %s within %s", workflow, afterTime.Format(time.RFC3339), maxWait)}
				}
				return &cliError{code: 6, err: fmt.Errorf("execution %s did not finish within %s", id, maxWait)}
			}
			if ex.Status != "success" {
				return &cliError{code: 1, err: fmt.Errorf("execution %s ended with status %s", id, ex.Status)}
			}
			return nil
		}}
	cmd.Flags().StringVar(&workflow, "workflow", "", "Wait for newest execution of this workflow")
	cmd.Flags().StringVar(&after, "after", "", "For --workflow, only executions started after this time or duration ago")
	cmd.Flags().StringVar(&maxWait, "max-wait", "5m", "Maximum time to wait")
	cmd.Flags().StringVar(&interval, "interval", "2s", "Polling interval")
	return cmd
}

func terminal(status string) bool {
	return status == "success" || status == "error" || status == "crashed" || status == "canceled"
}
