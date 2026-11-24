package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var (
	snapNote       string
	snapListJSON   bool
	snapForce      bool
	snapNoSnapshot bool
)

var snapshotCmd = &cobra.Command{
	Use:     "snapshot",
	Aliases: []string{"snap"},
	Short:   "Create, list, restore, and delete managed database snapshots",
}

var snapCreateCmd = &cobra.Command{
	Use:   "create <database-name> [snapshot-name]",
	Short: "Take an online compressed snapshot of a running database",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) == 2 {
			name = args[1]
		}
		return runSnapshotCreate(args[0], name)
	},
}

var snapListCmd = &cobra.Command{
	Use:     "list <database-name>",
	Aliases: []string{"ls"},
	Short:   "List saved snapshots for a database",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSnapshotList(args[0])
	},
}

var snapRestoreCmd = &cobra.Command{
	Use:   "restore <database-name> <snapshot-name-or-id>",
	Short: "Roll back a database to a saved snapshot",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSnapshotRestore(args[0], args[1])
	},
}

var snapDeleteCmd = &cobra.Command{
	Use:     "delete <database-name> <snapshot-name-or-id>",
	Aliases: []string{"rm", "remove"},
	Short:   "Delete a saved snapshot",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSnapshotDelete(args[0], args[1])
	},
}

func init() {
	snapCreateCmd.Flags().StringVarP(&snapNote, "note", "m", "", "Optional description for the snapshot")
	snapListCmd.Flags().BoolVar(&snapListJSON, "json", false, "Output as JSON")
	snapRestoreCmd.Flags().BoolVarP(&snapForce, "force", "f", false, "Skip confirmation prompt")
	snapRestoreCmd.Flags().BoolVar(&snapNoSnapshot, "no-snapshot", false, "Skip automatic safety snapshot before rolling back")
	snapDeleteCmd.Flags().BoolVarP(&snapForce, "force", "f", false, "Skip confirmation prompt")

	snapshotCmd.AddCommand(snapCreateCmd)
	snapshotCmd.AddCommand(snapListCmd)
	snapshotCmd.AddCommand(snapRestoreCmd)
	snapshotCmd.AddCommand(snapDeleteCmd)
}

func runSnapshotCreate(dbName, snapName string) error {
	color.Cyan("\nCreating snapshot for database '%s'...\n", dbName)
	meta, err := service.CreateSnapshot(context.Background(), runtimeMode, dbName, snapName, snapNote, false)
	if err != nil {
		return err
	}

	color.Green("Snapshot created successfully!\n")
	color.Cyan("  Name:    %s", meta.Name)
	color.Cyan("  ID:      %s", meta.ID)
	color.Cyan("  Format:  %s", meta.Format)
	color.Cyan("  Size:    %s", formatBytes(meta.SizeBytes))
	if meta.Note != "" {
		color.Cyan("  Note:    %s", meta.Note)
	}
	fmt.Println()
	return nil
}

func runSnapshotList(dbName string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}
	if !stateMgr.Exists(dbName) {
		return fmt.Errorf("database '%s' not found", dbName)
	}

	snapMgr, err := service.NewSnapshotManager()
	if err != nil {
		return err
	}
	snaps, err := snapMgr.List(dbName)
	if err != nil {
		return err
	}

	if snapListJSON {
		if snaps == nil {
			snaps = []*service.SnapshotMeta{}
		}
		return printJSON(snaps)
	}

	if len(snaps) == 0 {
		color.Yellow("\nNo snapshots found for database '%s'.", dbName)
		fmt.Printf("Create one with: xmdb snapshot create %s [name]\n\n", dbName)
		return nil
	}

	color.Cyan("\nSnapshots for '%s':\n", dbName)
	var rows [][]any
	for _, s := range snaps {
		kind := "manual"
		if s.Auto {
			kind = "auto"
		}
		rows = append(rows, []any{
			s.Name,
			s.ID,
			kind,
			formatBytes(s.SizeBytes),
			s.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			s.Note,
		})
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("Name", "ID", "Type", "Size", "Created", "Note")
	table.Bulk(rows)
	table.Render()
	fmt.Println()
	return nil
}

func runSnapshotRestore(dbName, ref string) error {
	if !snapForce {
		var confirm bool
		prompt := &survey.Confirm{
			Message: color.YellowString("WARNING: This will replace all data in '%s' with snapshot '%s'. Continue?", dbName, ref),
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

	color.Cyan("\nRolling back '%s' to snapshot '%s'...\n", dbName, ref)
	meta, err := service.RestoreSnapshot(context.Background(), runtimeMode, dbName, ref, service.SnapshotRestoreOptions{
		NoAutoSnapshot: snapNoSnapshot,
	}, func(msg string) {
		color.White("  %s", msg)
	})
	if err != nil {
		return err
	}

	color.Green("\nDatabase '%s' rolled back to snapshot '%s' (%s)!\n", dbName, meta.Name, meta.ID)
	return nil
}

func runSnapshotDelete(dbName, ref string) error {
	snapMgr, err := service.NewSnapshotManager()
	if err != nil {
		return err
	}
	meta, err := snapMgr.Find(dbName, ref)
	if err != nil {
		return err
	}

	if !snapForce {
		var confirm bool
		prompt := &survey.Confirm{
			Message: color.RedString("Delete snapshot '%s' (%s) for '%s'?", meta.Name, meta.ID, dbName),
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

	if _, err := snapMgr.Delete(dbName, meta.ID); err != nil {
		return err
	}
	color.Green("Deleted snapshot '%s' (%s)\n", meta.Name, meta.ID)
	return nil
}

func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func resolveManifestFlag(flagPath string) (string, error) {
	if p := strings.TrimSpace(flagPath); p != "" {
		return p, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return projectManifestPath(cwd)
}
