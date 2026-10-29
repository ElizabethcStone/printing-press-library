// Copyright 2026 Som Samantray and contributors. Licensed under Apache-2.0. See LICENSE.

package addrkit

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Table is a header plus string rows, read from CSV or JSONL. Every row has
// exactly len(Header) cells.
type Table struct {
	Header []string
	Rows   [][]string
}

// Col returns the index of the first header equal (case-insensitively, after
// trimming) to one of names, or -1.
func (t *Table) Col(names ...string) int {
	for _, name := range names {
		want := strings.ToLower(strings.TrimSpace(name))
		for i, h := range t.Header {
			if strings.ToLower(strings.TrimSpace(h)) == want {
				return i
			}
		}
	}
	return -1
}

// Cell returns row[col], or "" when col is out of range.
func Cell(row []string, col int) string {
	if col < 0 || col >= len(row) {
		return ""
	}
	return row[col]
}

// Input limits applied by ReadTable.
const (
	// MaxFileBytes is the largest input file ReadTable will load.
	MaxFileBytes = 64 << 20
	// HardMaxRows is the most rows any caller may ask for.
	HardMaxRows = 200000
)

// ErrLimit marks an input that is refused for size, row count or file type.
var ErrLimit = errors.New("input limit exceeded")

// ReadTable reads path with the hard row limit. See ReadTableMax.
func ReadTable(path string) (*Table, error) {
	return ReadTableMax(path, HardMaxRows)
}

// ReadTableMax reads path as JSON Lines when its extension is .jsonl or
// .ndjson, as JSON Lines or a JSON array of objects when it is .json, and as
// CSV otherwise. A UTF-8 byte-order mark is stripped. It refuses anything that
// is not a regular file (FIFOs, devices), files over MaxFileBytes, and inputs
// with more than maxRows rows (capped at HardMaxRows); those errors wrap
// ErrLimit.
func ReadTableMax(path string, maxRows int) (*Table, error) {
	if maxRows < 1 || maxRows > HardMaxRows {
		return nil, fmt.Errorf("%w: max rows must be between 1 and %d", ErrLimit, HardMaxRows)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrLimit, path)
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes, the limit is %d", ErrLimit, path, info.Size(), MaxFileBytes)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- user-specified input table path is the command's documented purpose.
	if err != nil {
		return nil, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var t *Table
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '[' {
			t, err = ParseJSONArrayMax(bytes.NewReader(trimmed), maxRows)
		} else {
			t, err = ParseJSONLMax(bytes.NewReader(data), maxRows)
		}
	case ".jsonl", ".ndjson":
		t, err = ParseJSONLMax(bytes.NewReader(data), maxRows)
	default:
		t, err = ParseCSVMax(bytes.NewReader(data), maxRows)
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ErrNoInput is returned when standard input holds no data.
var ErrNoInput = errors.New("standard input is empty")

// StdinPath is the --file / --orders value that means "read standard input".
const StdinPath = "-"

// ReadTableReader reads a table from r (standard input) under the same size and
// row limits as ReadTableMax. The format is sniffed from the first non-space
// byte: '[' is a JSON array of objects, '{' is JSON Lines, anything else is CSV.
// Empty input returns ErrNoInput; oversized input and row overflow wrap ErrLimit.
func ReadTableReader(r io.Reader, maxRows int) (*Table, error) {
	if maxRows < 1 || maxRows > HardMaxRows {
		return nil, fmt.Errorf("%w: max rows must be between 1 and %d", ErrLimit, HardMaxRows)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading standard input: %w", err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%w: standard input is over %d bytes", ErrLimit, MaxFileBytes)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, ErrNoInput
	}
	switch trimmed[0] {
	case '[':
		return ParseJSONArrayMax(bytes.NewReader(trimmed), maxRows)
	case '{':
		return ParseJSONLMax(bytes.NewReader(trimmed), maxRows)
	default:
		return ParseCSVMax(bytes.NewReader(data), maxRows)
	}
}

func rowLimitErr(maxRows int) error {
	return fmt.Errorf("%w: more than %d rows (the limit is %d; raise --max-rows, up to %d)", ErrLimit, maxRows, maxRows, HardMaxRows)
}

// ParseCSV parses CSV with a header row, without a row limit. Short rows are
// padded and blank lines skipped.
func ParseCSV(r io.Reader) (*Table, error) { return ParseCSVMax(r, 0) }

// ParseCSVMax is ParseCSV that stops reading as soon as it sees more than
// maxRows data rows (maxRows <= 0 means no limit), so a huge file costs only
// the rows up to the limit.
func ParseCSVMax(r io.Reader, maxRows int) (*Table, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	cr.ReuseRecord = false
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("file is empty: expected a header row")
	}
	if err != nil {
		return nil, fmt.Errorf("parsing csv: %w", err)
	}
	t := &Table{Header: header}
	for i := range t.Header {
		t.Header[i] = strings.TrimSpace(t.Header[i])
	}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return t, nil
		}
		if err != nil {
			return nil, fmt.Errorf("parsing csv: %w", err)
		}
		if len(rec) == 1 && strings.TrimSpace(rec[0]) == "" {
			continue
		}
		if maxRows > 0 && len(t.Rows) >= maxRows {
			return nil, rowLimitErr(maxRows)
		}
		row := make([]string, len(t.Header))
		copy(row, rec)
		t.Rows = append(t.Rows, row)
	}
}

// jsonTable accumulates JSON objects into a Table: the header is the union of
// keys in first-seen order and non-string values are rendered as JSON text.
type jsonTable struct {
	t       *Table
	index   map[string]int
	objs    []map[string]json.RawMessage
	maxRows int
}

