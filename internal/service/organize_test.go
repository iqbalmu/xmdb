package service

import (
	"testing"

	"github.com/iqbalmu/xmdb/internal/state"
)

func TestMoveDatabase(t *testing.T) {
	stateMgr, err := state.NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	orig := &state.Database{
		Name:          "appdb",
		Provider:      "postgres",
		Version:       "16",
		ContainerName: "xmdb_appdb",
		Port:          5432,
		DatabaseName:  "appdb",
	}
	if err := stateMgr.Save(orig); err != nil {
		t.Fatal(err)
	}

	moved, err := MoveDatabase(stateMgr, "appdb", "work")
	if err != nil {
		t.Fatalf("MoveDatabase(work) failed: %v", err)
	}
	if moved.Group != "work" {
		t.Errorf("Group = %q, want work", moved.Group)
	}

	// Moving to "global" clears Group.
	global, err := MoveDatabase(stateMgr, "appdb", "global")
	if err != nil {
		t.Fatalf("MoveDatabase(global) failed: %v", err)
	}
	if global.Group != "" {
		t.Errorf("Group = %q, want empty", global.Group)
	}

	if _, err := MoveDatabase(stateMgr, "missing", "work"); err == nil {
		t.Errorf("expected error moving nonexistent database")
	}
}
