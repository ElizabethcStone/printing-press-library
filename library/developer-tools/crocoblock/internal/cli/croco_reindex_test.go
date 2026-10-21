package cli

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCrocoReindexPHPKeepsURLLiteral(t *testing.T) {
	assignment := regexp.MustCompile(`\A<\?php\n\$expected = base64_decode\('([A-Za-z0-9+/=]*)'\);\n`)
	for _, site := range []string{
		"https://example.test",
		"https://example.test/$name",
		"https://example.test/${phpinfo()}",
		"https://example.test/${system('id')}",
		`https://example.test/'"\${value}`,
		"https://example.test/área",
	} {
		t.Run(site, func(t *testing.T) {
			source := crocoReindexPHP(site + "/wp-json/")
			match := assignment.FindStringSubmatch(source)
			if len(match) != 2 {
				t.Fatal("URL was not emitted solely as a fixed-alphabet literal")
			}
			decoded, err := base64.StdEncoding.DecodeString(match[1])
			if err != nil || string(decoded) != site {
				t.Fatalf("decoded site = %q, error = %v", decoded, err)
			}
			if strings.Contains(source, site) {
				t.Fatal("raw URL was inserted into PHP source")
			}
			guard := strings.Index(source, "if (rtrim(get_option('siteurl')")
			write := strings.Index(source, "->index_filters()")
			if guard < len(match[0]) || write <= guard {
				t.Fatal("site check must precede native index mutation")
			}
		})
	}
}

func TestCrocoReindexCleansPartialUpload(t *testing.T) {
	f, _ := crocoEngineTestHTTP(t, func(http.ResponseWriter, *http.Request) {
		t.Error("reindex must not make REST requests")
	})
	dir := t.TempDir()
	log := filepath.Join(dir, "ssh.log")
	t.Setenv("CROCO_TEST_SSH_LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CROCO_TEST_SSH_LOG\"\ncase \"$*\" in\n*\"cat > \"*) cat >/dev/null; exit 7 ;;\n*\"rm -f -- \"*) exit 0 ;;\n*) exit 9 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := crocoEngineTestRun(crocoReindexCmd(f), "--ssh", "fixture@host", "--wp-path", "/sites/test with 'quote")
	if err == nil || !strings.Contains(err.Error(), "SSH/WP-CLI command failed") {
		t.Fatalf("upload failure = %v", err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "cat > ") || !strings.Contains(lines[1], "rm -f -- ") || strings.Contains(string(b), "eval-file") {
		t.Fatalf("failed upload must be cleaned without evaluation: %s", b)
	}
}

func TestCrocoReindexJournalFailureIsPrewrite(t *testing.T) {
	f, _ := crocoEngineTestHTTP(t, func(http.ResponseWriter, *http.Request) {
		t.Error("reindex must not make REST requests")
	})
	t.Setenv("CROCOBLOCK_DB", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // Any attempted SSH would fail with executable-not-found.
	_, err := crocoEngineTestRun(crocoReindexCmd(f), "--ssh", "fixture@host", "--wp-path", "/sites/test")
	if err == nil || !strings.Contains(err.Error(), "prepare index journal") || strings.Contains(err.Error(), "index rebuilt") || strings.Contains(err.Error(), "SSH") {
		t.Fatalf("prewrite journal failure = %v", err)
	}
}
