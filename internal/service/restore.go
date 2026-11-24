package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

// RestoreOptions controls safety behavior when restoring from a backup file.
type RestoreOptions struct {
	NoSnapshot bool
	OnProgress func(string)
}

// RestoreDatabase streams backupFile into the named database container without
// interactive prompts. A safety auto-snapshot is taken first by default.
func RestoreDatabase(ctx context.Context, runtimeMode, name, backupFile string, stdout io.Writer) (string, int64, string, error) {
	return RestoreDatabaseWithOptions(ctx, runtimeMode, name, backupFile, RestoreOptions{}, stdout)
}

// RestoreDatabaseWithOptions streams backupFile into the named database container.
func RestoreDatabaseWithOptions(ctx context.Context, runtimeMode, name, backupFile string, opts RestoreOptions, stdout io.Writer) (string, int64, string, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	report := func(msg string) {
		if opts.OnProgress != nil {
			opts.OnProgress(msg)
		}
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		return "", 0, "", err
	}

	db, err := stateMgr.Load(name)
	if err != nil {
		return "", 0, "", fmt.Errorf("database '%s' not found", name)
	}

	prov, err := providers.For(db.Provider)
	if err != nil {
		return "", 0, "", err
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return "", 0, "", err
	}

	running, err := dockerMgr.IsContainerRunning(ctx, db.ContainerID, db.ContainerName)
	if err != nil || !running {
		return "", 0, "", fmt.Errorf("database '%s' is not running", name)
	}

	absPath, err := filepath.Abs(backupFile)
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid backup file path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", 0, "", fmt.Errorf("backup file not found: %s", absPath)
	}

	if !opts.NoSnapshot {
		report("Taking safety auto-snapshot before restore...")
		if autoMeta, err := CreateAutoSnapshot(ctx, runtimeMode, name, "restore"); err != nil {
			return "", 0, "", fmt.Errorf("creating safety snapshot (pass --no-snapshot to skip): %w", err)
		} else if autoMeta != nil {
			report(fmt.Sprintf("Saved safety snapshot '%s'", autoMeta.Name))
		}
	}

	f, err := os.Open(absPath)
	if err != nil {
		return "", 0, "", fmt.Errorf("reading backup file: %w", err)
	}
	defer f.Close()

	var stderr string
	if kvProv, ok := providers.AsKV(prov); ok {
		if err := kvProv.RestoreKV(ctx, adminDBConfig(db), f); err != nil {
			return "", 0, "", fmt.Errorf("restoring KV database: %w", err)
		}
	} else if snapProv, ok := providers.AsSnapshot(prov); ok {
		cmd := snapProv.RestoreCommand(adminDBConfig(db))
		res, execErr := dockerMgr.Exec(ctx, db.ContainerID, db.ContainerName, cmd, f, stdout)
		if execErr != nil {
			return "", 0, "", fmt.Errorf("restoring database: %w", execErr)
		}
		if res.ExitCode != 0 {
			return "", 0, "", fmt.Errorf("restore failed (exit %d):\n%s", res.ExitCode, res.Stderr)
		}
		stderr = res.Stderr
	} else {
		return "", 0, "", fmt.Errorf("provider '%s' does not support restore", db.Provider)
	}

	fileInfo, _ := os.Stat(absPath)
	var sizeBytes int64
	if fileInfo != nil {
		sizeBytes = fileInfo.Size()
	}
	return absPath, sizeBytes, stderr, nil
}
