package commands

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/commands/provider"
	"github.com/iqbalmu/xmdb/internal/commands/table"
	"github.com/spf13/cobra"
)

var (
	Version     = "1.0.0"
	runtimeMode string
)

var rootCmd = &cobra.Command{
	Use:           "xmdb",
	Short:         "Database Management CLI for Development",
	SilenceUsage:  true,
	SilenceErrors: true,
	Long: `xmdb is a powerful CLI tool for managing development databases.
It supports MySQL and PostgreSQL with easy container management.`,
	Run: func(cmd *cobra.Command, args []string) {
		runWizard()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&runtimeMode, "runtime", "auto", "Container daemon to connect to (auto, docker, podman)")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(provider.Cmd)
	rootCmd.AddCommand(table.Cmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(connectCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(updateCmd)
}

func showOverview() {
	titleColor := color.New(color.FgCyan, color.Bold)
	subtitleColor := color.New(color.FgWhite)
	commandColor := color.New(color.FgGreen)

	titleColor.Println("\n╔═══════════════════════════════════════════════════════╗")
	titleColor.Println("║          xmdb - Database Management CLI               ║")
	titleColor.Println("╚═══════════════════════════════════════════════════════╝")

	subtitleColor.Println("\nManage your development databases with ease!")
	subtitleColor.Println("Supports: MySQL, PostgreSQL")

	fmt.Println("\nCommon Commands:")
	commandColor.Print("  xmdb version")
	fmt.Println("              Show version information")

	commandColor.Print("  xmdb status")
	fmt.Println("               Show status of all databases")

	commandColor.Print("  xmdb provider list")
	fmt.Println("         List available database providers")

	commandColor.Print("  xmdb create <name>")
	fmt.Println("           Create a new database")

	commandColor.Print("  xmdb list")
	fmt.Println("                  List all managed databases")

	commandColor.Print("  xmdb start <name>")
	fmt.Println("           Start a database")

	commandColor.Print("  xmdb stop <name>")
	fmt.Println("            Stop a database")

	commandColor.Print("  xmdb connect <name>")
	fmt.Println("          Show connection information")

	fmt.Println("\nQuick Start:")
	fmt.Println("  1. xmdb create mydb --provider mysql")
	fmt.Println("  2. xmdb connect mydb")
	fmt.Println("  3. xmdb table list mydb")

	fmt.Println("\nFor more information, run: xmdb --help")
	fmt.Println()
}
