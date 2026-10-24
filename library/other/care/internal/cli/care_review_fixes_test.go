package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/other/care/internal/client"
	"github.com/mvanhorn/printing-press-library/library/other/care/internal/cliutil"
)

func TestCareHomeIsolationAndAuthLifecycle(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	t.Setenv("CARE_SESSION_COOKIE", "")

	restoreA, err := cliutil.SetHomeOverride(homeA)
	if err != nil {
		t.Fatalf("SetHomeOverride(homeA): %v", err)
	}
	defer restoreA()

	// Issue 2: all custom paths resolve under the active CLI home.
	for name, got := range map[string]string{
		"cookie":     client.CareCookieCachePath(),
		"profile":    careProfileDir(),
		"db":         careDBPath(),
		"active_job": careActiveJobPath(),
	} {
		if !strings.HasPrefix(got, homeA) {
			t.Fatalf("%s path %q does not live under homeA %q", name, got, homeA)
		}
	}

	if err := careCacheCookies("csc-session=test"); err != nil {
		t.Fatalf("careCacheCookies: %v", err)
	}
	if err := os.MkdirAll(careProfileDir(), 0o700); err != nil {
		t.Fatalf("MkdirAll(careProfileDir): %v", err)
	}

	// Issue 11: auth status and doctor hook recognize profile-login session.
	ok, src := doctorAuthConfiguredState(nil)
	if !ok || !strings.Contains(src, "profile-login") {
		t.Fatalf("doctorAuthConfiguredState = (%v, %q), want (true, profile-login)", ok, src)
	}
	var statusOut bytes.Buffer
	flags := &rootFlags{}
	statusCmd := newAuthStatusCmd(flags)
	statusCmd.SetOut(&statusOut)
	if err := statusCmd.RunE(statusCmd, nil); err != nil {
		t.Fatalf("auth status failed after profile-login cache: %v", err)
	}
	if !strings.Contains(statusOut.String(), "Authenticated") || !strings.Contains(statusOut.String(), "profile-login") {
		t.Fatalf("unexpected auth status output: %s", statusOut.String())
	}

	// Switching to homeB must not see homeA's session cookie.
	restoreB, err := cliutil.SetHomeOverride(homeB)
	if err != nil {
		t.Fatalf("SetHomeOverride(homeB): %v", err)
	}
	if got := client.SessionCookie(); got != "" {
		restoreB()
		t.Fatalf("homeB leaked session cookie from homeA: %q", got)
	}
	restoreB()

	// Issue 1: auth logout clears session-cookie and persistent browser profile.
	var logoutOut bytes.Buffer
	logoutCmd := newAuthLogoutCmd(flags)
	logoutCmd.SetOut(&logoutOut)
	if err := logoutCmd.RunE(logoutCmd, nil); err != nil {
		t.Fatalf("auth logout failed: %v", err)
	}
	if got := client.SessionCookie(); got != "" {
		t.Fatalf("SessionCookie still present after logout: %q", got)
	}
	if _, err := os.Stat(careProfileDir()); !os.IsNotExist(err) {
		t.Fatalf("careProfileDir still exists after logout: err=%v", err)
	}
}

