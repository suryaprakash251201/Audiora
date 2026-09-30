package db

import (
	"path/filepath"
	"testing"
)

func TestMigrationsApplyAndDetectFTS(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()

	// Every migration should have registered itself.
	var applied int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied < 1 {
		t.Fatalf("expected at least one applied migration, got %d", applied)
	}

	// FTS5 is an optional SQLite build feature, so its presence is an
	// environment fact rather than a correctness requirement. Log which way
	// it went so a CI failure is legible.
	t.Logf("FTS5 available: %v", d.FTS)
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	first, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	first.Close()

	// Reopening must not re-apply anything, or the CREATE TABLE statements
	// would fail on every restart.
	second, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	second.Close()
}

func TestFTSQueryEscapesUserInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"single term gets a prefix match", "bach", `"bach"*`},
		{"multi term only prefixes the last", "gold metal", `"gold" "metal"*`},
		{"quotes are escaped not dropped", `say "hi"`, `"say" """hi"""*`},
		{"hyphen is literal", "blue-note", `"blue-note"*`},
		{"colon is literal", "ft:8080", `"ft:8080"*`},
		{"empty input matches nothing", "", `""`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ftsQuery(tc.input); got != tc.want {
				t.Errorf("ftsQuery(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
