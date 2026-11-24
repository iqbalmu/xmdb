package table

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var describeCmd = &cobra.Command{
	Use:     "describe <database-name> <table-name>",
	Aliases: []string{"desc"},
	Short:   "Describe table structure",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDescribe(args[0], args[1])
	},
}

func runDescribe(dbName, tableName string) error {
	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	dbState, err := stateMgr.Load(dbName)
	if err != nil {
		return fmt.Errorf("database '%s' not found", dbName)
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

	var rows *sql.Rows
	if dbState.Provider == utils.ProviderMySQL {
		rows, err = db.QueryContext(ctx, fmt.Sprintf("DESCRIBE %s", providers.QuoteIdentMySQL(tableName)))
	} else {
		schema, tbl := providers.SplitTablePG(tableName)
		rows, err = db.QueryContext(ctx, fmt.Sprintf(`
			SELECT column_name, data_type, is_nullable, column_default
			FROM information_schema.columns
			WHERE table_schema = %s AND table_name = %s
			ORDER BY ordinal_position
		`, providers.QuoteLiteralPG(schema), providers.QuoteLiteralPG(tbl)))
	}

	if err != nil {
		return fmt.Errorf("describing table: %w", err)
	}
	defer rows.Close()

	color.Cyan("\nTable Structure: %s.%s\n", dbName, tableName)

	var data [][]any
	table := tablewriter.NewTable(os.Stdout)

	if dbState.Provider == utils.ProviderMySQL {
		table.Header([]string{"Field", "Type", "Null", "Key", "Default", "Extra"})

		for rows.Next() {
			var field, typ, null, key, extra string
			var def sql.NullString

			if err := rows.Scan(&field, &typ, &null, &key, &def, &extra); err != nil {
				continue
			}

			defaultVal := "NULL"
			if def.Valid {
				defaultVal = def.String
			}

			data = append(data, []any{field, typ, null, key, defaultVal, extra})
		}

	} else {
		table.Header([]string{"Column", "Type", "Nullable", "Default"})

		for rows.Next() {
			var column, dataType, nullable string
			var def sql.NullString

			if err := rows.Scan(&column, &dataType, &nullable, &def); err != nil {
				continue
			}

			defaultVal := "NULL"
			if def.Valid {
				defaultVal = def.String
			}

			data = append(data, []any{column, dataType, nullable, defaultVal})
		}
	}

	table.Bulk(data)
	table.Render()

	fmt.Println()
	return nil
}