func TestCareMarkContactedPreservedAcrossLaterSync(t *testing.T) {
	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("SetHomeOverride: %v", err)
	}
	defer restore()

	// Issue 3: mark-contacted before sync inserts a contact record that survives
	// a subsequent sync upsert.
	if err := careMarkContacted("cg-1", "Jane S."); err != nil {
		t.Fatalf("careMarkContacted: %v", err)
	}

	db, err := openCareDB()
	if err != nil {
		t.Fatalf("openCareDB: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`INSERT INTO caregivers
  (id,name,city,state,years_experience,hourly_rate,total_reviews,avg_rating,has_care_check,bio,zip,first_seen,last_synced)
  VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
  ON CONFLICT(id) DO UPDATE SET
    name=excluded.name, city=excluded.city, state=excluded.state,
    years_experience=excluded.years_experience, hourly_rate=excluded.hourly_rate,
    total_reviews=excluded.total_reviews, avg_rating=excluded.avg_rating,
    has_care_check=excluded.has_care_check, bio=excluded.bio, zip=excluded.zip,
    last_synced=excluded.last_synced`,
		"cg-1", "Jane S.", "Beverly Hills", "CA", 7, 25.0, 12, 5.0, 1, "Experienced nanny", "90210", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("sync upsert: %v", err)
	}

	var out bytes.Buffer
	flags := &rootFlags{asJSON: true}
	listCmd := newCareSyncListCmd(flags)
	listCmd.SetOut(&out)
	_ = listCmd.Flags().Set("uncontacted", "true")
	if err := listCmd.RunE(listCmd, nil); err != nil {
		t.Fatalf("sync list --uncontacted: %v", err)
	}
	if strings.Contains(out.String(), "cg-1") {
		t.Fatalf("already-contacted caregiver appeared in --uncontacted list: %s", out.String())
	}
}

func TestCareFirstInitialUTF8(t *testing.T) {
	// Issue 9: multi-byte first rune must not slice a single byte.
	if got := firstInitial("Élodie"); got != "É." {
		t.Fatalf("firstInitial(Élodie) = %q, want %q", got, "É.")
	}
	if got := firstInitial("张伟"); got != "张." {
		t.Fatalf("firstInitial(张伟) = %q, want %q", got, "张.")
	}
}

func TestCareFindConversationRejectsAmbiguousAndPages(t *testing.T) {
	// Issue 4 & Issue 7: paging across >30 channels and rejecting ambiguous matches.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Offset int `json:"offset"`
			Limit  int `json:"limit"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		type user struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		type member struct {
			User user `json:"user"`
		}
		type ch struct {
			Channel struct {
				CID string `json:"cid"`
			} `json:"channel"`
			Members []member `json:"members"`
		}
		var chans []ch
		if body.Offset == 0 {
			for i := 0; i < 30; i++ {
				name := "Other Caregiver"
				cid := "messaging:other"
				if i == 1 || i == 2 {
					name = "Jane S."
					if i == 2 {
						name = "Jane M."
					}
					cid = "messaging:jane-" + string(rune('0'+i))
				}
				c := ch{Members: []member{{User: user{ID: "me", Name: "Me"}}, {User: user{ID: "u", Name: name}}}}
				c.Channel.CID = cid
				chans = append(chans, c)
			}
		} else {
			c := ch{Members: []member{{User: user{ID: "me", Name: "Me"}}, {User: user{ID: "u-page2", Name: "PageTwo Nanny"}}}}
			c.Channel.CID = "messaging:page2"
			chans = append(chans, c)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"channels": chans})
	}))
	defer srv.Close()

	hc := srv.Client()
	hc.Transport = rewriteHostRoundTripper{target: srv.URL, base: http.DefaultTransport}
	creds := &careStreamCreds{APIKey: "k", Token: "t", UserID: "me"}

	_, err := careFindConversation(context.Background(), hc, creds, "Jane", 1)
	if err == nil || !strings.Contains(err.Error(), "matches 2 conversations") {
		t.Fatalf("expected ambiguous match error for Jane, got: %v", err)
	}

	got, err := careFindConversation(context.Background(), hc, creds, "PageTwo", 1)
	if err != nil || got.Channel.CID != "messaging:page2" {
		t.Fatalf("expected page-2 conversation messaging:page2, got %+v, err=%v", got, err)
	}
}

func TestCareFetchStreamCredsUsesCookieJarFallback(t *testing.T) {
	// Issue 5: careFetchStreamCreds resolves cookies from WriteCookieJarFromMap
	// when session-cookie is absent.
	home := t.TempDir()
	t.Setenv("CARE_SESSION_COOKIE", "")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("SetHomeOverride: %v", err)
	}
	defer restore()

	if err := client.WriteCookieJarFromMap(".care.com", map[string]string{"csc-session": "test"}); err != nil {
		t.Fatalf("WriteCookieJarFromMap: %v", err)
	}

	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"user_id":"seeker-123"}`))
	jwt := "hdr." + payload + ".sig"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "csc-session=test") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"isLoggedIn":true,"streamToken":"` + jwt + `","streamApiKey":"key-1"}`))
	}))
	defer srv.Close()

	hc := srv.Client()
	hc.Transport = rewriteHostRoundTripper{target: srv.URL, base: http.DefaultTransport}
	creds, err := careFetchStreamCreds(context.Background(), &rootFlags{}, hc)
	if err != nil {
		t.Fatalf("careFetchStreamCreds failed with jar cookie: %v", err)
	}
	if creds.UserID != "seeker-123" || creds.APIKey != "key-1" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}

