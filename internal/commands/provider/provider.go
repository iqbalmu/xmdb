package provider

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage database providers",
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available database providers",
	Run: func(cmd *cobra.Command, args []string) {
		runList()
	},
}

func init() {
	Cmd.AddCommand(listCmd)
}

func runList() {
	titleColor := color.New(color.FgCyan, color.Bold)
	titleColor.Println("\nSupported Database Providers:")

	var data [][]any
	for _, p := range providers.AllProviders() {
		presets := strings.Join(providers.KnownPresets(p.Name()), ", ")
		if presets == "" {
			presets = "-"
		}
		data = append(data, []any{
			p.Name(),
			string(p.Kind()),
			p.GetDefaultVersion(),
			p.GetDefaultPort(),
			strings.Join(providers.KnownVersions(p.Name()), ", "),
			presets,
		})
	}

	fmt.Println()

	table := tablewriter.NewTable(os.Stdout)
	table.Header("Provider", "Kind", "Default Version", "Default Port", "Known Versions", "Presets")
	table.Bulk(data)
	table.Render()

	fmt.Println()
	fmt.Println("Usage:   xmdb create <name> --provider <provider> [--preset <preset>] [--image <image>]")
	fmt.Println("Example: xmdb create vectordb --provider postgres --preset pgvector")
	fmt.Println("Example: xmdb create cache --provider redis")
	fmt.Println()
}
