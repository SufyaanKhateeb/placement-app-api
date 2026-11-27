package main

import "testing"

func TestMigrationDatabaseTarget(t *testing.T) {
	const local = "postgres://localhost/placement_dev"
	const managed = "postgres://production.example.invalid/production"
	t.Setenv("LOCAL_DATABASE_URL", local)
	t.Setenv("MIGRATION_DATABASE_URL", managed)
	if got, err := migrationDatabaseURL(false); err != nil || got != local {
		t.Fatal("the default migration must use only the local database")
	}
	if got, err := migrationDatabaseURL(true); err != nil || got != managed {
		t.Fatal("the explicit managed migration must use MIGRATION_DATABASE_URL")
	}
	t.Setenv("MIGRATION_DATABASE_URL", "")
	if _, err := migrationDatabaseURL(true); err == nil {
		t.Fatal("a managed migration must require its own database URL")
	}
}
