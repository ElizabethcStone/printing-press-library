// Hand-written addition: tests for enti istat CKAN query — preserve on regeneration.

package cli

import "testing"

func TestEntiIstatParams(t *testing.T) {
	t.Run("--codice queries the Enti dataset, not the AOO one", func(t *testing.T) {
		p, err := entiIstatParams("oam_2", "")
		if err != nil {
			t.Fatal(err)
		}
		if p["resource_id"] != ckanEntiResourceID {
			t.Errorf("resource_id = %q, want Enti dataset %q", p["resource_id"], ckanEntiResourceID)
		}
		if p["filters"] != `{"Codice_IPA":"oam_2"}` {
			t.Errorf("filters = %q", p["filters"])
		}
		if p["limit"] != "1" {
			t.Errorf("limit = %q, want 1", p["limit"])
		}
	})

	t.Run("--istat filters entities by comune", func(t *testing.T) {
		p, err := entiIstatParams("", "082053")
		if err != nil {
			t.Fatal(err)
		}
		if p["resource_id"] != ckanEntiResourceID || p["filters"] != `{"Codice_comune_ISTAT":"082053"}` || p["limit"] != "500" {
			t.Errorf("params = %v", p)
		}
	})

	t.Run("entity without AOO resolves its seat code", func(t *testing.T) {
		// Enti dataset row for oam_2 (0 AOOs), trimmed from the live response of 2026-10-16.
		raw := []byte(`{"success":true,"result":{"total":1,"records":[{"Codice_IPA":"oam_2","Denominazione_ente":"Ordine Avvocati di Mondovi'","Codice_comune_ISTAT":"004130"}]}}`)
		records, _, err := parseCKANDatastore(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 1 || records[0]["Codice_comune_ISTAT"] != "004130" {
			t.Errorf("records = %v", records)
		}
	})
}
