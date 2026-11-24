package table

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list <database-name>",
	Aliases: []string{"ls"},
	Short:   "List all tables in a database",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runList(args[0])
	},
}

func runList(name string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	dbState, err := stateMgr.Load(name)
	if err != nil {
		return fmt.Errorf("database '%s' not found", name)
	}

	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return err
	}
	sqlProv, ok := providers.AsSQL(prov)
	if !ok {
		return fmt.Errorf("provider '%s' does not have SQL tables", dbState.Provider)
	}

	dbConfig := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	}

	db, err := sqlProv.Connect(dbConfig)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()

	ctx := context.Background()

	tables, err := sqlProv.ListTables(ctx, db, dbState.DatabaseName)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}

	if len(tables) == 0 {
		color.Yellow("\nNo tables found in database '%s'", name)
		fmt.Println()
		return nil
	}

	color.Cyan("\nTables in '%s':\n", name)
	for i, table := range tables {
		fmt.Printf("  %d. %s\n", i+1, table)
	}
	fmt.Println()
	return nil
}
