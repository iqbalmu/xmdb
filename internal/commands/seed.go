package commands

import (
	"context"

	"github.com/iqbalmu/xmdb/internal/service"
)

func collectSeedFiles(seedPath string) ([]string, error) {
	return service.CollectSeedFiles(seedPath)
}

func seedDatabase(ctx context.Context, dbName, seedPath string, onProgress func(string)) (int, error) {
	return service.SeedDatabase(ctx, runtimeMode, dbName, seedPath, onProgress)
}
