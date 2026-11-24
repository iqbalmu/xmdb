package service

import (
	"os"
	"testing"
	"time"
)

func TestSnapshotManagerLifecycleAndPruning(t *testing.T) {
	mgr, err := NewSnapshotManagerAt(t.TempDir())
	if err != nil {
		t.Fatalf("NewSnapshotManagerAt() failed: %v", err)
	}

	base := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

	// Save 2 manual snapshots and 7 auto snapshots.
	manual1 := &SnapshotMeta{
		ID: "20261124-100000-aa0001", Name: "before-mig", Database: "appdb",
		Provider: "postgres", Format: "pgdump", FileName: "data.pgdump",
		SizeBytes: 128, Auto: false, CreatedAt: base,
	}
	manual2 := &SnapshotMeta{
		ID: "20261124-100500-aa0002", Name: "v2-clean", Database: "appdb",
		Provider: "postgres", Format: "pgdump", FileName: "data.pgdump",
		SizeBytes: 256, Auto: false, CreatedAt: base.Add(5 * time.Minute),
	}
	for _, m := range []*SnapshotMeta{manual1, manual2} {
		if err := mgr.SaveMeta(m); err != nil {
			t.Fatalf("SaveMeta(%s) failed: %v", m.ID, err)
		}
		if err := os.WriteFile(mgr.DataPath(m), []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 7; i++ {
		m := &SnapshotMeta{
			ID:        generateSnapshotID(base.Add(time.Duration(i+1) * time.Minute)),
			Name:      "auto-pre-reset-" + string(rune('a'+i)),
			Database:  "appdb",
			Provider:  "postgres",
			Format:    "pgdump",
			FileName:  "data.pgdump",
			Auto:      true,
			CreatedAt: base.Add(time.Duration(10+i) * time.Minute),
		}
		if err := mgr.SaveMeta(m); err != nil {
			t.Fatalf("SaveMeta auto %d failed: %v", i, err)
		}
	}

	removed, err := mgr.PruneAuto("appdb", DefaultMaxAutoSnapshots)
	if err != nil {
		t.Fatalf("PruneAuto() failed: %v", err)
	}
	if removed != 2 {
		t.Errorf("PruneAuto() removed = %d, want 2", removed)
	}

	snaps, err := mgr.List("appdb")
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	// 2 manual + 5 remaining auto = 7
	if len(snaps) != 7 {
		t.Fatalf("List() count = %d, want 7", len(snaps))
	}
	if !snaps[0].CreatedAt.After(snaps[len(snaps)-1].CreatedAt) {
		t.Errorf("expected newest-first ordering")
	}

	// Find by Name and by ID
	if found, err := mgr.Find("appdb", "before-mig"); err != nil || found.ID != manual1.ID {
		t.Errorf("Find by name failed: %v, %+v", err, found)
	}
	if found, err := mgr.Find("appdb", manual2.ID); err != nil || found.Name != "v2-clean" {
		t.Errorf("Find by ID failed: %v, %+v", err, found)
	}

	// RenameDatabaseDir updates directory and metadata database field
	if err := mgr.RenameDatabaseDir("appdb", "shopdb"); err != nil {
		t.Fatalf("RenameDatabaseDir() failed: %v", err)
	}
	renamed, err := mgr.List("shopdb")
	if err != nil || len(renamed) != 7 {
		t.Fatalf("List(shopdb) = %d, err=%v", len(renamed), err)
	}
	for _, s := range renamed {
		if s.Database != "shopdb" {
			t.Errorf("renamed snapshot still has Database=%q", s.Database)
		}
	}

	// Delete snapshot
	if _, err := mgr.Delete("shopdb", "before-mig"); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
	if _, err := mgr.Find("shopdb", "before-mig"); err == nil {
		t.Errorf("expected deleted snapshot not to be found")
	}
}

func TestValidateSnapshotName(t *testing.T) {
	if err := ValidateSnapshotName("pre-mig_v1.2"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	for _, bad := range []string{"", "../escape", "has space", "-leading-dash"} {
		if err := ValidateSnapshotName(bad); err == nil {
			t.Errorf("expected error for invalid snapshot name %q", bad)
		}
	}
}
