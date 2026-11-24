package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/state"
)

// RenameDatabase renames a managed database state entry, its container (if
// present), and its snapshot directory while preserving the underlying data
// volume and logical database name.
func RenameDatabase(ctx context.Context, runtimeMode string, stateMgr *state.Manager, oldName, newName string) (*state.Database, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == newName {
		return nil, fmt.Errorf("new name must be different from '%s'", oldName)
	}
	if err := DBNameValidator(stateMgr)(newName); err != nil {
		return nil, err
	}

	db, err := stateMgr.Load(oldName)
	if err != nil {
		return nil, fmt.Errorf("database '%s' not found", oldName)
	}

	newContainerName := fmt.Sprintf("xmdb_%s", newName)
	oldContainerName := db.ContainerName

	if dockerMgr, err := container.NewDockerManager(runtimeMode); err == nil {
		if _, statusErr := dockerMgr.ContainerStatus(ctx, db.ContainerID, oldContainerName); statusErr == nil {
			if err := dockerMgr.RenameContainer(ctx, db.ContainerID, oldContainerName, newContainerName); err != nil {
				return nil, fmt.Errorf("renaming container '%s' -> '%s': %w", oldContainerName, newContainerName, err)
			}
		}
	}

	db.Name = newName
	db.ContainerName = newContainerName

	if err := stateMgr.Save(db); err != nil {
		return nil, fmt.Errorf("saving renamed state: %w", err)
	}
	if err := stateMgr.Delete(oldName); err != nil {
		return nil, fmt.Errorf("removing old state file: %w", err)
	}

	if snapMgr, err := NewSnapshotManager(); err == nil {
		_ = snapMgr.RenameDatabaseDir(oldName, newName)
	}

	return db, nil
}

// MoveDatabase reassigns a managed database to a different group.
// Passing "" or "global" clears the group (moves to global).
func MoveDatabase(stateMgr *state.Manager, name, group string) (*state.Database, error) {
	db, err := stateMgr.Load(name)
	if err != nil {
		return nil, fmt.Errorf("database '%s' not found", name)
	}

	g := strings.TrimSpace(group)
	if strings.EqualFold(g, GlobalGroupLabel) {
		g = ""
	}
	if g != "" {
		if err := GroupValidator(g); err != nil {
			return nil, err
		}
	}

	db.Group = g
	if err := stateMgr.Save(db); err != nil {
		return nil, fmt.Errorf("saving database state: %w", err)
	}
	return db, nil
}
