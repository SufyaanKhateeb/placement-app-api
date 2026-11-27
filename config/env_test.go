package config

import "testing"

func TestLocalDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user@production.example.invalid/production")
	t.Setenv("DATABASE_URL_UNPOOLED", "postgres://user@production.example.invalid/production")
	t.Setenv("MIGRATION_DATABASE_URL", "postgres://user@production.example.invalid/production")
	t.Setenv("LOCAL_DATABASE_URL", "")
	if got, err := LocalDatabaseURL(); err != nil || got != defaultLocalDatabaseURL {
		t.Fatalf("local default was not selected: %v", err)
	}

	for _, tc := range []struct {
		name, url string
		valid     bool
	}{
		{"IPv4", "postgres://postgres@127.0.0.1:55518/development?sslmode=disable", true},
		{"IPv6", "postgresql://postgres@[::1]:5432/development", true},
		{"loopback host", "postgres://postgres@localhost/development", true},
		{"remote host", "postgres://postgres@production.example.invalid/production", false},
		{"host override", "postgres://localhost/development?host=production.example.invalid", false},
		{"service override", "postgres://localhost/development?service=production", false},
		{"multiple hosts", "postgres://127.0.0.1,production.example.invalid/development", false},
		{"no database", "postgres://localhost", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LOCAL_DATABASE_URL", tc.url)
			if _, err := LocalDatabaseURL(); (err == nil) != tc.valid {
				t.Fatalf("unexpected local database validation result: %v", err)
			}
		})
	}
}
