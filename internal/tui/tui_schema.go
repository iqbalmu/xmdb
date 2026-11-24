package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/iqbalmu/xmdb/internal/providers"
)

func fetchTableSchemaGraph(ctx context.Context, conn *sql.DB, provider, dbName string, tables []string) *tableSchemaGraph {
	graph := &tableSchemaGraph{
		tables: make([]tableOverview, len(tables)),
		byName: make(map[string]*tableOverview, len(tables)),
	}
	for i, t := range tables {
		graph.tables[i] = tableOverview{name: t}
		graph.byName[t] = &graph.tables[i]
	}
	if len(tables) == 0 || conn == nil {
		return graph
	}

	if providers.IsMySQLFamily(provider) {
		colRows, err := conn.QueryContext(ctx, `
			SELECT table_name, column_name, column_type, is_nullable, column_key, COALESCE(column_default, '')
			FROM information_schema.columns
			WHERE table_schema = ?
			ORDER BY table_name, ordinal_position`, dbName)
		if err == nil {
			defer colRows.Close()
			for colRows.Next() {
				var tbl, col, dtype, nullable, colKey, defVal string
				if err := colRows.Scan(&tbl, &col, &dtype, &nullable, &colKey, &defVal); err != nil {
					continue
				}
				t := graph.byName[tbl]
				if t == nil {
					continue
				}
				key := ""
				isUniq := false
				switch colKey {
				case "PRI":
					key = "PK"
					isUniq = true
				case "UNI":
					key = "UNI"
					isUniq = true
				case "MUL":
					key = "IDX"
				}
				t.columns = append(t.columns, tableColumnInfo{
					name:     col,
					dataType: dtype,
					nullable: strings.EqualFold(nullable, "YES"),
					key:      key,
					unique:   isUniq,
					defVal:   defVal,
				})
			}
		}

		fkRows, err := conn.QueryContext(ctx, `
			SELECT table_name, column_name, referenced_table_name, referenced_column_name
			FROM information_schema.key_column_usage
			WHERE table_schema = ? AND referenced_table_name IS NOT NULL
			ORDER BY table_name, ordinal_position`, dbName)
		if err == nil {
			defer fkRows.Close()
			for fkRows.Next() {
				var fromTbl, fromCol, toTbl, toCol string
				if err := fkRows.Scan(&fromTbl, &fromCol, &toTbl, &toCol); err != nil {
					continue
				}
				graph.relations = append(graph.relations, tableRelation{
					fromTable: fromTbl,
					fromCol:   fromCol,
					toTable:   toTbl,
					toCol:     toCol,
				})
				markColumnFK(graph.byName[fromTbl], fromCol)
			}
		}
		finalizeRelationCardinality(graph)
		return graph
	}

	// PostgreSQL
	colRows, err := conn.QueryContext(ctx, `
		SELECT
			CASE WHEN c.table_schema = 'public' THEN c.table_name ELSE c.table_schema || '.' || c.table_name END AS tbl,
			c.column_name,
			c.data_type,
			c.is_nullable,
			COALESCE(c.column_default, '')
		FROM information_schema.columns c
		JOIN information_schema.tables t
			ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema NOT IN ('pg_catalog', 'information_schema')
			AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_schema = 'public' DESC, c.table_schema, c.table_name, c.ordinal_position`)
	if err == nil {
		defer colRows.Close()
		for colRows.Next() {
			var tbl, col, dtype, nullable, defVal string
			if err := colRows.Scan(&tbl, &col, &dtype, &nullable, &defVal); err != nil {
				continue
			}
			t := graph.byName[tbl]
			if t == nil {
				continue
			}
			t.columns = append(t.columns, tableColumnInfo{
				name:     col,
				dataType: dtype,
				nullable: strings.EqualFold(nullable, "YES"),
				defVal:   defVal,
			})
		}
	}

	constRows, err := conn.QueryContext(ctx, `
		SELECT
			tc.constraint_type,
			CASE WHEN tc.table_schema = 'public' THEN tc.table_name ELSE tc.table_schema || '.' || tc.table_name END AS from_tbl,
			kcu.column_name AS from_col,
			COALESCE(CASE WHEN ccu.table_schema = 'public' THEN ccu.table_name ELSE ccu.table_schema || '.' || ccu.table_name END, '') AS to_tbl,
			COALESCE(ccu.column_name, '') AS to_col
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		LEFT JOIN information_schema.constraint_column_usage ccu
			ON tc.constraint_type = 'FOREIGN KEY' AND tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
		WHERE tc.table_schema NOT IN ('pg_catalog', 'information_schema')
			AND tc.constraint_type IN ('PRIMARY KEY', 'FOREIGN KEY', 'UNIQUE')
		ORDER BY from_tbl, kcu.ordinal_position`)
	if err == nil {
		defer constRows.Close()
		for constRows.Next() {
			var ctype, fromTbl, fromCol, toTbl, toCol string
			if err := constRows.Scan(&ctype, &fromTbl, &fromCol, &toTbl, &toCol); err != nil {
				continue
			}
			if ctype == "PRIMARY KEY" {
				markColumnPK(graph.byName[fromTbl], fromCol)
			} else if ctype == "UNIQUE" {
				markColumnUnique(graph.byName[fromTbl], fromCol)
			} else if ctype == "FOREIGN KEY" && toTbl != "" {
				graph.relations = append(graph.relations, tableRelation{
					fromTable: fromTbl,
					fromCol:   fromCol,
					toTable:   toTbl,
					toCol:     toCol,
				})
				markColumnFK(graph.byName[fromTbl], fromCol)
			}
		}
	}

	finalizeRelationCardinality(graph)
	return graph
}

