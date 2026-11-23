package commands

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/spf13/cobra"
)

var backupOutput string

var backupCmd = &cobra.Command{
	Use:   "backup <name>",
	Short: "Backup database to SQL file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackup(args[0])
	},
}

func init() {
	backupCmd.Flags().StringVarP(&backupOutput, "output", "o", "", "Output file path (default: ./backup_<name>_<timestamp>.sql)")
}

func runBackup(name string) error {
	color.Cyan("\nCreating backup for '%s'...\n", name)

	absPath, sizeBytes, stderr, err := backupDatabase(context.Background(), name, backupOutput)
	if err != nil {
		return err
	}
	if stderr != "" {
		color.Yellow("Warnings:\n%s", stderr)
	}

	size := float64(sizeBytes) / 1024 / 1024 // MB
	color.Green("\nBackup created successfully!\n")
	color.Cyan("  File: %s", absPath)
	color.Cyan("  Size: %.2f MB", size)
	fmt.Println()
	return nil
}

func backupDatabase(ctx context.Context, name, outputPath string) (string, int64, string, error) {
	return service.BackupDatabase(ctx, runtimeMode, name, outputPath)
}
