package commands

import (
	"context"
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/spf13/cobra"
)

var resetForce bool

var resetCmd = &cobra.Command{
	Use:   "reset <database-name>",
	Short: "Wipe all data and re-initialize a database with the same port and credentials",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runReset(args[0])
	},
}

func init() {
	resetCmd.Flags().BoolVarP(&resetForce, "force", "f", false, "Skip confirmation prompt")
}

func runReset(name string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}
	if _, err := stateMgr.Load(name); err != nil {
		return fmt.Errorf("database '%s' not found", name)
	}

	if !resetForce {
		var confirm bool
		prompt := &survey.Confirm{
			Message: color.RedString("WARNING: This will wipe all data in '%s' and recreate it fresh. Continue?", name),
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

	color.Cyan("\nResetting database '%s'...\n", name)
	dbState, err := service.ResetDatabase(context.Background(), runtimeMode, name, func(msg string) {
		color.White("  %s", msg)
	})
	if err != nil {
		return err
	}

	color.Green("\nDatabase '%s' reset successfully! (Port %d, credentials unchanged)\n", name, dbState.Port)
	return nil
}
