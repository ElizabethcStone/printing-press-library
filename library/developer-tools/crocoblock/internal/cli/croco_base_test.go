package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCrocoBodyRejectsAmbiguity(t *testing.T) {
	for _, input := range []string{`[]`, `null`, `{"title":"x"} {"y":1}`, `{"bad":`} {
		if _, err := crocoBody(input, ""); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	m, e := crocoBody(`{"id":9007199254740993}`, "")
	if e != nil || m["id"] != json.Number("9007199254740993") {
		t.Fatal(m, e)
	}
}
func TestCrocoSecretRedaction(t *testing.T) {
	v := crocoSafe(map[string]any{"title": "Room", "nested": []any{map[string]any{"api_key": "never-print", "_jf_actions": "never-actions", "status": "draft"}}})
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "never-") || !strings.Contains(string(b), "Room") {
		t.Fatal(string(b))
	}
}
func TestCrocoIdentifierValidation(t *testing.T) {
	for _, v := range []string{"../users", "a/b", "https://bad", "a?context=edit", ""} {
		if _, e := crocoSlug(v); e == nil {
			t.Errorf("accepted %q", v)
		}
	}
	for _, v := range []string{"-1", "0", "1e2", "1/2"} {
		if _, e := crocoID([]string{v}, 0); e == nil {
			t.Errorf("accepted %q", v)
		}
	}
}
