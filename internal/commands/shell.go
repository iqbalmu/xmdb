package commands

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:     "shell <database-name>",
	Aliases: []string{"sh", "console"},
	Short:   "Open an interactive SQL shell (psql/mysql) inside the container",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShell(args[0])
	},
}

func runShell(name string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	dbState, err := stateMgr.Load(name)
	if err != nil {
		return fmt.Errorf("database '%s' not found", name)
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return err
	}

	ctx := context.Background()
	running, err := dockerMgr.IsContainerRunning(ctx, dbState.ContainerID, dbState.ContainerName)
	if err != nil || !running {
		return fmt.Errorf("database '%s' is not running (start it with: xmdb start %s)", name, name)
	}

	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return err
	}

	cmd := prov.ShellCommand(&providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	})

	color.Cyan("Opening %s shell for '%s' (type \\q or exit to quit)...\n", dbState.Provider, name)
	return dockerMgr.ExecInteractive(ctx, dbState.ContainerID, dbState.ContainerName, cmd)
}
