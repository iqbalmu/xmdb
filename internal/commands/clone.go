package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/spf13/cobra"
)

var (
	clonePort         int
	cloneGroup        string
	cloneVersion      string
	cloneWithVolume   bool
	cloneFromSnapshot string
)

var cloneCmd = &cobra.Command{
	Use:   "clone <source> <target>",
	Short: "Clone an existing database and its data into a new instance",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runClone(cmd, args[0], args[1])
	},
}

func init() {
	cloneCmd.Flags().IntVar(&clonePort, "port", 0, "Host port for cloned database (default: next free port)")
	cloneCmd.Flags().StringVarP(&cloneGroup, "group", "g", "", "Group for cloned database (default: inherit from source)")
	cloneCmd.Flags().StringVarP(&cloneVersion, "version", "v", "", "Database version override (default: inherit from source)")
	cloneCmd.Flags().BoolVar(&cloneWithVolume, "volume", true, "Create with persistent volume (default: true)")
	cloneCmd.Flags().StringVar(&cloneFromSnapshot, "from-snapshot", "", "Clone from a saved snapshot name or ID instead of live data")
}

type cloneOptions = service.CloneOptions

func cloneDatabase(ctx context.Context, stateMgr *state.Manager, opts cloneOptions, onProgress func(string)) (*state.Database, *providers.DBConfig, error) {
	return service.CloneDatabase(ctx, runtimeMode, stateMgr, opts, onProgress)
}

func runClone(cmd *cobra.Command, sourceName, targetName string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	opts := cloneOptions{
		SourceName:   sourceName,
		TargetName:   targetName,
		Port:         clonePort,
		Group:        strings.TrimSpace(cloneGroup),
		GroupSet:     cmd.Flags().Changed("group"),
		Version:      strings.TrimSpace(cloneVersion),
		WithVolume:   cloneWithVolume,
		FromSnapshot: strings.TrimSpace(cloneFromSnapshot),
	}

	color.Cyan("\nCloning database '%s' -> '%s'...\n", sourceName, targetName)
	dbState, dbConfig, err := cloneDatabase(context.Background(), stateMgr, opts, func(msg string) {
		color.White(msg)
	})
	if err != nil {
		return err
	}

	prov, _ := providers.For(dbState.Provider)
	color.Green("\nDatabase '%s' cloned successfully from '%s'!\n", dbState.Name, sourceName)
	fmt.Println("Connection Information:")
	color.Cyan("  Provider: %s", dbState.Provider)
	color.Cyan("  Port: %d", dbState.Port)
	color.Cyan("  Database: %s", dbState.DatabaseName)
	if dbState.User != "" {
		color.Cyan("  User: %s", dbState.User)
		color.Cyan("  Password: %s", dbState.UserPassword)
		fmt.Println()
		color.White("Connection String:")
		fmt.Printf("  %s\n", prov.GetConnectionString(dbConfig))
	}
	fmt.Println()
	return nil
}

// cloneDatabase duplicates a managed database (schema and data) into a new
// managed instance via logical dump/restore streaming. If source is stopped,
// it is temporarily started for the dump and stopped again afterwards.
// end of clone.go
