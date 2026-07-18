package main

import (
	"testing"

	"github.com/lasseh/whynoipv6/internal/api"
	"github.com/lasseh/whynoipv6/internal/config"
)

// TestConfigBinding drives the API's options binding through the real
// registry — see cmd/crawler's twin for the rationale.
func TestConfigBinding(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u@localhost/whynoipv6")
	cfg, err := config.Load("api")
	if err != nil {
		t.Fatal(err)
	}
	_ = api.OptionsFrom(cfg)
}
