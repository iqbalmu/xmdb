package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/spf13/cobra"
)

var restoreForce bool

var restoreCmd = &cobra.Command{
	Use:   "restore <name> <backup-file>",
	Short: "Restore database from SQL file",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRestore(args[0], args[1])
	},
}

func init() {
	restoreCmd.Flags().BoolVarP(&restoreForce, "force", "f", false, "Skip confirmation prompt")
}

func runRestore(name, backupFile string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	db, err := stateMgr.Load(name)
	if err != nil {
		return fmt.Errorf("database '%s' not found", name)
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return err
	}

	ctx := context.Background()

	running, err := dockerMgr.IsContainerRunning(ctx, db.ContainerID, db.ContainerName)
	if err != nil || !running {
		return fmt.Errorf("database '%s' is not running", name)
	}

	absPath, err := filepath.Abs(backupFile)
	if err != nil {
		return fmt.Errorf("invalid backup file path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", absPath)
	}

	if !restoreForce {
		var confirm bool
		prompt := &survey.Confirm{
			Message: color.YellowString("WARNING: This will replace all data in database '%s'. Continue?", name),
			Default: false,
		}
		if err := survey.AskOne(prompt, &confirm); err != nil {
			return err
		}
		if !confirm {
			color.Yellow("Cancelled")
			return nil
		}
	}

	color.Cyan("\nRestoring database '%s' from backup...\n", name)

	absPath, sizeBytes, stderr, err := restoreDatabase(ctx, name, backupFile, os.Stdout)
	if err != nil {
		return err
	}
	if stderr != "" {
		color.Yellow("Warnings:\n%s", stderr)
	}

	size := float64(sizeBytes) / 1024 / 1024 // MB

	color.Green("\nDatabase restored successfully!\n")
	color.Cyan("  File: %s", absPath)
	color.Cyan("  Size: %.2f MB", size)
	fmt.Println()
	return nil
}

func restoreDatabase(ctx context.Context, name, backupFile string, stdout io.Writer) (string, int64, string, error) {
	return service.RestoreDatabase(ctx, runtimeMode, name, backupFile, stdout)
}
