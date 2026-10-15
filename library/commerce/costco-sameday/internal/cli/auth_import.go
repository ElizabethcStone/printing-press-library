// Copyright 2026 DashLabsDev and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-authored cookie import parsing and the names-only --diagnose report.

package cli

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse stages, in order. Errors and --diagnose name the stage reached so a
// failed import can be debugged without ever showing a cookie value.
const (
	stageRead           = "read-input"
	stageDetect         = "detect-format"
	stageExtract        = "extract-cookie"
	stageParsePairs     = "parse-cookie-pairs"
	stageRequireSession = "require-session-cookie"
	stageOK             = "ok"
)

// Detected input formats.
const (
	formatEmpty         = "empty"
	formatStorageState  = "playwright-storage-state"
	formatCookieHeader  = "cookie header"
	formatCurlB         = "cURL -b"
	formatCurlH         = "cURL -H"
	formatCurlBH        = "cURL -b and -H"
	formatCurlNoCookie  = "cURL without cookie"
	formatCurlCmd       = "cURL (cmd)"
	formatReqHeaders    = "request headers"
	formatFetch         = "fetch (browser, no cookies)"
	formatFetchNode     = "fetch (Node.js)"
	formatPowerShell    = "PowerShell"
	formatUnknown       = "unknown"
	maxCookieInputBytes = 4 << 20
)

// cookieImportError carries the failing stage and detected format. Messages
// are built only from names, counts, and fixed text, never cookie values.
type cookieImportError struct {
	Stage  string
	Format string
	Msg    string
}

func (e *cookieImportError) Error() string {
	if e.Format != "" {
		return fmt.Sprintf("cookie import failed at stage %s (input format: %s): %s", e.Stage, e.Format, e.Msg)
	}
	return fmt.Sprintf("cookie import failed at stage %s: %s", e.Stage, e.Msg)
}

// readCookieInput reads the whole stream with no line-length limit (no
// bufio.Scanner), capped at maxCookieInputBytes.
func readCookieInput(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxCookieInputBytes+1))
	if err != nil {
		return nil, &cookieImportError{Stage: stageRead, Msg: fmt.Sprintf("reading stdin: %v", err)}
	}
	if len(data) > maxCookieInputBytes {
		return nil, &cookieImportError{Stage: stageRead, Msg: fmt.Sprintf("input is larger than %d bytes", maxCookieInputBytes)}
	}
	return data, nil
}

var (
	curlStartRE    = regexp.MustCompile(`^curl(\.exe)?[ \t\r\n^]`)
	nodeCookieRE   = regexp.MustCompile(`(?i)"cookie"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	psCookieRE     = regexp.MustCompile("System\\.Net\\.Cookie\\(\\s*\"((?:[^\"`]|`.)*)\"\\s*,\\s*\"((?:[^\"`]|`.)*)\"")
	longDigitsRE   = regexp.MustCompile(`[0-9]{6,}`)
	longHexRE      = regexp.MustCompile(`[0-9A-Fa-f]{16,}`)
	promptPrefixRE = regexp.MustCompile(`^(\$|%|>)\s+`)
)

// extractCookieHeader turns pasted text into a Cookie header value. It
// accepts a bare header value, a "Cookie: ..." line, DevTools "Copy request
// headers", "Copy as cURL (bash)" (zsh pastes are identical), "Copy as cURL
// (cmd)", "Copy as fetch (Node.js)", and "Copy as PowerShell". Browser
// "Copy as fetch" has no cookies and gets a clear error.
func extractCookieHeader(text string) (header, format string, err error) {
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", formatEmpty, &cookieImportError{Stage: stageRead, Format: formatEmpty, Msg: "input is empty (0 non-blank bytes); the clipboard may be empty or pbpaste may not have clipboard access"}
	}
	text = promptPrefixRE.ReplaceAllString(text, "")
	if !utf8.ValidString(text) {
		return "", formatUnknown, &cookieImportError{Stage: stageDetect, Format: formatUnknown, Msg: "input is not valid UTF-8 text"}
	}

	switch {
	case curlStartRE.MatchString(text + " "):
		if strings.Contains(text, `^"`) || strings.Contains(text, "^\n") {
			return cookieFromCurlWords(splitShellWords(unescapeCmd(text)), formatCurlCmd)
		}
		return cookieFromCurlWords(splitShellWords(text), "")
	case strings.HasPrefix(text, "fetch(") || strings.HasPrefix(text, "await fetch("):
		var parts []string
		for _, m := range nodeCookieRE.FindAllStringSubmatch(text, -1) {
			v, uerr := strconv.Unquote(`"` + m[1] + `"`)
			if uerr != nil {
				return "", formatFetchNode, &cookieImportError{Stage: stageExtract, Format: formatFetchNode, Msg: "could not decode the quoted cookie header"}
			}
			parts = append(parts, strings.TrimSpace(v))
		}
		if len(parts) == 0 {
			return "", formatFetch, &cookieImportError{Stage: stageExtract, Format: formatFetch, Msg: `"Copy as fetch" never includes cookies (it uses credentials: "include"); use Copy > Copy as cURL (bash) instead`}
		}
		return strings.Join(parts, "; "), formatFetchNode, nil
	case strings.Contains(text, "System.Net.Cookie("):
		var parts []string
		for _, m := range psCookieRE.FindAllStringSubmatch(text, -1) {
			parts = append(parts, unescapePowerShell(m[1])+"="+unescapePowerShell(m[2]))
		}
		if len(parts) == 0 {
			return "", formatPowerShell, &cookieImportError{Stage: stageExtract, Format: formatPowerShell, Msg: "no System.Net.Cookie entries could be read; use Copy as cURL (bash) instead"}
		}
		return strings.Join(parts, "; "), formatPowerShell, nil
	}

	lines := strings.Split(text, "\n")
	if len(lines) > 1 {
		var parts []string
		for _, line := range lines {
			if v, ok := cutHeaderPrefix(strings.TrimSpace(line), "cookie:"); ok && v != "" {
				parts = append(parts, v)
			}
		}
		if len(parts) == 0 {
			return "", formatUnknown, &cookieImportError{Stage: stageDetect, Format: formatUnknown, Msg: fmt.Sprintf("%d lines of text but no cURL command and no \"cookie:\" line; use Copy > Copy as cURL (bash)", len(lines))}
		}
		return strings.Join(parts, "; "), formatReqHeaders, nil
	}
	if v, ok := cutHeaderPrefix(text, "cookie:"); ok {
		return v, formatCookieHeader, nil
	}
	if strings.Contains(text, "=") {
		return text, formatCookieHeader, nil
	}
	return "", formatUnknown, &cookieImportError{Stage: stageDetect, Format: formatUnknown, Msg: "input is not a Cookie header, request headers, or a cURL command"}
}

func cutHeaderPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return strings.TrimSpace(s[len(prefix):]), true
	}
	return "", false
}

func cookieFromCurlWords(args []string, format string) (string, string, error) {
	var parts []string
	sawB, sawH := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		var v string
		switch {
		case a == "-b" || a == "--cookie":
			v, sawB = next(), true
		case strings.HasPrefix(a, "--cookie="):
			v, sawB = strings.TrimPrefix(a, "--cookie="), true
		case strings.HasPrefix(a, "-b") && len(a) > 2 && !strings.HasPrefix(a, "--"):
			v, sawB = a[2:], true
		case a == "-H" || a == "--header":
			if hv, ok := cutHeaderPrefix(strings.TrimSpace(next()), "cookie:"); ok {
				v, sawH = hv, true
			}
		case strings.HasPrefix(a, "--header="):
			if hv, ok := cutHeaderPrefix(strings.TrimSpace(strings.TrimPrefix(a, "--header=")), "cookie:"); ok {
				v, sawH = hv, true
			}
		}
		if v = strings.TrimSpace(v); v != "" {
			parts = append(parts, v)
		}
	}
	if format == "" {
		switch {
		case sawB && sawH:
			format = formatCurlBH
		case sawB:
			format = formatCurlB
		case sawH:
			format = formatCurlH
		default:
			format = formatCurlNoCookie
		}
	}
	if len(parts) == 0 {
		return "", format, &cookieImportError{Stage: stageExtract, Format: format, Msg: "cURL command has no cookie (no -b/--cookie and no -H 'cookie: ...'); copy a sameday.costco.com graphql request that returned 200 with Copy as cURL (bash). " + safariCookieHint}
	}
	header := strings.Join(parts, "; ")
	if !strings.Contains(header, "=") {
		return "", format, &cookieImportError{Stage: stageExtract, Format: format, Msg: "cURL -b points at a cookie-jar file name, not cookie pairs"}
	}
	return header, format, nil
}

