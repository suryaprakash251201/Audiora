package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationCreatedTriggers lists what the migrator actually built, so a
// failure in trigger creation is visible rather than showing up much later as
// a search that silently returns nothing.
func TestMigrationCreatedTriggers(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	rows, err := d.Query(`SELECT type, name, COALESCE(sql, '') FROM sqlite_master
		WHERE type IN ('table', 'trigger', 'view') ORDER BY type, name`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	found := map[string]string{}
	for rows.Next() {
		var typ, name, sql string
		if err := rows.Scan(&typ, &name, &sql); err != nil {
			t.Fatal(err)
		}
		found[typ+":"+name] = sql
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	t.Logf("schema objects: %d", len(found))
	for k, v := range found {
		if strings.HasPrefix(k, "trigger:") {
			// Printed in full: a trigger whose BEGIN...END body was truncated
			// by the migrator's statement splitting still exists in
			// sqlite_master, but is silently inert.
			t.Logf("  %s\n%s\n---", k, v)
		}
	}

	// The FTS index depends on these three triggers. If any is missing, every
	// scan leaves the index empty and search returns nothing at all.
	for _, want := range []string{
		"trigger:tracks_fts_insert",
		"trigger:tracks_fts_delete",
		"trigger:tracks_fts_update",
	} {
		if _, ok := found[want]; !ok {
			t.Errorf("missing %s; search will return no results for any track", want)
		}
	}
}
