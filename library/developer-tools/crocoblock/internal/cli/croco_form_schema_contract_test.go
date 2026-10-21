package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestCrocoFormSchemaReportsValidationChanges(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"required", `"required":false`, `"required":true`},
		{"field_type", `"field_type":"text"`, `"field_type":"email"`},
		{"minlength", `"minlength":"2"`, `"minlength":4`},
		{"maxlength", `"maxlength":10`, `"maxlength":20`},
		{"numeric_limit", `"max":10`, `"max":20`},
		{"multiple", `"multiple":false`, `"multiple":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := func(attrs string) map[string]any {
				return map[string]any{"id": 42, "status": "draft", "title": map[string]any{"raw": "Fixture"}, "content": map[string]any{"raw": `<!-- wp:jet-forms/text-field {"name":"contact","label":"Contact",` + attrs + `} /-->`}}
			}
			before, err := crocoSchemaRow("forms", row(tc.before))
			if err != nil {
				t.Fatal(err)
			}
			after, err := crocoSchemaRow("forms", row(tc.after))
			if err != nil {
				t.Fatal(err)
			}
			changes := []any{}
			crocoSchemaChanges("forms", "42", "", before, after, &changes)
			if len(changes) != 1 {
				t.Fatalf("expected field-definition difference, got %#v", changes)
			}
			if changes[0].(map[string]any)["path"] != "/fields" {
				t.Fatal(changes)
			}
		})
	}
}

func TestCrocoFormSchemaDefaultsAndPrivacy(t *testing.T) {
	base, err := crocoParseFormFields(`<!-- wp:jet-forms/text-field {"name":"reference"} /-->`)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := crocoParseFormFields(`<!-- wp:jet-forms/text-field {"name":"reference","required":false,"field_type":"text","default":"PRIVATE_DEFAULT","value":{"groups":["PRIVATE_VALUE"]},"field_options":[{"value":"PRIVATE_OPTION"}],"validation":{"messages":{"empty":"PRIVATE_MESSAGE"},"rules":[{"type":"equal","value":"PRIVATE_RULE"}]},"webhook":"PRIVATE_WEBHOOK","minlength":"PRIVATE_DYNAMIC_PRESET"} /-->`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base, explicit) {
		t.Fatalf("defaults or private configuration polluted schema: %#v", explicit)
	}
	encoded, err := json.Marshal(explicit)
	if err != nil || strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatalf("private values leaked: %s (%v)", encoded, err)
	}
}

func TestCrocoFormSchemaPublishingKeepsRawStructure(t *testing.T) {
	status, nativeCalls := "draft", 0
	raw := `<!-- wp:jet-forms/text-field {"name":"contact","field_type":"email","required":true,"minlength":"2"} /-->`
	c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		if strings.HasSuffix(r.URL.Path, "/fields") {
			nativeCalls++
			fmt.Fprint(w, `{"fields":[{"value":"contact","label":"Native label","type":"text"}]}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": 42, "status": status, "content": map[string]any{"raw": raw}})
	})
	draft, source, err := crocoFormFields(context.Background(), c, "42")
	if err != nil || source != "gutenberg_raw" || nativeCalls != 0 {
		t.Fatalf("draft: %#v %s %v", draft, source, err)
	}
	status = "publish"
	published, source, err := crocoFormFields(context.Background(), c, "42")
	if err != nil || source != "gutenberg_raw_native_verified" || nativeCalls != 1 {
		t.Fatalf("published: %#v %s %v", published, source, err)
	}
	if !reflect.DeepEqual(draft, published) {
		t.Fatalf("publication changed field schema: %#v -> %#v", draft, published)
	}
	field := published[0].(map[string]any)
	if field["required"] != true || field["field_type"] != "email" || field["type"] != "text-field" {
		t.Fatal(field)
	}
}

func TestCrocoFormPublishedSchemaStillValidatesNativeRoute(t *testing.T) {
	for _, reply := range []string{`null`, `{}`, `{"fields":null}`, `{"fields":{}}`, `[{"type":"text"}]`, `[{"value":"reference"}]`} {
		t.Run(reply, func(t *testing.T) {
			c := crocoFormsTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/fields") {
					fmt.Fprint(w, reply)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"id": 42, "status": "publish", "content": map[string]any{"raw": `<!-- wp:jet-forms/text-field {"name":"reference"} /-->`}})
			})
			if _, _, err := crocoFormFields(context.Background(), c, "42"); err == nil {
				t.Fatal("invalid native field response accepted")
			}
		})
	}
}

func TestCrocoReservationRequiresExplicitCollection(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		valid       bool
	}{
		{"empty", `{"success":true,"data":[],"total":0}`, true},
		{"missing", `{"success":true,"total":0}`, false},
		{"null", `{"success":true,"data":null,"total":0}`, false},
		{"object", `{"success":true,"data":{},"total":0}`, false},
	} {
		for _, booking := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/booking=%t", tc.name, booking), func(t *testing.T) {
				c := crocoReservationTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.reply) })
				rows, err := crocoReservationAll(context.Background(), c, booking, map[string]any{})
				if (err == nil) != tc.valid {
					t.Fatalf("rows=%#v err=%v", rows, err)
				}
				if tc.valid && len(rows) != 0 {
					t.Fatal(rows)
				}
			})
		}
	}
}
