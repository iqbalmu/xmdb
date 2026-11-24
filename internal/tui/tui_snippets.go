package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/iqbalmu/xmdb/internal/providers"
)

const savedQueriesFileName = "queries.json"

// SavedQuery represents a user-saved query or command snippet.
type SavedQuery struct {
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Database  string    `json:"database,omitempty"`
	Query     string    `json:"query"`
	CreatedAt time.Time `json:"created_at"`
}

func savedQueriesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".xmdb", savedQueriesFileName), nil
}

func loadSavedQueriesFrom(path string) []SavedQuery {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []SavedQuery
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func saveSavedQueriesTo(path string, items []SavedQuery) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadSavedQueries() []SavedQuery {
	p, err := savedQueriesPath()
	if err != nil {
		return nil
	}
	return loadSavedQueriesFrom(p)
}

func saveSavedQueries(items []SavedQuery) error {
	p, err := savedQueriesPath()
	if err != nil {
		return err
	}
	return saveSavedQueriesTo(p, items)
}

func upsertSavedQuery(items []SavedQuery, q SavedQuery) []SavedQuery {
	q.Name = strings.TrimSpace(q.Name)
	q.Query = strings.TrimSpace(q.Query)
	if q.Name == "" || q.Query == "" {
		return items
	}
	if q.CreatedAt.IsZero() {
		q.CreatedAt = time.Now().UTC()
	}
	for i := range items {
		if strings.EqualFold(items[i].Name, q.Name) && items[i].Provider == q.Provider {
			items[i] = q
			return items
		}
	}
	items = append(items, q)
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

func deleteSavedQuery(items []SavedQuery, idx int) []SavedQuery {
	if idx < 0 || idx >= len(items) {
		return items
	}
	return append(items[:idx], items[idx+1:]...)
}

func filterSavedQueries(items []SavedQuery, provider, dbName string) []SavedQuery {
	var out []SavedQuery
	for _, q := range items {
		if q.Provider != "" && provider != "" && q.Provider != provider {
			continue
		}
		if q.Database != "" && dbName != "" && q.Database != dbName {
			continue
		}
		out = append(out, q)
	}
	return out
}

// buildExplainQuery wraps a SQL query with the engine-appropriate EXPLAIN statement.
func buildExplainQuery(provider, query string) (string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", fmt.Errorf("write a query before running EXPLAIN")
	}
	prov, err := providers.For(provider)
	if err != nil {
		return "", err
	}
	if prov.Kind() != providers.EngineSQL {
		return "", fmt.Errorf("EXPLAIN is only supported for SQL providers")
	}
	upper := strings.ToUpper(q)
	if strings.HasPrefix(upper, "EXPLAIN") {
		return q, nil
	}
	if providers.IsMySQLFamily(provider) {
		return "EXPLAIN " + q, nil
	}
	return "EXPLAIN (ANALYZE, BUFFERS) " + q, nil
}

// --- Lightweight SQL syntax highlighter ---

var sqlKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "JOIN": true, "LEFT": true,
	"RIGHT": true, "INNER": true, "OUTER": true, "FULL": true, "CROSS": true,
	"ON": true, "AS": true, "AND": true, "OR": true, "NOT": true, "IN": true,
	"IS": true, "NULL": true, "TRUE": true, "FALSE": true, "ORDER": true,
	"BY": true, "GROUP": true, "HAVING": true, "LIMIT": true, "OFFSET": true,
	"ASC": true, "DESC": true, "DISTINCT": true, "UNION": true, "ALL": true,
	"INSERT": true, "INTO": true, "VALUES": true, "UPDATE": true, "SET": true,
	"DELETE": true, "CREATE": true, "TABLE": true, "INDEX": true, "ALTER": true,
	"DROP": true, "WITH": true, "EXPLAIN": true, "ANALYZE": true, "BUFFERS": true,
	"CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true,
	"LIKE": true, "ILIKE": true, "BETWEEN": true, "EXISTS": true, "RETURNING": true,
}

var (
	styleSQLKeyword = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	styleSQLString  = lipgloss.NewStyle().Foreground(colorSuccess)
	styleSQLNumber  = lipgloss.NewStyle().Foreground(colorWarning)
	styleSQLComment = lipgloss.NewStyle().Foreground(colorMuted)
)

// highlightSQL colorizes SQL keywords, strings, numbers, and comments using lipgloss.
func highlightSQL(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(src)
	n := len(runes)
	i := 0
	for i < n {
		r := runes[i]

		// Line comment --
		if r == '-' && i+1 < n && runes[i+1] == '-' {
			start := i
			for i < n && runes[i] != '\n' {
				i++
			}
			b.WriteString(styleSQLComment.Render(string(runes[start:i])))
			continue
		}

		// String literal '...'
		if r == '\'' {
			start := i
			i++
			for i < n {
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteString(styleSQLString.Render(string(runes[start:i])))
			continue
		}

		// Word / keyword
		if unicode.IsLetter(r) || r == '_' {
			start := i
			for i < n && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			word := string(runes[start:i])
			if sqlKeywords[strings.ToUpper(word)] {
				b.WriteString(styleSQLKeyword.Render(word))
			} else {
				b.WriteString(word)
			}
			continue
		}

		// Number literal
		if unicode.IsDigit(r) {
			start := i
			for i < n && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				i++
			}
			b.WriteString(styleSQLNumber.Render(string(runes[start:i])))
			continue
		}

		b.WriteRune(r)
		i++
	}
	return b.String()
}
