package commands

import (
	"context"
	"fmt"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/spf13/cobra"
)

var (
	downFile   string
	downRemove bool
	downForce  bool
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop (or remove) all databases defined in xmdb.yaml",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDown()
	},
}

func init() {
	downCmd.Flags().StringVarP(&downFile, "file", "f", "", "Path to xmdb.yaml (default: search current and parent directories)")
	downCmd.Flags().BoolVar(&downRemove, "remove", false, "Also remove containers, data volumes, and state")
	downCmd.Flags().BoolVarP(&downForce, "yes", "y", false, "Skip confirmation prompt when using --remove")
}

func runDown() error {
	manifestPath, err := resolveManifestFlag(downFile)
	if err != nil {
		return err
	}

	if downRemove && !downForce {
		var confirm bool
		prompt := &survey.Confirm{
			Message: color.RedString("WARNING: This will permanently remove all containers and volumes in %s. Continue?", manifestPath),
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

	actionLabel := "Stopping"
	if downRemove {
		actionLabel = "Removing"
	}
	color.Cyan("\n%s project databases from %s...\n", actionLabel, manifestPath)

	results, err := service.DownProject(context.Background(), runtimeMode, manifestPath, service.DownOptions{
		Remove: downRemove,
	}, func(msg string) {
		color.White("  %s", msg)
	})
	if err != nil {
		return err
	}

	for _, r := range results {
		color.Green("  [%s] %s: %s", r.Key, r.InstanceName, r.Action)
	}
	fmt.Println()
	return nil
}
