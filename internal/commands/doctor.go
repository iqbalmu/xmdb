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
	doctorFix   bool
	doctorForce bool
	doctorJSON  bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check container runtime, orphan resources, stale states, and port conflicts",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor()
	},
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Clean up orphan containers, orphan volumes, and stale states")
	doctorCmd.Flags().BoolVarP(&doctorForce, "force", "f", false, "Skip confirmation prompt when using --fix")
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Output diagnostic report as JSON")
}

func runDoctor() error {
	ctx := context.Background()
	report, err := service.AnalyzeDoctor(ctx, runtimeMode)
	if err != nil {
		return err
	}

	if doctorJSON && !doctorFix {
		return printJSON(report)
	}

	if !doctorJSON {
		printDoctorReport(report)
	}

	if !doctorFix {
		return nil
	}

	if !report.HasFixableIssues() {
		if doctorJSON {
			return printJSON(map[string]any{"report": report, "fix": &service.DoctorFixResult{}})
		}
		color.Green("Nothing to clean up.\n")
		return nil
	}

	if !doctorForce {
		var confirm bool
		prompt := &survey.Confirm{
			Message: fmt.Sprintf(
				"Remove %d orphan container(s), %d orphan volume(s), and %d stale state(s)?",
				len(report.OrphanContainers), len(report.OrphanVolumes), len(report.StaleStates),
			),
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

	fixRes, err := service.FixDoctor(ctx, runtimeMode, report)
	if err != nil {
		return err
	}

	if doctorJSON {
		return printJSON(map[string]any{"report": report, "fix": fixRes})
	}

	color.Green("\nCleanup complete: %d container(s), %d volume(s), %d stale state(s) removed.\n",
		fixRes.RemovedContainers, fixRes.RemovedVolumes, fixRes.RemovedStates)
	for _, e := range fixRes.Errors {
		color.Yellow("  Warning: %s", e)
	}
	return nil
}

func printDoctorReport(r *service.DoctorReport) {
	color.Cyan("\nxmdb doctor\n")
	if r.RuntimeOK {
		color.Green("  ✓ Container runtime: connected")
	} else {
		color.Red("  ✗ Container runtime: unavailable (%s)", r.RuntimeError)
	}
	fmt.Printf("  • Managed databases in state: %d\n", r.DatabasesCount)

	if len(r.StaleStates) == 0 {
		color.Green("  ✓ Stale states: none")
	} else {
		color.Yellow("  ! Stale states (%d):", len(r.StaleStates))
		for _, s := range r.StaleStates {
			fmt.Printf("      - %s (missing container %s)\n", s.Name, s.ContainerName)
		}
	}

	if len(r.OrphanContainers) == 0 {
		color.Green("  ✓ Orphan containers: none")
	} else {
		color.Yellow("  ! Orphan containers (%d):", len(r.OrphanContainers))
		for _, c := range r.OrphanContainers {
			fmt.Printf("      - %s (%s)\n", c.Name, c.State)
		}
	}

	if len(r.OrphanVolumes) == 0 {
		color.Green("  ✓ Orphan volumes: none")
	} else {
		color.Yellow("  ! Orphan volumes (%d):", len(r.OrphanVolumes))
		for _, v := range r.OrphanVolumes {
			fmt.Printf("      - %s\n", v)
		}
	}

	if len(r.PortConflicts) == 0 {
		color.Green("  ✓ Port conflicts: none")
	} else {
		color.Yellow("  ! Port conflicts (%d):", len(r.PortConflicts))
		for _, p := range r.PortConflicts {
			fmt.Printf("      - %s (port %d): %s\n", p.DatabaseName, p.Port, p.Reason)
		}
	}

	if r.HasFixableIssues() && !doctorFix {
		color.Cyan("\nRun 'xmdb doctor --fix' to clean up orphan containers/volumes and stale states.\n")
	} else if r.Healthy {
		color.Green("\nEverything looks healthy!\n")
	} else {
		fmt.Println()
	}
}
