package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"

	"github.com/mvanhorn/printing-press-library/library/productivity/concur/internal/client"
	"github.com/spf13/cobra"
)

// PATCH(amend-2026-10-15: infer policyId over HTTP before the browser
// fallback) -- on a policy-gated tenant the create call is rejected with
// "policyId is required", and the only recovery was a browser fallback that
// needs its own, separate Concur login. Every report already carries the
// policy it was created under, so the user's most recent report is enough to
// retry over the same authenticated HTTP session; the browser stays as the
// last resort for a user with no report to learn a policy from.

// newestReportIDs returns up to n report IDs from a reports list response,
// newest reportDate first. The response is walked structurally rather than
// decoded into a fixed shape because the list envelope has varied between
// API revisions.
func newestReportIDs(raw []byte, n int) []string {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	type report struct{ id, date string }
	var reports []report
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if id, ok := t["reportId"].(string); ok && id != "" {
				date, _ := t["reportDate"].(string)
				reports = append(reports, report{id: id, date: date})
				return
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(doc)
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].date != reports[j].date {
			return reports[i].date > reports[j].date
		}
		return reports[i].id > reports[j].id
	})
	if len(reports) > n {
		reports = reports[:n]
	}
	ids := make([]string, 0, len(reports))
	for _, r := range reports {
		ids = append(ids, r.id)
	}
	return ids
}

// policyFromReportHeader reads the policy a report was created under.
func policyFromReportHeader(raw []byte) (id, name string) {
	var header struct {
		PolicyID string `json:"policyId"`
		Policy   string `json:"policy"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return "", ""
	}
	return header.PolicyID, header.Policy
}

// inferReportPolicy returns the policy of the user's most recent report that
// carries one. It looks at a few reports, not just the newest, so one oddly
// shaped header does not defeat the inference.
func inferReportPolicy(ctx context.Context, c *client.Client, userID, contextType string) (policyID, policyName string, err error) {
	listPath := "/expensereports/v4/users/{user_id}/context/{context_type}/reports"
	listPath = replacePathParam(listPath, "user_id", formatCLIParamValue(userID))
	listPath = replacePathParam(listPath, "context_type", formatCLIParamValue(contextType))
	raw, err := c.Get(ctx, listPath, nil)
	if err != nil {
		return "", "", fmt.Errorf("listing reports: %w", err)
	}
	for _, reportID := range newestReportIDs(raw, 3) {
		header, err := c.Get(ctx, listPath+"/"+url.PathEscape(reportID), nil)
		if err != nil {
			continue
		}
		if id, name := policyFromReportHeader(header); id != "" {
			return id, name, nil
		}
	}
	return "", "", errors.New("no existing report carries a policyId")
}

// retryReportCreateWithInferredPolicy retries a create that the API rejected
// for a missing policyId, using the policy of the user's most recent report.
// When it cannot infer one it returns origErr untouched so the caller's
// browser fallback still runs. A policyId the caller supplied is never
// replaced: if the API still wants one, the caller's value is the problem.
func retryReportCreateWithInferredPolicy(cmd *cobra.Command, c *client.Client, path string, params map[string]string, body any, userID, contextType string, origErr error) (json.RawMessage, int, error) {
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return nil, 0, origErr
	}
	if given, _ := bodyMap["policyId"].(string); given != "" {
		return nil, 0, origErr
	}
	policyID, policyName, err := inferReportPolicy(cmd.Context(), c, userID, contextType)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Could not infer a policyId from your existing reports: %v\n", err)
		return nil, 0, origErr
	}
	if policyName == "" {
		policyName = policyID
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "policyId is required; retrying with %q from your most recent report. To use another policy, pass policyId in a --stdin body.\n", policyName)
	retryBody := make(map[string]any, len(bodyMap)+1)
	for k, v := range bodyMap {
		retryBody[k] = v
	}
	retryBody["policyId"] = policyID
	return c.PostWithParams(cmd.Context(), path, params, retryBody)
}
