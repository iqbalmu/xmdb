package commands

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/project"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/spf13/cobra"
)

var (
	envManifestPath string
	envJSON         bool
	envTargetFile   string
)

var envCmd = &cobra.Command{
	Use:   "env [database-name]",
	Short: "Print or sync database environment variables (from xmdb.yaml or a database)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbName := ""
		if len(args) == 1 {
			dbName = args[0]
		}
		return runEnvPrint(dbName)
	},
}

var envSyncCmd = &cobra.Command{
	Use:   "sync [database-name]",
	Short: "Safely upsert managed xmdb env vars into a .env file (# xmdb:start..end)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbName := ""
		if len(args) == 1 {
			dbName = args[0]
		}
		return runEnvSync(dbName)
	},
}

func init() {
	envCmd.PersistentFlags().StringVarP(&envManifestPath, "manifest", "m", "", "Path to xmdb.yaml (default: auto-discover when no database name given)")
	envCmd.Flags().BoolVar(&envJSON, "json", false, "Output as JSON")
	envSyncCmd.Flags().StringVarP(&envTargetFile, "file", "f", "", "Target .env file path (default: env_file in xmdb.yaml or .env)")
	envCmd.AddCommand(envSyncCmd)
}

func resolveEnvVars(dbName, manifestFlag string) (map[string]string, string, error) {
	if strings.TrimSpace(dbName) != "" {
		vars, err := service.ResolveDatabaseEnv(dbName)
		return vars, "", err
	}
	manifestPath, err := resolveManifestFlag(manifestFlag)
	if err != nil {
		return nil, "", err
	}
	vars, err := service.ResolveProjectEnv(manifestPath)
	return vars, manifestPath, err
}

func runEnvPrint(dbName string) error {
	vars, _, err := resolveEnvVars(dbName, envManifestPath)
	if err != nil {
		return err
	}
	if envJSON {
		return printJSON(vars)
	}
	for _, line := range project.FormatEnvLines(vars) {
		fmt.Println(line)
	}
	return nil
}

func runEnvSync(dbName string) error {
	vars, manifestPath, err := resolveEnvVars(dbName, envManifestPath)
	if err != nil {
		return err
	}

	target := strings.TrimSpace(envTargetFile)
	if target == "" && manifestPath != "" {
		if m, absPath, err := project.Load(manifestPath); err == nil && strings.TrimSpace(m.EnvFile) != "" {
			if filepath.IsAbs(m.EnvFile) {
				target = m.EnvFile
			} else {
				target = filepath.Join(filepath.Dir(absPath), m.EnvFile)
			}
		}
	}
	if target == "" {
		target = ".env"
	}

	absPath, err := project.SyncEnvFile(target, vars)
	if err != nil {
		return err
	}

	color.Green("Synced %d variable(s) into %s\n", len(vars), absPath)
	return nil
}
