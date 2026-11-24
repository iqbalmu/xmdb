package commands

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

var (
	execSQL  string
	execFile string
	execJSON bool
)

var execCmd = &cobra.Command{
	Use:   "exec <database-name>",
	Short: "Execute SQL query",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExec(args[0])
	},
}

func init() {
	execCmd.Flags().StringVar(&execSQL, "sql", "", "SQL statement to execute")
	execCmd.Flags().StringVar(&execFile, "file", "", "SQL script file to execute")
	execCmd.Flags().BoolVar(&execJSON, "json", false, "Output as JSON (for --sql)")
}

func runExec(name string) error {
	if (execSQL == "") == (execFile == "") {
		return fmt.Errorf("specify exactly one of --sql or --file")
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		return err
	}

	dbState, err := stateMgr.Load(name)
	if err != nil {
		return fmt.Errorf("database '%s' not found", name)
	}

	if execFile != "" {
		// Script files carry many statements; they run through the engine's
		// own client inside the container, which supports them everywhere.
		return runExecFile(dbState)
	}

	return runExecSQL(dbState)
}

// runExecFile streams a script through the database client inside the
// container, so multi-statement files work on both providers and dumps of any
// size pass through without buffering.
func runExecFile(dbState *state.Database) error {
	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return err
	}

	ctx := context.Background()

	running, err := dockerMgr.IsContainerRunning(ctx, dbState.ContainerID, dbState.ContainerName)
	if err != nil || !running {
		return fmt.Errorf("database '%s' is not running", dbState.Name)
	}

	sqlFile, err := os.Open(execFile)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer sqlFile.Close()

	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return err
	}
	snapProv, ok := providers.AsSnapshot(prov)
	if !ok {
		return fmt.Errorf("provider '%s' does not support executing SQL files", dbState.Provider)
	}
	cmd := snapProv.RestoreCommand(&providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	})

	color.Cyan("Running %s on '%s'...\n", execFile, dbState.Name)

	res, err := dockerMgr.Exec(ctx, dbState.ContainerID, dbState.ContainerName, cmd, sqlFile, os.Stdout)
	if err != nil {
		return fmt.Errorf("executing file: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("executing %s (exit %d):\n%s", execFile, res.ExitCode, res.Stderr)
	}
	if res.Stderr != "" {
		color.Yellow("Warnings:\n%s", res.Stderr)
	}
	return nil
}

// runExecSQL executes a single statement over a direct connection, printing
// result rows as a table or the affected row count.
func runExecSQL(dbState *state.Database) error {
	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return err
	}
	sqlProv, ok := providers.AsSQL(prov)
	if !ok {
		return fmt.Errorf("provider '%s' does not support SQL queries", dbState.Provider)
	}

	db, err := sqlProv.Connect(&providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	})
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()

	ctx := context.Background()

	if isRowQuery(execSQL) {
		rows, err := db.QueryContext(ctx, execSQL)
		if err != nil {
			// Detection is a guess; let the exec path try before failing.
			if res, execErr := db.ExecContext(ctx, execSQL); execErr == nil {
				return printExecResult(res)
			}
			return fmt.Errorf("executing query: %w", err)
		}
		defer rows.Close()
		return printQueryRows(rows)
	}

	res, err := db.ExecContext(ctx, execSQL)
	if err != nil {
		// Some row-returning statements slip past detection (e.g. CALL);
		// fall back to the query path so their rows still print.
		if rows, queryErr := db.QueryContext(ctx, execSQL); queryErr == nil {
			defer rows.Close()
			return printQueryRows(rows)
		}
		return fmt.Errorf("executing statement: %w", err)
	}
	return printExecResult(res)
}

// isRowQuery guesses whether a statement returns rows from its first
// keyword. DDL and DML fall through to Exec so affected rows are reported.
func isRowQuery(stmt string) bool {
	s := strings.TrimSpace(stmt)
	for {
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = strings.TrimSpace(s[i+1:])
				continue
			}
			return false
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = strings.TrimSpace(s[i+2:])
				continue
			}
			return false
		}
		break
	}
	if s == "" {
		return false
	}

	fields := strings.Fields(s)
	switch strings.ToUpper(fields[0]) {
	case "SELECT", "WITH", "SHOW", "EXPLAIN", "DESCRIBE", "DESC", "TABLE", "VALUES":
		return true
	}
	return false
}

func printQueryRows(rows *sql.Rows) error {
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("getting columns: %w", err)
	}

	values := make([]any, len(columns))
	valuePtrs := make([]any, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var data [][]any
	var jsonRows []map[string]any
	var scanErr error
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			if scanErr == nil {
				scanErr = err
			}
			continue
		}

		if execJSON {
			obj := make(map[string]any, len(columns))
			for i, col := range columns {
				val := values[i]
				if b, ok := val.([]byte); ok {
					obj[col] = string(b)
				} else {
					obj[col] = val
				}
			}
			jsonRows = append(jsonRows, obj)
			continue
		}

		row := make([]any, len(columns))
		for i, val := range values {
			if val == nil {
				row[i] = "NULL"
			} else if b, ok := val.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = val
			}
		}
		data = append(data, row)
	}
	if scanErr != nil {
		color.Yellow("Warning: some rows could not be read: %v", scanErr)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading rows: %w", err)
	}

	if execJSON {
		if jsonRows == nil {
			jsonRows = []map[string]any{}
		}
		return printJSON(struct {
			Columns  []string         `json:"columns"`
			Rows     []map[string]any `json:"rows"`
			RowCount int              `json:"row_count"`
		}{
			Columns:  columns,
			Rows:     jsonRows,
			RowCount: len(jsonRows),
		})
	}

	fmt.Println()

	table := tablewriter.NewTable(os.Stdout)
	table.Header(columns)
	table.Bulk(data)
	table.Render()

	color.Green("\n%d rows returned\n", len(data))
	return nil
}

func printExecResult(res sql.Result) error {
	affected, err := res.RowsAffected()
	if execJSON {
		out := map[string]any{"status": "ok"}
		if err == nil {
			out["rows_affected"] = affected
		}
		return printJSON(out)
	}

	color.Green("\nStatement executed successfully")
	if err == nil {
		fmt.Printf("  Rows affected: %d\n\n", affected)
		return nil
	}
	fmt.Println()
	return nil
}