// unescapeCmd converts Chrome's "Copy as cURL (cmd)" caret escaping into
// plain double-quoted words.
func unescapeCmd(s string) string {
	s = strings.ReplaceAll(s, "^\n", " ")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '^' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func unescapePowerShell(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '`' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitShellWords splits bash/zsh words: single quotes, double quotes,
// ANSI-C $'...' quotes (Chrome uses them when a value needs escaping),
// backslash escapes and backslash-newline continuations.
func splitShellWords(s string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && s[i+1] == '\n':
			i++
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			i = readANSICQuoted(s, i+2, &cur)
			inWord = true
		case c == '\'':
			i++
			for ; i < len(s) && s[i] != '\''; i++ {
				cur.WriteByte(s[i])
			}
			inWord = true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`\n", s[i+1]) >= 0 {
					i++
					if s[i] == '\n' {
						continue
					}
				}
				cur.WriteByte(s[i])
			}
			inWord = true
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// readANSICQuoted decodes a $'...' body starting at i and returns the index of
// the closing quote.
func readANSICQuoted(s string, i int, cur *strings.Builder) int {
	hexVal := func(start, max int) (rune, int) {
		end := start
		for end < len(s) && end-start < max && strings.IndexByte("0123456789abcdefABCDEF", s[end]) >= 0 {
			end++
		}
		if end == start {
			return -1, start
		}
		v, _ := strconv.ParseUint(s[start:end], 16, 32)
		return rune(v), end
	}
	for ; i < len(s) && s[i] != '\''; i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			cur.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			cur.WriteByte('\n')
		case 't':
			cur.WriteByte('\t')
		case 'r':
			cur.WriteByte('\r')
		case 'x':
			if r, end := hexVal(i+1, 2); r >= 0 {
				cur.WriteByte(byte(r))
				i = end - 1
			} else {
				cur.WriteByte('x')
			}
		case 'u', 'U':
			max := 4
			if s[i] == 'U' {
				max = 8
			}
			if r, end := hexVal(i+1, max); r >= 0 {
				cur.WriteRune(r)
				i = end - 1
			} else {
				cur.WriteByte(s[i])
			}
		default:
			cur.WriteByte(s[i])
		}
	}
	return i
}

// cookieDiagnosis is the names-only report for `auth login --diagnose`.
type cookieDiagnosis struct {
	InputBytes  int
	Format      string
	Stage       string
	Names       []string
	Dropped     []string
	HasSession  bool
	FailMessage string
}

// diagnoseCookieInput runs the import pipeline without saving anything.
func diagnoseCookieInput(data []byte, domain string) cookieDiagnosis {
	d := cookieDiagnosis{InputBytes: len(data), Format: formatUnknown, Stage: stageRead}
	imported, err := parseCookiesData(data, domain)
	if err != nil {
		if ie, ok := err.(*cookieImportError); ok {
			d.Stage, d.FailMessage = ie.Stage, ie.Msg
			if ie.Format != "" {
				d.Format = ie.Format
			}
		} else {
			d.Stage, d.FailMessage = stageExtract, "could not read cookies from the input"
		}
		return d
	}
	d.Format = imported.Format
	d.Stage = stageParsePairs
	kept, dropped, err := selectSessionCookies(imported.Header)
	d.Dropped = dropped
	for _, part := range strings.Split(imported.Header, ";") {
		name, _, ok := strings.Cut(strings.TrimSpace(part), "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		if name == sessionCookieName {
			d.HasSession = true
		}
		d.Names = appendUnique(d.Names, name)
	}
	if err != nil {
		if ie, ok := err.(*cookieImportError); ok {
			d.Stage, d.FailMessage = ie.Stage, ie.Msg
		}
		return d
	}
	_ = kept
	d.Stage = stageOK
	return d
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// redactCookieName masks long digit or hex runs that some sites embed in
// cookie names (user or org identifiers) so even names stay non-identifying.
func redactCookieName(name string) string {
	name = longHexRE.ReplaceAllString(name, "<id>")
	return longDigitsRE.ReplaceAllString(name, "<id>")
}

func writeCookieDiagnosis(w io.Writer, d cookieDiagnosis) {
	fmt.Fprintln(w, "Cookie import diagnosis (nothing saved; cookie values are never shown)")
	fmt.Fprintf(w, "  input_bytes:    %d\n", d.InputBytes)
	fmt.Fprintf(w, "  format:         %s\n", d.Format)
	fmt.Fprintf(w, "  stage_reached:  %s\n", d.Stage)
	names := make([]string, 0, len(d.Names))
	for _, n := range d.Names {
		names = append(names, redactCookieName(n))
	}
	fmt.Fprintf(w, "  cookie_names:   %d", len(names))
	if len(names) > 0 {
		fmt.Fprintf(w, " (%s)", strings.Join(names, ", "))
	}
	fmt.Fprintln(w)
	if len(d.Dropped) > 0 {
		dropped := make([]string, 0, len(d.Dropped))
		for _, n := range d.Dropped {
			dropped = append(dropped, redactCookieName(n))
		}
		fmt.Fprintf(w, "  malformed:      %s\n", strings.Join(dropped, ", "))
	}
	present := "absent"
	if d.HasSession {
		present = "present"
	}
	fmt.Fprintf(w, "  %s: %s\n", sessionCookieName, present)
	if d.Stage == stageOK {
		fmt.Fprintln(w, "  result:         OK (auth login without --diagnose would import this)")
	} else {
		fmt.Fprintf(w, "  result:         FAIL at %s: %s\n", d.Stage, d.FailMessage)
	}
}
