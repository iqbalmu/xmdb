package tui

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

type docCollectionInfo struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type tableDetail struct {
	tableName   string
	kind        providers.EngineKind
	schemaCols  []string
	schemaRows  [][]string
	previewCols []string
	previewRows [][]string
	rawText     string
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
	query     string
	cols      []string
	rows      [][]string
	affected  int64
	isSelect  bool
	rawOutput string
	duration  time.Duration
}

func openDBConn(dbState *state.Database) (providers.SQLProvider, *sql.DB, error) {
	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return nil, nil, err
	}
	sqlProv, ok := providers.AsSQL(prov)
	if !ok {
		return nil, nil, fmt.Errorf("provider '%s' does not support SQL connections", dbState.Provider)
	}
	conn, err := sqlProv.Connect(&providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	})
	if err != nil {
		return nil, nil, err
	}
	return sqlProv, conn, nil
}

func fetchTableDetail(ctx context.Context, runtimeMode string, dbState *state.Database, tableName string) (*tableDetail, error) {
	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return nil, err
	}

	adminCfg := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	}

	if kvProv, ok := providers.AsKV(prov); ok {
		kvDetail, err := kvProv.GetKeyDetail(ctx, adminCfg, tableName)
		if err != nil {
			return nil, err
		}
		ttlStr := "no expiry"
		if kvDetail.TTL >= 0 {
			ttlStr = fmt.Sprintf("%ds", kvDetail.TTL)
		} else if kvDetail.TTL == -2 {
			ttlStr = "missing"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("Key:   %s\n", kvDetail.Key))
		b.WriteString(fmt.Sprintf("Type:  %s\n", kvDetail.Type))
		b.WriteString(fmt.Sprintf("TTL:   %s\n", ttlStr))
		b.WriteString(fmt.Sprintf("Size:  %d\n\n", kvDetail.Size))
		b.WriteString("Value:\n")
		b.WriteString(kvDetail.Value)
		return &tableDetail{
			tableName: tableName,
			kind:      providers.EngineKV,
			rawText:   b.String(),
		}, nil
	}

	if docProv, ok := providers.AsDocument(prov); ok {
		dockerMgr, err := container.NewDockerManager(runtimeMode)
		if err != nil {
			return nil, err
		}
		var out bytes.Buffer
		res, err := dockerMgr.Exec(ctx, dbState.ContainerID, dbState.ContainerName,
			docProv.SampleCollectionCommand(adminCfg, tableName, 25, 0, "{}"), nil, &out)
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			return nil, fmt.Errorf("sampling collection failed: %s", strings.TrimSpace(res.Stderr))
		}
		return &tableDetail{
			tableName: tableName,
			kind:      providers.EngineDocument,
			rawText:   strings.TrimSpace(out.String()),
		}, nil
	}

	_, conn, err := openDBConn(dbState)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	detail := &tableDetail{tableName: tableName, kind: providers.EngineSQL}

	var schemaQ string
	var previewQ string
	if providers.IsMySQLFamily(dbState.Provider) {
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

func fetchMongoCollections(ctx context.Context, runtimeMode string, dbState *state.Database, docProv providers.DocumentProvider) ([]docCollectionInfo, error) {
	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, err
	}
	adminCfg := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	}
	var out bytes.Buffer
	res, err := dockerMgr.Exec(ctx, dbState.ContainerID, dbState.ContainerName, docProv.ListCollectionsCommand(adminCfg), nil, &out)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("listing collections failed: %s", strings.TrimSpace(res.Stderr))
	}
	raw := strings.TrimSpace(out.String())
	if raw == "" {
		return nil, nil
	}
	var cols []docCollectionInfo
	if err := json.Unmarshal([]byte(raw), &cols); err != nil {
		return nil, fmt.Errorf("parsing collections: %w", err)
	}
	return cols, nil
}

func executeQuickQuery(ctx context.Context, runtimeMode string, dbState *state.Database, query string) (*sqlQueryResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}

	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return nil, err
	}
	adminCfg := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	}

	if kvProv, ok := providers.AsKV(prov); ok {
		out, dur, err := kvProv.ExecKV(ctx, adminCfg, q)
		if err != nil {
			return nil, err
		}
		return &sqlQueryResult{
			query:     q,
			rawOutput: out,
			duration:  dur,
		}, nil
	}

	if docProv, ok := providers.AsDocument(prov); ok {
		dockerMgr, err := container.NewDockerManager(runtimeMode)
		if err != nil {
			return nil, err
		}
		start := time.Now()
		var out bytes.Buffer
		res, err := dockerMgr.Exec(ctx, dbState.ContainerID, dbState.ContainerName, docProv.EvalQueryCommand(adminCfg, q), nil, &out)
		dur := time.Since(start)
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			return nil, fmt.Errorf("%s", strings.TrimSpace(res.Stderr))
		}
		text := strings.TrimSpace(out.String())
		if text == "" {
			text = "(ok)"
		}
		return &sqlQueryResult{
			query:     q,
			rawOutput: text,
			duration:  dur,
		}, nil
	}

	return executeQuickSQL(ctx, dbState, q)
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
