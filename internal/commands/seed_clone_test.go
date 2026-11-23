package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

func TestCollectSeedFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	sqlFile := filepath.Join(dir, "init.sql")
	if err := os.WriteFile(sqlFile, []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := collectSeedFiles(sqlFile)
	if err != nil {
		t.Fatalf("collectSeedFiles() error = %v", err)
	}
	if len(files) != 1 || files[0] != sqlFile {
		t.Fatalf("got %v, want [%s]", files, sqlFile)
	}
}

func TestCollectSeedFiles_DirectorySorted(t *testing.T) {
	dir := t.TempDir()
	names := []string{"010_seed.sql", "001_init.sql", "002_users.SQL", "README.md"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("-- "+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested.sql"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := collectSeedFiles(dir)
	if err != nil {
		t.Fatalf("collectSeedFiles() error = %v", err)
	}
	want := []string{
		filepath.Join(dir, "001_init.sql"),
		filepath.Join(dir, "002_users.SQL"),
		filepath.Join(dir, "010_seed.sql"),
	}
	if len(files) != len(want) {
		t.Fatalf("got %d files (%v), want %d (%v)", len(files), files, len(want), want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Errorf("files[%d] = %s, want %s", i, files[i], want[i])
		}
	}
}

func TestCollectSeedFiles_Errors(t *testing.T) {
	emptyDir := t.TempDir()

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "empty path", path: "   ", want: "seed path is empty"},
		{name: "missing path", path: filepath.Join(emptyDir, "missing.sql"), want: "seed path not found"},
		{name: "empty dir", path: emptyDir, want: "no .sql files found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := collectSeedFiles(tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("collectSeedFiles(%q) error = %v, want substring %q", tt.path, err, tt.want)
			}
		})
	}
}

func TestCloneDatabase_Validation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("state.NewManager() error = %v", err)
	}

	src := &state.Database{
		Name:         "srcdb",
		Provider:     utils.ProviderPostgres,
		Version:      "16",
		Port:         5432,
		DatabaseName: "srcdb",
		CreatedAt:    time.Now(),
	}
	if err := mgr.Save(src); err != nil {
		t.Fatal(err)
	}

	if _, _, err := cloneDatabase(context.Background(), mgr, cloneOptions{
		SourceName: "missing",
		TargetName: "targetdb",
	}, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}

	if _, _, err := cloneDatabase(context.Background(), mgr, cloneOptions{
		SourceName: "srcdb",
		TargetName: "srcdb",
	}, nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already exists error, got %v", err)
	}

	if _, _, err := cloneDatabase(context.Background(), mgr, cloneOptions{
		SourceName: "srcdb",
		TargetName: "-bad-name",
	}, nil); err == nil {
		t.Fatal("expected invalid target name error, got nil")
	}
}

// TUI createModel & cloneModal tests live in internal/tui/tui_test.go.
