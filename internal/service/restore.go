package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

// RestoreDatabase streams backupFile into the named database container without
// interactive prompts. Output from the restore command is written to stdout.
func RestoreDatabase(ctx context.Context, runtimeMode, name, backupFile string, stdout io.Writer) (string, int64, string, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	stateMgr, err := state.NewManager()
	if err != nil {
		return "", 0, "", err
	}

	db, err := stateMgr.Load(name)
	if err != nil {
		return "", 0, "", fmt.Errorf("database '%s' not found", name)
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

	sqlFile, err := os.Open(absPath)
	if err != nil {
		return "", 0, "", fmt.Errorf("reading backup file: %w", err)
	}
	defer sqlFile.Close()

	var cmd []string
	if db.Provider == utils.ProviderMySQL {
		cmd = []string{
			"mysql",
			"-u", "root",
			fmt.Sprintf("-p%s", db.RootPassword),
			db.DatabaseName,
		}
	} else {
		cmd = []string{
			"psql",
			"-U", "postgres",
			"-d", db.DatabaseName,
			"-v", "ON_ERROR_STOP=1",
		}
	}

	res, err := dockerMgr.Exec(ctx, db.ContainerID, db.ContainerName, cmd, sqlFile, stdout)
	if err != nil {
		return "", 0, "", fmt.Errorf("restoring database: %w", err)
	}
	if res.ExitCode != 0 {
		return "", 0, "", fmt.Errorf("restore failed (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	fileInfo, _ := os.Stat(absPath)
	var sizeBytes int64
	if fileInfo != nil {
		sizeBytes = fileInfo.Size()
	}
	return absPath, sizeBytes, res.Stderr, nil
}
