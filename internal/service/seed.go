package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CollectSeedFiles resolves seedPath to an ordered list of SQL files.
func CollectSeedFiles(seedPath string) ([]string, error) {
	trimmed := strings.TrimSpace(seedPath)
	if trimmed == "" {
		return nil, fmt.Errorf("seed path is empty")
	}

	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid seed path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("seed path not found: %s", absPath)
		}
		return nil, fmt.Errorf("inspecting seed path: %w", err)
	}

	if !info.IsDir() {
		return []string{absPath}, nil
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading seed directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".sql") {
			files = append(files, filepath.Join(absPath, entry.Name()))
		}
	}

	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no .sql files found in seed directory: %s", absPath)
	}
	return files, nil
}

// SeedDatabase executes one or more SQL files (sorted alphabetically when
// seedPath is a directory) against the named database.
func SeedDatabase(ctx context.Context, runtimeMode, dbName, seedPath string, onProgress func(string)) (int, error) {
	files, err := CollectSeedFiles(seedPath)
	if err != nil {
		return 0, err
	}

	for i, file := range files {
		if onProgress != nil {
			onProgress(fmt.Sprintf("Seeding (%d/%d) %s...", i+1, len(files), filepath.Base(file)))
		}
		if _, _, _, err := RestoreDatabaseWithOptions(ctx, runtimeMode, dbName, file, RestoreOptions{NoSnapshot: true}, io.Discard); err != nil {
			return i, fmt.Errorf("seeding %s: %w", filepath.Base(file), err)
		}
	}

	return len(files), nil
}
