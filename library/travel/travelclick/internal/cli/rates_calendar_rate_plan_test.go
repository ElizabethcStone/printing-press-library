package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/travel/travelclick/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/travel/travelclick/internal/cliutil/testenv"
)

// calendarRequestBody runs `rates calendar` against a fake server and returns
// the JSON body it POSTed. User-directory lookups are sandboxed so the run
// cannot read the operator's real config or credentials, and the empty
// "dates" response keeps the command from writing anything to the store.
func calendarRequestBody(t *testing.T, extraArgs ...string) map[string]any {
	t.Helper()
	testenv.Isolate(t, cliutil.StateDir)
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/basicavail/multi-room") {
			_ = json.NewDecoder(r.Body).Decode(&got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"dates":[]}`)
	}))
	t.Cleanup(server.Close)

	t.Setenv("TRAVELCLICK_BASE_URL", server.URL)
	t.Setenv("TRAVELCLICK_TOKEN", "test-token-not-real")

	args := append([]string{
		"rates", "calendar", "102306",
		"--hotel-code", "102306",
		"--from", "2026-09-01", "--to", "2026-09-30",
		"--multi-room-occupancy", `[{"adults":2,"infant":0,"children":0}]`,
		"--yes", "--json",
	}, extraArgs...)
	cmd := RootCmd()
	cmd.SetArgs(args)
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rates calendar failed: %v\nstderr: %s", err, errOut.String())
	}
	if got == nil {
		t.Fatalf("server never received a calendar POST\nstdout: %s\nstderr: %s", out.String(), errOut.String())
	}
	return got
}

func TestRatesCalendar_RatePlanFlags(t *testing.T) {
	t.Run("a default call is still the public rate calendar", func(t *testing.T) {
		body := calendarRequestBody(t)
		for _, field := range []string{"ratePlanCode", "ratePlanType"} {
			if _, present := body[field]; present {
				t.Errorf("default request must not send %s, got body %v", field, body)
			}
		}
	})

	t.Run("a rate plan code is sent with the Regular type by default", func(t *testing.T) {
		body := calendarRequestBody(t, "--rate-plan-code", "123456")
		if body["ratePlanCode"] != "123456" || body["ratePlanType"] != "Regular" {
			t.Errorf("want ratePlanCode=123456 ratePlanType=Regular, got %v", body)
		}
	})

	t.Run("an explicit rate plan type is sent as given", func(t *testing.T) {
		body := calendarRequestBody(t, "--rate-plan-code", "123456", "--rate-plan-type", "Group")
		if body["ratePlanCode"] != "123456" || body["ratePlanType"] != "Group" {
			t.Errorf("want ratePlanCode=123456 ratePlanType=Group, got %v", body)
		}
	})

	t.Run("an emptied rate plan type falls back to Regular rather than sending a blank", func(t *testing.T) {
		body := calendarRequestBody(t, "--rate-plan-code", "123456", "--rate-plan-type", "")
		if body["ratePlanType"] != "Regular" {
			t.Errorf("want ratePlanType=Regular, got %v", body)
		}
	})

	t.Run("a rate plan type without a code is ignored", func(t *testing.T) {
		body := calendarRequestBody(t, "--rate-plan-type", "Group")
		for _, field := range []string{"ratePlanCode", "ratePlanType"} {
			if _, present := body[field]; present {
				t.Errorf("a type alone must not send %s, got body %v", field, body)
			}
		}
	})
}
