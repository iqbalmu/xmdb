package tui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

const (
	maxQueryHistory      = 200
	queryHistoryFileName = "query_history.json"
)

func queryHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".xmdb", queryHistoryFileName), nil
}

func loadQueryHistory() []string {
	p, err := queryHistoryPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var history []string
	if err := json.Unmarshal(data, &history); err != nil {
		return nil
	}
	return history
}

func saveQueryHistory(history []string) error {
	p, err := queryHistoryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// recordQueryHistory appends query to history, deduplicating earlier identical
// entries and capping the slice at maxQueryHistory.
func recordQueryHistory(history []string, query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return history
	}
	out := make([]string, 0, len(history)+1)
	for _, item := range history {
		if item != q {
			out = append(out, item)
		}
	}
	out = append(out, q)
	if len(out) > maxQueryHistory {
		out = out[len(out)-maxQueryHistory:]
	}
	return out
}

func formatQueryResultJSON(res *sqlQueryResult) ([]byte, error) {
	if res == nil || !res.isSelect {
		return nil, fmt.Errorf("no tabular query result to format")
	}
	records := make([]map[string]string, 0, len(res.rows))
	for _, row := range res.rows {
		obj := make(map[string]string, len(res.cols))
		for i, col := range res.cols {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			obj[col] = val
		}
		records = append(records, obj)
	}
	return json.MarshalIndent(records, "", "  ")
}

func exportQueryResult(res *sqlQueryResult, format, outPath string) (string, error) {
	if res == nil || !res.isSelect {
		return "", fmt.Errorf("no tabular query result to export")
	}
	trimmed := strings.TrimSpace(outPath)
	if trimmed == "" {
		return "", fmt.Errorf("output path cannot be empty")
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	switch strings.ToLower(format) {
	case "json":
		data, err := formatQueryResultJSON(res)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(absPath, append(data, '\n'), 0o600); err != nil {
			return "", fmt.Errorf("writing JSON file: %w", err)
		}
		return absPath, nil

	case "csv":
		f, err := os.OpenFile(absPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return "", fmt.Errorf("creating CSV file: %w", err)
		}
		w := csv.NewWriter(f)
		if err := w.Write(res.cols); err != nil {
			f.Close()
			return "", fmt.Errorf("writing CSV header: %w", err)
		}
		for _, row := range res.rows {
			if err := w.Write(row); err != nil {
				f.Close()
				return "", fmt.Errorf("writing CSV row: %w", err)
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			f.Close()
			return "", fmt.Errorf("flushing CSV file: %w", err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("closing CSV file: %w", err)
		}
		return absPath, nil

	default:
		return "", fmt.Errorf("unsupported export format %q (use csv or json)", format)
	}
}

func buildTableSelectQuery(provider, tableName string) string {
	if provider == utils.ProviderMySQL {
		return fmt.Sprintf("SELECT * FROM %s LIMIT 50;", providers.QuoteIdentMySQL(tableName))
	}
	return fmt.Sprintf("SELECT * FROM %s LIMIT 50;", providers.QuoteTablePG(tableName))
}
