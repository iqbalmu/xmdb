package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

func TestManifestLoadSaveAndResolveNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultManifestFile)

	volFalse := false
	m := &Manifest{
		Version: 1,
		Project: "my-shop",
		EnvFile: ".env.local",
		Databases: map[string]DatabaseSpec{
			"primary": {
				Provider: "postgres",
				Version:  "16",
				Port:     5432,
				Env: map[string]string{
					"DATABASE_URL": "{{url}}",
					"DB_PORT":      "{{port}}",
				},
			},
			"analytics": {
				Name:     "custom_analytics",
				Provider: "mysql",
				Version:  "8.0",
				Volume:   &volFalse,
			},
		},
	}

	if err := m.Save(path); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, absPath, err := Load(path)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if absPath != path {
		t.Errorf("absPath = %q, want %q", absPath, path)
	}
	if got := loaded.ResolveInstanceName("primary"); got != "my-shop_primary" {
		t.Errorf("primary instance name = %q, want my-shop_primary", got)
	}
	if got := loaded.ResolveInstanceName("analytics"); got != "custom_analytics" {
		t.Errorf("analytics instance name = %q, want custom_analytics", got)
	}
	if !loaded.Databases["primary"].VolumeEnabled() {
		t.Errorf("primary VolumeEnabled() = false, want true by default")
	}
	if loaded.Databases["analytics"].VolumeEnabled() {
		t.Errorf("analytics VolumeEnabled() = true, want false")
	}

	// FindManifest walks up from nested subdirectories.
	nested := filepath.Join(dir, "apps", "api")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := FindManifest(nested)
	if err != nil {
		t.Fatalf("FindManifest() failed: %v", err)
	}
	if found != path {
		t.Errorf("FindManifest() = %q, want %q", found, path)
	}
}

func TestManifestValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		m    Manifest
	}{
		{
			name: "missing project",
			m: Manifest{
				Version:   1,
				Databases: map[string]DatabaseSpec{"db": {Provider: "postgres"}},
			},
		},
		{
			name: "unsupported provider",
			m: Manifest{
				Version:   1,
				Project:   "app",
				Databases: map[string]DatabaseSpec{"db": {Provider: "oracle"}},
			},
		},
		{
			name: "duplicate resolved instance names",
			m: Manifest{
				Version: 1,
				Project: "app",
				Databases: map[string]DatabaseSpec{
					"a": {Name: "shared", Provider: "postgres"},
					"b": {Name: "shared", Provider: "mysql"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.m.Validate(); err == nil {
				t.Errorf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestBuildDatabaseEnv(t *testing.T) {
	prov := providers.NewPostgresProvider()
	db := &state.Database{
		Name:         "shop_db",
		Provider:     "postgres",
		Port:         55432,
		RootPassword: "root",
		DatabaseName: "shop_db",
		User:         "dev_user",
		UserPassword: "pw",
	}

	// Custom template mapping
	spec := DatabaseSpec{
		Provider: "postgres",
		Env: map[string]string{
			"DATABASE_URL": "{{url}}",
			"CUSTOM":       "{{host}}:{{port}}/{{database}}",
		},
	}
	got := BuildDatabaseEnv("db", spec, prov, db, true)
	if !strings.HasPrefix(got["DATABASE_URL"], "postgres://dev_user:pw@localhost:55432/shop_db") {
		t.Errorf("unexpected DATABASE_URL: %q", got["DATABASE_URL"])
	}
	if got["CUSTOM"] != "localhost:55432/shop_db" {
		t.Errorf("unexpected CUSTOM: %q", got["CUSTOM"])
	}

	// Multi-DB fallback prefixes keys to prevent collisions
	multi := BuildDatabaseEnv("analytics-db", DatabaseSpec{Provider: "postgres"}, prov, db, true)
	if multi["ANALYTICS_DB_DATABASE_URL"] == "" || multi["ANALYTICS_DB_DB_PORT"] != "55432" {
		t.Errorf("expected prefixed keys for multi-db default env, got %+v", multi)
	}
}

func TestUpsertManagedBlockAndSyncEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// Start with existing user secrets
	initial := "APP_SECRET=keep-me\n# custom comment\nPORT=3000\n"
	if err := os.WriteFile(envPath, []byte(initial), 0o640); err != nil {
		t.Fatal(err)
	}

	vars1 := map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost:5432/app?sslmode=disable",
		"DB_PORT":      "5432",
	}
	if _, err := SyncEnvFile(envPath, vars1); err != nil {
		t.Fatalf("first SyncEnvFile() failed: %v", err)
	}

	after1, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	s1 := string(after1)
	if !strings.Contains(s1, "APP_SECRET=keep-me") || !strings.Contains(s1, "PORT=3000") {
		t.Fatalf("user variables lost:\n%s", s1)
	}
	if !strings.Contains(s1, "DB_PORT=5432") {
		t.Fatalf("managed block missing:\n%s", s1)
	}

	// Second sync updates ONLY the managed block and keeps user lines intact.
	vars2 := map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost:55432/app?sslmode=disable",
		"DB_PORT":      "55432",
		"NOTE":         "hello world",
	}
	if _, err := SyncEnvFile(envPath, vars2); err != nil {
		t.Fatalf("second SyncEnvFile() failed: %v", err)
	}

	after2, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	s2 := string(after2)
	if strings.Count(s2, EnvBlockStartPrefix) != 1 || strings.Count(s2, EnvBlockEndLine) != 1 {
		t.Fatalf("expected exactly one managed block, got:\n%s", s2)
	}
	if !strings.Contains(s2, "APP_SECRET=keep-me") || !strings.Contains(s2, "DB_PORT=55432") || !strings.Contains(s2, `NOTE="hello world"`) {
		t.Fatalf("unexpected synced contents:\n%s", s2)
	}
	if strings.Contains(s2, "DB_PORT=5432\n") {
		t.Fatalf("stale port not replaced:\n%s", s2)
	}

	// Malformed file with unclosed start marker must error cleanly instead of corrupting.
	if _, err := UpsertManagedBlock("FOO=1\n# xmdb:start\nBAR=2\n", vars2); err == nil {
		t.Errorf("expected error for unclosed managed block")
	}
}