func newJSONTable(maxRows int) *jsonTable {
	return &jsonTable{t: &Table{}, index: map[string]int{}, maxRows: maxRows}
}

func (b *jsonTable) add(text, where string) error {
	dec := json.NewDecoder(strings.NewReader(text))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return fmt.Errorf("%s: expected a JSON object", where)
	}
	if b.maxRows > 0 && len(b.objs) >= b.maxRows {
		return rowLimitErr(b.maxRows)
	}
	obj := map[string]json.RawMessage{}
	var keys []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		key, _ := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		obj[key] = raw
		keys = append(keys, key)
	}
	b.objs = append(b.objs, obj)
	for _, k := range keys {
		if _, ok := b.index[k]; !ok {
			b.index[k] = len(b.t.Header)
			b.t.Header = append(b.t.Header, k)
		}
	}
	return nil
}

func (b *jsonTable) finish() (*Table, error) {
	if len(b.objs) == 0 {
		return nil, errors.New("file is empty: expected at least one JSON object")
	}
	for _, obj := range b.objs {
		row := make([]string, len(b.t.Header))
		for k, raw := range obj {
			row[b.index[k]] = jsonCell(raw)
		}
		b.t.Rows = append(b.t.Rows, row)
	}
	return b.t, nil
}

// ParseJSONL parses one JSON object per line, without a row limit.
func ParseJSONL(r io.Reader) (*Table, error) { return ParseJSONLMax(r, 0) }

// ParseJSONLMax is ParseJSONL that stops at the first object beyond maxRows
// (maxRows <= 0 means no limit).
func ParseJSONLMax(r io.Reader, maxRows int) (*Table, error) {
	b := newJSONTable(maxRows)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		if err := b.add(text, fmt.Sprintf("line %d", line)); err != nil {
			return nil, err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return b.finish()
}

// ParseJSONArray parses a JSON array of objects, without a row limit.
func ParseJSONArray(r io.Reader) (*Table, error) { return ParseJSONArrayMax(r, 0) }

// ParseJSONArrayMax parses a JSON array of objects element by element and
// stops at the first element beyond maxRows (maxRows <= 0 means no limit).
func ParseJSONArrayMax(r io.Reader, maxRows int) (*Table, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return nil, errors.New("parsing json array: expected '['")
	}
	b := newJSONTable(maxRows)
	for i := 1; dec.More(); i++ {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("parsing json array: %w", err)
		}
		if err := b.add(string(raw), fmt.Sprintf("element %d", i)); err != nil {
			return nil, err
		}
	}
	return b.finish()
}

func jsonCell(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	text := strings.TrimSpace(string(raw))
	if text == "null" {
		return ""
	}
	return text
}

// NeutralizeCell defuses spreadsheet formula injection: a cell that starts
// with = + - @ tab or carriage return gets a leading single quote, unless the
// whole cell is a plain finite number such as -12.5.
func NeutralizeCell(c string) string {
	if c == "" {
		return c
	}
	switch c[0] {
	case '=', '+', '-', '@', '\t', '\r':
		if _, err := ParseFiniteFloat(c); err == nil {
			return c
		}
		return "'" + c
	}
	return c
}

func neutralizeRow(row []string) []string {
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = NeutralizeCell(c)
	}
	return out
}

// ErrExists is returned by WriteCSVMode when overwrite is false and the target
// already exists.
var ErrExists = errors.New("output file already exists")

// WriteCSV writes header and rows to path atomically (temp file + rename),
// replacing any existing file. See WriteCSVMode.
func WriteCSV(path string, header []string, rows [][]string) error {
	return WriteCSVMode(path, header, rows, true)
}

// WriteCSVMode writes header and rows to path with 0600 permissions. Cells that
// could execute as spreadsheet formulas are neutralized (see NeutralizeCell).
// The data goes to a temp file first, which is removed on every failure path.
// With overwrite it is renamed over the target; without it the target is
// published with a hard link, which fails if the path already exists, so a
// file created by someone else after a pre-check is never replaced (ErrExists).
func WriteCSVMode(path string, header []string, rows [][]string, overwrite bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".csv-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	closed, committed := false, false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		// With the link path the temp name is removed even on success.
		if !committed || !overwrite {
			_ = os.Remove(tmpName)
		}
	}()
	w := csv.NewWriter(tmp)
	if err := w.Write(neutralizeRow(header)); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(neutralizeRow(row)); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	closed = true
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if overwrite {
		if err := os.Rename(tmpName, path); err != nil {
			return err
		}
		committed = true
		return nil
	}
	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: %s", ErrExists, path)
		}
		// Some filesystems cannot hard-link: claim the name exclusively, then
		// move the finished file over the placeholder.
		f, oerr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- user-specified output path is the command's documented purpose.
		if oerr != nil {
			if errors.Is(oerr, os.ErrExist) {
				return fmt.Errorf("%w: %s", ErrExists, path)
			}
			return oerr
		}
		_ = f.Close()
		if rerr := os.Rename(tmpName, path); rerr != nil {
			_ = os.Remove(path)
			return rerr
		}
		committed = true
		return nil
	}
	committed = true
	return nil
}

// WriteCSVStream writes header and rows as CSV to w, neutralizing cells that
// could run as spreadsheet formulas (see NeutralizeCell).
func WriteCSVStream(w io.Writer, header []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(neutralizeRow(header)); err != nil {
		return err
	}
	for _, row := range rows {
		if err := cw.Write(neutralizeRow(row)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
