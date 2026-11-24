package commands

import (
	"context"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/spf13/cobra"
)

var renameCmd = &cobra.Command{
	Use:   "rename <old-name> <new-name>",
	Short: "Rename a managed database, its container, and its snapshot directory",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRename(args[0], args[1])
	},
}

func runRename(oldName, newName string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	db, err := service.RenameDatabase(context.Background(), runtimeMode, stateMgr, oldName, newName)
	if err != nil {
		return err
	}

	color.Green("Renamed database '%s' -> '%s' (container: %s)\n", oldName, db.Name, db.ContainerName)
	return nil
}