func markColumnPK(t *tableOverview, colName string) {
	if t == nil {
		return
	}
	for i := range t.columns {
		if t.columns[i].name == colName {
			t.columns[i].unique = true
			if t.columns[i].key == "FK" {
				t.columns[i].key = "PK,FK"
			} else if t.columns[i].key != "PK,FK" {
				t.columns[i].key = "PK"
			}
		}
	}
}

func markColumnUnique(t *tableOverview, colName string) {
	if t == nil {
		return
	}
	for i := range t.columns {
		if t.columns[i].name == colName {
			t.columns[i].unique = true
			if t.columns[i].key == "" {
				t.columns[i].key = "UNI"
			}
		}
	}
}

func finalizeRelationCardinality(graph *tableSchemaGraph) {
	if graph == nil {
		return
	}
	for i := range graph.relations {
		rel := &graph.relations[i]
		if t := graph.byName[rel.fromTable]; t != nil {
			for _, c := range t.columns {
				if c.name == rel.fromCol && (c.unique || c.key == "PK" || c.key == "PK,FK" || c.key == "UNI") {
					rel.isOneToOne = true
					break
				}
			}
		}
	}
}

func markColumnFK(t *tableOverview, colName string) {
	if t == nil {
		return
	}
	for i := range t.columns {
		if t.columns[i].name == colName {
			if t.columns[i].key == "PK" {
				t.columns[i].key = "PK,FK"
			} else if t.columns[i].key != "PK,FK" {
				t.columns[i].key = "FK"
			}
		}
	}
}

func formatTableOverviewList(tables []string, graph *tableSchemaGraph, cursor int, focused bool, showFields bool, maxWidth int) (string, int) {
	var b strings.Builder
	lineNo := 0
	cursorLine := 0

	for i, t := range tables {
		var cols []tableColumnInfo
		if graph != nil {
			if ov := graph.byName[t]; ov != nil {
				cols = ov.columns
			}
		}

		countSuffix := ""
		if len(cols) > 0 {
			countSuffix = fmt.Sprintf(" (%d fields)", len(cols))
		}

		prefix := "  "
		if focused && i == cursor {
			prefix = "▸ "
		}
		if i == cursor {
			cursorLine = lineNo
		}

		header := fmt.Sprintf("%s%d. %s%s", prefix, i+1, t, countSuffix)
		if maxWidth > 4 && lipgloss.Width(header) > maxWidth {
			header = ansi.Truncate(header, maxWidth-1, "…")
		}
		if focused && i == cursor {
			header = styleSelectedRow.Render(header)
		}
		b.WriteString(header + "\n")
		lineNo++

		if showFields && len(cols) > 0 {
			for _, col := range cols {
				badge := "   "
				if col.key != "" {
					badge = fmt.Sprintf("[%-2s]", col.key)
				}
				line := fmt.Sprintf("     • %s %-18s %s", badge, col.name, col.dataType)
				if maxWidth > 4 && lipgloss.Width(line) > maxWidth {
					line = ansi.Truncate(line, maxWidth-1, "…")
				}
				b.WriteString(styleMutedText.Render(line) + "\n")
				lineNo++
			}
		}
	}

	return b.String(), cursorLine
}
