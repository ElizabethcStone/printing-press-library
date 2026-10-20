// Copyright 2026 Mini and contributors. Licensed under Apache-2.0. See LICENSE.

package config

import "testing"

func TestN8NURLAlias(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"https://n8n.example.com", "https://n8n.example.com/api/v1"},
		{"https://n8n.example.com/", "https://n8n.example.com/api/v1"},
		{"https://n8n.example.com/api/v1", "https://n8n.example.com/api/v1"},
		{"https://n8n.example.com/api/v1/", "https://n8n.example.com/api/v1"},
	}
	for _, tc := range tests {
		t.Setenv("N8N_URL", tc.in)
		if got := n8nURLAlias(); got != tc.want {
			t.Errorf("N8N_URL=%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestBaseURLWinsOverAlias(t *testing.T) {
	t.Setenv("N8N_URL", "https://alias.example.com")
	t.Setenv("N8N_BASE_URL", "https://base.example.com/api/v1")
	t.Setenv("N8N_CONFIG", t.TempDir()+"/config.toml")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://base.example.com/api/v1" {
		t.Fatalf("got %q", cfg.BaseURL)
	}
	t.Setenv("N8N_BASE_URL", "")
	cfg, err = Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://alias.example.com/api/v1" {
		t.Fatalf("alias not applied: %q", cfg.BaseURL)
	}
}
