package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

// BackupDatabase dumps the named database to outputPath (or a timestamped
// default in the current working directory if empty) without writing to stdout.
func BackupDatabase(ctx context.Context, runtimeMode, name, outputPath string) (string, int64, string, error) {
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
	snapProv, ok := providers.AsSnapshot(prov)
	if !ok {
		return "", 0, "", fmt.Errorf("provider '%s' does not support backup", db.Provider)
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return "", 0, "", err
	}

	running, err := dockerMgr.IsContainerRunning(ctx, db.ContainerID, db.ContainerName)
	if err != nil || !running {
		return "", 0, "", fmt.Errorf("database '%s' is not running", name)
	}

	if outputPath == "" {
		timestamp := time.Now().Format("20060102_150405")
		outputPath = fmt.Sprintf("backup_%s_%s.sql", name, timestamp)
	}

	absPath, err := filepath.Abs(outputPath)
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid output path: %w", err)
	}

	cmd := snapProv.BackupCommand(adminDBConfig(db))

	dumpFile, err := os.OpenFile(absPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, "", fmt.Errorf("creating backup file: %w", err)
	}

	res, err := dockerMgr.Exec(ctx, db.ContainerID, db.ContainerName, cmd, nil, dumpFile)
	dumpFile.Close()
	if err != nil {
		os.Remove(absPath)
		return "", 0, "", fmt.Errorf("creating backup: %w", err)
	}
	if res.ExitCode != 0 {
		os.Remove(absPath)
		return "", 0, "", fmt.Errorf("creating backup (exit %d):\n%s", res.ExitCode, res.Stderr)
	}

	fileInfo, _ := os.Stat(absPath)
	var sizeBytes int64
	if fileInfo != nil {
		sizeBytes = fileInfo.Size()
	}
	return absPath, sizeBytes, res.Stderr, nil
}
