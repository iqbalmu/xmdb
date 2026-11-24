package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

const defaultExplorerPageSize = 50

// tableExplorerOpts controls pagination, filtering, and sorting in the Data Explorer.
type tableExplorerOpts struct {
	Page     int
	PageSize int
	Filter   string
	SortCol  string
	SortDesc bool
}

func (o tableExplorerOpts) normalizedPageSize() int {
	if o.PageSize <= 0 {
		return defaultExplorerPageSize
	}
	return o.PageSize
}

func fetchTableDetailWithOpts(ctx context.Context, runtimeMode string, dbState *state.Database, tableName string, opts tableExplorerOpts) (*tableDetail, error) {
	pageSize := opts.normalizedPageSize()
	if opts.Page < 0 {
		opts.Page = 0
	}
	offset := opts.Page * pageSize

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
		b.WriteString(prettyMaybeJSON(kvDetail.Value))
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
		filter := strings.TrimSpace(opts.Filter)
		if filter == "" {
			filter = "{}"
		}
		var out bytes.Buffer
		res, err := dockerMgr.Exec(ctx, dbState.ContainerID, dbState.ContainerName,
			docProv.SampleCollectionCommand(adminCfg, tableName, pageSize, offset, filter), nil, &out)
		if err != nil {
			return nil, err
		}
		if res.ExitCode != 0 {
			return nil, fmt.Errorf("sampling collection failed: %s", strings.TrimSpace(res.Stderr))
		}
		raw := strings.TrimSpace(out.String())
		var parsed struct {
			Total int64             `json:"total"`
			Docs  []json.RawMessage `json:"docs"`
		}
		detail := &tableDetail{
			tableName: tableName,
			kind:      providers.EngineDocument,
			page:      opts.Page,
			pageSize:  pageSize,
			filter:    opts.Filter,
			rawText:   raw,
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			detail.totalRows = parsed.Total
			if pretty, err := json.MarshalIndent(parsed.Docs, "", "  "); err == nil {
				detail.rawText = string(pretty)
			}
		}
		return detail, nil
	}

	_, conn, err := openDBConn(dbState)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	detail := &tableDetail{
		tableName: tableName,
		kind:      providers.EngineSQL,
		page:      opts.Page,
		pageSize:  pageSize,
		filter:    strings.TrimSpace(opts.Filter),
		sortCol:   opts.SortCol,
		sortDesc:  opts.SortDesc,
	}

	isMySQL := providers.IsMySQLFamily(dbState.Provider)
	var schemaQ, quotedTbl string
	if isMySQL {
		quotedTbl = providers.QuoteIdentMySQL(tableName)
		schemaQ = fmt.Sprintf("DESCRIBE %s", quotedTbl)
	} else {
		schema, tbl := providers.SplitTablePG(tableName)
		quotedTbl = providers.QuoteTablePG(tableName)
		schemaQ = fmt.Sprintf(`
			SELECT column_name, data_type, is_nullable, COALESCE(column_default, 'NULL')
			FROM information_schema.columns
			WHERE table_schema = %s AND table_name = %s
			ORDER BY ordinal_position`, providers.QuoteLiteralPG(schema), providers.QuoteLiteralPG(tbl))
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

	whereClause := ""
	if detail.filter != "" {
		whereClause = " WHERE " + detail.filter
	}

	orderClause := ""
	if detail.sortCol != "" {
		var quotedCol string
		if isMySQL {
			quotedCol = providers.QuoteIdentMySQL(detail.sortCol)
		} else {
			quotedCol = providers.QuoteIdentPG(detail.sortCol)
		}
		dir := "ASC"
		if detail.sortDesc {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf(" ORDER BY %s %s", quotedCol, dir)
	}

	countQ := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", quotedTbl, whereClause)
	var total int64
	if err := conn.QueryRowContext(ctx, countQ).Scan(&total); err == nil {
		detail.totalRows = total
	} else if detail.filter != "" {
		return nil, fmt.Errorf("filter error: %w", err)
	}

	previewQ := fmt.Sprintf("SELECT * FROM %s%s%s LIMIT %d OFFSET %d", quotedTbl, whereClause, orderClause, pageSize, offset)
	pRows, err := conn.QueryContext(ctx, previewQ)
	if err != nil {
		return nil, fmt.Errorf("querying table rows: %w", err)
	}
	detail.previewCols, detail.previewRows, err = scanAllRows(pRows, pageSize)
	pRows.Close()
	if err != nil {
		return nil, err
	}

	return detail, nil
}

// prettyMaybeJSON formats s as indented JSON if it is a valid JSON object or array.
func prettyMaybeJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

// formatRowAsJSON builds a pretty-printed JSON object from a single row.
func formatRowAsJSON(cols []string, row []string) string {
	obj := make(map[string]any, len(cols))
	for i, c := range cols {
		val := ""
		if i < len(row) {
			val = row[i]
		}
		if val == "NULL" {
			obj[c] = nil
			continue
		}
		trimmed := strings.TrimSpace(val)
		if len(trimmed) >= 2 && (trimmed[0] == '{' || trimmed[0] == '[') {
			var parsed any
			if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
				obj[c] = parsed
				continue
			}
		}
		obj[c] = val
	}
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

// formatRowInspectorText formats a row for the Row / JSON Inspector modal.
func formatRowInspectorText(cols []string, row []string) string {
	var b strings.Builder
	for i, c := range cols {
		val := ""
		if i < len(row) {
			val = row[i]
		}
		pretty := prettyMaybeJSON(val)
		b.WriteString(styleLabel.Render(c + ":"))
		if strings.Contains(pretty, "\n") {
			b.WriteString("\n")
			for _, line := range strings.Split(pretty, "\n") {
				b.WriteString("  " + line + "\n")
			}
		} else {
			b.WriteString(" " + pretty + "\n")
		}
	}
	return b.String()
}
