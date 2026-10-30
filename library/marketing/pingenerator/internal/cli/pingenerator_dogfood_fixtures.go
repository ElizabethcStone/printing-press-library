// Copyright 2026 Elizabeth Stone and contributors. Licensed under Apache-2.0. See LICENSE.
// pp:data-source live
// TEST-ONLY live-dogfood fixtures. Some read-only previews need inputs that
// only a real account has (a connected Pinterest profile, a connected store,
// a public post). Committing those values would publish account data, so the
// operator supplies them through PINGENERATOR_TEST_* env vars for a live gate
// run with PRINTING_PRESS_DOGFOOD=1 exported. Outside dogfood nothing here
// changes behavior: annotations stay explicit example-value blocked fixtures
// and the sentinel is never resolved. Annotations carry only the sentinel, so
// gate records never contain the real values, and no fixture adds --execute.

package cli

import (
	"os"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/marketing/pingenerator/internal/cliutil"
)

const (
	pgTestPostURL    = "PINGENERATOR_TEST_POST_URL"
	pgTestWebsiteURL = "PINGENERATOR_TEST_WEBSITE_URL"
	pgTestProfile    = "PINGENERATOR_TEST_PROFILE"
	pgTestStoreID    = "PINGENERATOR_TEST_STORE_ID"

	// pgEnvFixture stands in for an env-supplied value in pp:happy-args.
	pgEnvFixture = "pp-env-fixture"
)

// pgDogfoodEnv returns a test fixture value, only under live dogfood.
func pgDogfoodEnv(name string) (string, bool) {
	if !cliutil.IsDogfoodEnv() {
		return "", false
	}
	v := strings.TrimSpace(os.Getenv(name))
	return v, v != ""
}

// pgDogfoodHappyArgs picks envArgs when running under dogfood with every
// named env var set; otherwise the blocked-fixture fallback.
func pgDogfoodHappyArgs(fallback, envArgs string, envs ...string) string {
	for _, e := range envs {
		if _, ok := pgDogfoodEnv(e); !ok {
			return fallback
		}
	}
	return envArgs
}

// pgFixtureValue swaps the sentinel for its env value under dogfood only.
func pgFixtureValue(v, env string) string {
	if v != pgEnvFixture {
		return v
	}
	if real, ok := pgDogfoodEnv(env); ok {
		return real
	}
	return v
}

// pgToolFixtures maps typed read tools to their env-backed happy-args and
// the env var behind each sentinel-bearing flag. get_automation_batch_status
// stays a blocked fixture: the account has no automation batch.
var pgToolFixtures = map[string]struct {
	args    string
	flagEnv map[string]string
}{
	"list_boards":         {"--pinterest-profile=" + pgEnvFixture, map[string]string{"pinterest-profile": pgTestProfile}},
	"get_post_content":    {"--post-url=" + pgEnvFixture, map[string]string{"post-url": pgTestPostURL}},
	"list_store_products": {"--store-id=" + pgEnvFixture, map[string]string{"store-id": pgTestStoreID}},
}
