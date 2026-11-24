package commands

import (
	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/spf13/cobra"
)

var moveGroup string

var moveCmd = &cobra.Command{
	Use:   "move <database-name>",
	Short: "Move a managed database to another group (use -g global to ungroup)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMove(args[0])
	},
}

func init() {
	moveCmd.Flags().StringVarP(&moveGroup, "group", "g", "", "Target group name (use 'global' to clear group)")
	_ = moveCmd.MarkFlagRequired("group")
}

func runMove(name string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	db, err := service.MoveDatabase(stateMgr, name, moveGroup)
	if err != nil {
		return err
	}

	dest := service.GroupLabel(db.Group)
	color.Green("Moved database '%s' to group '%s'\n", db.Name, dest)
	return nil
}
