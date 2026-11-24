package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var (
	upFile    string
	upReseed  bool
	upSyncEnv bool
	upEnvFile string
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Provision and start all databases defined in xmdb.yaml",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runUp()
	},
}

func init() {
	upCmd.Flags().StringVarP(&upFile, "file", "f", "", "Path to xmdb.yaml (default: search current and parent directories)")
	upCmd.Flags().BoolVar(&upReseed, "reseed", false, "Re-run seed scripts on already-existing databases")
	upCmd.Flags().BoolVar(&upSyncEnv, "sync-env", false, "Sync managed environment variables into .env (or env_file)")
	upCmd.Flags().StringVar(&upEnvFile, "env-file", "", "Target .env file path when using --sync-env")
}

func runUp() error {
	manifestPath, err := resolveManifestFlag(upFile)
	if err != nil {
		return err
	}

	color.Cyan("\nBringing up project databases from %s...\n", manifestPath)
	results, syncedPath, err := service.UpProject(context.Background(), runtimeMode, manifestPath, service.UpOptions{
		Reseed:  upReseed,
		SyncEnv: upSyncEnv,
		EnvFile: upEnvFile,
	}, func(msg string) {
		color.White("  %s", msg)
	})
	if err != nil {
		return err
	}

	fmt.Println()
	var rows [][]any
	for _, r := range results {
		note := r.PortReassign
		if r.SeededFiles > 0 {
			if note != "" {
				note += "; "
			}
			note += fmt.Sprintf("seeded %d file(s)", r.SeededFiles)
		}
		rows = append(rows, []any{r.Key, r.InstanceName, r.Provider, r.Port, r.Action, note})
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("Service", "Database", "Provider", "Port", "Action", "Notes")
	table.Bulk(rows)
	table.Render()

	if syncedPath != "" {
		color.Green("\nSynced env block to %s", syncedPath)
	}
	fmt.Println()
	return nil
}
