package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/spf13/cobra"
)

var runManifestPath string

var runCmd = &cobra.Command{
	Use:   "run [database-name] -- <command> [args...]",
	Short: "Run a command with database environment variables injected",
	Long: `Run a command with DATABASE_URL and DB_* (or custom xmdb.yaml env mappings)
injected directly into the child process without modifying any .env file.

Examples:
  xmdb run -- npm run dev
  xmdb run mydb -- go test ./...`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithEnv(cmd, args)
	},
}

func init() {
	runCmd.Flags().StringVarP(&runManifestPath, "file", "f", "", "Path to xmdb.yaml (default: auto-discover when no database name given)")
}

func runWithEnv(cmd *cobra.Command, args []string) error {
	dashIdx := cmd.ArgsLenAtDash()
	var dbName string
	var subArgs []string

	if dashIdx >= 0 {
		if dashIdx > 1 {
			return fmt.Errorf("expected at most one database name before '--'")
		}
		if dashIdx == 1 {
			dbName = args[0]
		}
		subArgs = args[dashIdx:]
	} else {
		if len(args) == 0 {
			return fmt.Errorf("missing command to execute (usage: xmdb run [db] -- <command> [args...])")
		}
		subArgs = args
	}

	if len(subArgs) == 0 {
		return fmt.Errorf("missing command after '--'")
	}

	vars, _, err := resolveEnvVars(dbName, runManifestPath)
	if err != nil {
		return err
	}

	child := exec.Command(subArgs[0], subArgs[1:]...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	env := os.Environ()
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, fmt.Sprintf("%s=%s", k, vars[k]))
	}
	child.Env = env

	if err := child.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}
