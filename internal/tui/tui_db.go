package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

type tableDetail struct {
	tableName   string
	schemaCols  []string
	schemaRows  [][]string
	previewCols []string
	previewRows [][]string
}

type tableColumnInfo struct {
	name     string
	dataType string
	nullable bool
	key      string // "PK", "FK", "PK,FK", "UNI", or ""
	unique   bool
	defVal   string
}

type tableRelation struct {
	fromTable  string
	fromCol    string
	toTable    string
	toCol      string
	isOneToOne bool
}

type tableOverview struct {
	name    string
	columns []tableColumnInfo
}

type tableSchemaGraph struct {
	tables    []tableOverview
	byName    map[string]*tableOverview
	relations []tableRelation
}

type sqlQueryResult struct {
	query    string
	cols     []string
	rows     [][]string
	affected int64
	isSelect bool
	duration time.Duration
}

func openDBConn(dbState *state.Database) (providers.Provider, *sql.DB, error) {
	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return nil, nil, err
	}
	conn, err := prov.Connect(&providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	})
	if err != nil {
		return nil, nil, err
	}
	return prov, conn, nil
}

func fetchTableDetail(ctx context.Context, dbState *state.Database, tableName string) (*tableDetail, error) {
	_, conn, err := openDBConn(dbState)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	detail := &tableDetail{tableName: tableName}

	var schemaQ string
	var previewQ string
	if dbState.Provider == utils.ProviderMySQL {
		quoted := providers.QuoteIdentMySQL(tableName)
		schemaQ = fmt.Sprintf("DESCRIBE %s", quoted)
		previewQ = fmt.Sprintf("SELECT * FROM %s LIMIT 25", quoted)
	} else {
		schema, tbl := providers.SplitTablePG(tableName)
		schemaQ = fmt.Sprintf(`
			SELECT column_name, data_type, is_nullable, COALESCE(column_default, 'NULL')
			FROM information_schema.columns
			WHERE table_schema = %s AND table_name = %s
			ORDER BY ordinal_position`, providers.QuoteLiteralPG(schema), providers.QuoteLiteralPG(tbl))
		previewQ = fmt.Sprintf("SELECT * FROM %s LIMIT 25", providers.QuoteTablePG(tableName))
	}

	sRows, err := conn.QueryContext(ctx, schemaQ)
	if err != nil {
		return nil, fmt.Errorf("describing table: %w", err)
	}
	detail.schemaCols, detail.schemaRows, err = scanAllRows(sRows, 200)
	sRows.Close()
	if err != nil {
		return nil, err
	}

	pRows, err := conn.QueryContext(ctx, previewQ)
	if err == nil {
		detail.previewCols, detail.previewRows, _ = scanAllRows(pRows, 25)
		pRows.Close()
	}

	return detail, nil
}

func executeQuickSQL(ctx context.Context, dbState *state.Database, query string) (*sqlQueryResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}

	_, conn, err := openDBConn(dbState)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	upper := strings.ToUpper(q)
	isSelect := strings.HasPrefix(upper, "SELECT") ||
		strings.HasPrefix(upper, "SHOW") ||
		strings.HasPrefix(upper, "DESCRIBE") ||
		strings.HasPrefix(upper, "DESC ") ||
		strings.HasPrefix(upper, "EXPLAIN") ||
		strings.HasPrefix(upper, "WITH")

	res := &sqlQueryResult{
		query:    q,
		isSelect: isSelect,
	}
	start := time.Now()

	if isSelect {
		rows, err := conn.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		res.cols, res.rows, err = scanAllRows(rows, 200)
		if err != nil {
			return nil, err
		}
		res.duration = time.Since(start)
		return res, nil
	}

	execRes, err := conn.ExecContext(ctx, q)
	if err != nil {
		return nil, err
	}
	affected, _ := execRes.RowsAffected()
	res.affected = affected
	res.duration = time.Since(start)
	return res, nil
}

func scanAllRows(rows *sql.Rows, maxRows int) ([]string, [][]string, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]string
	for rows.Next() {
		if maxRows > 0 && len(out) >= maxRows {
			break
		}
		vals := make([]sql.RawBytes, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			if v == nil {
				row[i] = "NULL"
			} else {
				s := strings.ReplaceAll(string(v), "\n", " ")
				s = strings.ReplaceAll(s, "\r", "")
				row[i] = s
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return cols, out, nil
}

func formatCompactTable(cols []string, rows [][]string, maxWidth int) string {
	out, _ := formatCompactTableWithOffset(cols, rows, maxWidth, 0)
	return out
}

func formatCompactTableWithOffset(cols []string, rows [][]string, maxWidth, xOffset int) (string, int) {
	if len(cols) == 0 {
		return styleMutedText.Render("(no columns)"), 0
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = lipgloss.Width(c)
	}
	for _, r := range rows {
		for i := range cols {
			if i < len(r) {
				if w := lipgloss.Width(r[i]); w > widths[i] {
					widths[i] = w
				}
			}
		}
	}
	const maxColW = 28
	for i := range widths {
		if widths[i] > maxColW {
			widths[i] = maxColW
		}
		if widths[i] < 3 {
			widths[i] = 3
		}
	}

	renderRow := func(cells []string) string {
		parts := make([]string, len(cols))
		for i := range cols {
			val := ""
			if i < len(cells) {
				val = cells[i]
			}
			if lipgloss.Width(val) > widths[i] {
				val = ansi.Truncate(val, widths[i]-1, "…")
			}
			pad := widths[i] - lipgloss.Width(val)
			if pad > 0 {
				val += strings.Repeat(" ", pad)
			}
			parts[i] = val
		}
		return strings.Join(parts, " │ ")
	}

	sliceLine := func(line string) string {
		if maxWidth <= 0 {
			return line
		}
		total := lipgloss.Width(line)
		if xOffset <= 0 && total <= maxWidth {
			return line
		}
		start := max(0, xOffset)
		if start >= total {
			return ""
		}
		end := start + maxWidth
		sliced := ansi.Cut(line, start, end)
		if end < total && maxWidth > 1 {
			sliced = ansi.Cut(line, start, end-1) + "…"
		}
		return sliced
	}

	headerLine := renderRow(cols)
	totalWidth := lipgloss.Width(headerLine)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(sliceLine(headerLine)) + "\n")

	sepParts := make([]string, len(cols))
	for i, w := range widths {
		sepParts[i] = strings.Repeat("─", w)
	}
	sep := strings.Join(sepParts, "─┼─")
	b.WriteString(styleMutedText.Render(sliceLine(sep)) + "\n")

	if len(rows) == 0 {
		b.WriteString(styleMutedText.Render("(0 rows)") + "\n")
		return b.String(), totalWidth
	}
	for _, r := range rows {
		b.WriteString(sliceLine(renderRow(r)) + "\n")
	}
	return b.String(), totalWidth
}