func TestCareFavoriteAndOutreachSendJSON(t *testing.T) {
	// Issue 8: structured JSON output on favorite and outreach send previews.
	var favOut bytes.Buffer
	flags := &rootFlags{asJSON: true}
	favCmd := newCareFavoriteCmd(flags)
	favCmd.SetOut(&favOut)
	if err := favCmd.RunE(favCmd, []string{"cg-123"}); err != nil {
		t.Fatalf("favorite --json dry-run: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(favOut.Bytes(), &parsed); err != nil {
		t.Fatalf("favorite --json did not emit valid JSON: %v (%s)", err, favOut.String())
	}
	if parsed["dry_run"] != true || parsed["applied"] != false {
		t.Fatalf("unexpected favorite JSON: %+v", parsed)
	}
}

func TestCareFindSortsAllMatchesBeforeApplyingLimit(t *testing.T) {
	// Issue 10: over-fetched candidates later in the page that outrank earlier
	// candidates must not be discarded before sorting.
	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("SetHomeOverride: %v", err)
	}
	defer restore()

	if err := careWriteActiveJob(careJob{ID: "12345678", Title: "Summer Sitter", Status: "OPEN"}); err != nil {
		t.Fatalf("careWriteActiveJob: %v", err)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			OperationName string `json:"operationName"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body.OperationName == careQJobsBySeekerOp {
			_, _ = w.Write([]byte(`{"data":{"jobsBySeekerUUID":{"edges":[{"node":{"id":"12345678","title":"Summer Sitter","status":"OPEN"}}]}}}`))
			return
		}
		if body.OperationName == careQLoggedInUserOp {
			_, _ = w.Write([]byte(`{"data":{"loggedInUser":{"subject":"seeker-1"}}}`))
			return
		}
		// Return 3 matching caregivers for --limit 1 --min-exp 3: the 3rd edge
		// has 50 reviews and should win after sorting even though limit=1.
		_, _ = w.Write([]byte(`{"data":{"searchProvidersChildCare":{"searchProvidersConnection":{"totalHits":3,"edges":[` +
			`{"node":{"__typename":"Caregiver","member":{"id":"cg-low","firstName":"Low","lastName":"One"},"yearsOfExperience":5,"revieweeMetrics":{"metrics":{"totalReviews":2}}}},` +
			`{"node":{"__typename":"Caregiver","member":{"id":"cg-mid","firstName":"Mid","lastName":"Two"},"yearsOfExperience":5,"revieweeMetrics":{"metrics":{"totalReviews":10}}}},` +
			`{"node":{"__typename":"Caregiver","member":{"id":"cg-top","firstName":"Top","lastName":"Three"},"yearsOfExperience":8,"revieweeMetrics":{"metrics":{"totalReviews":50}}}}` +
			`]}}}}`))
	}))
	defer srv.Close()

	certPath := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("writing CA cert: %v", err)
	}
	t.Setenv("CARE_CA_BUNDLE", certPath)
	t.Setenv("CARE_BASE_URL", srv.URL)
	t.Setenv("CARE_SESSION_COOKIE", "csc-session=test")

	var out bytes.Buffer
	flags := &rootFlags{asJSON: true, caCert: certPath, insecure: true}
	findCmd := newCareFindCmd(flags)
	findCmd.SetContext(context.Background())
	findCmd.SetOut(&out)
	findCmd.SetErr(&bytes.Buffer{})
	_ = findCmd.Flags().Set("zip", "90210")
	_ = findCmd.Flags().Set("job", "12345678")
	_ = findCmd.Flags().Set("min-exp", "3")
	_ = findCmd.Flags().Set("limit", "1")
	if err := findCmd.RunE(findCmd, nil); err != nil {
		t.Fatalf("find failed: %v", err)
	}
	var results []careCaregiverSummary
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("unmarshaling find output: %v (%s)", err, out.String())
	}
	if len(results) != 1 || results[0].ID != "cg-top" {
		t.Fatalf("expected [cg-top], got %+v", results)
	}
}

type rewriteHostRoundTripper struct {
	target string
	base   http.RoundTripper
}

func (rt rewriteHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	targetURL := strings.TrimRight(rt.target, "/") + req.URL.RequestURI()
	u, err := req.URL.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	clone.URL = u
	clone.Host = u.Host
	return rt.base.RoundTrip(clone)
}
