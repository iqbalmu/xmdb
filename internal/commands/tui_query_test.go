package commands

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

func TestQueryHistory_RecordAndPersist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var hist []string
	hist = recordQueryHistory(hist, "  SELECT 1; ")
	hist = recordQueryHistory(hist, "SELECT 2;")
	hist = recordQueryHistory(hist, "SELECT 1;") // deduplicated and moved to end
	hist = recordQueryHistory(hist, "   ")       // ignored

	if len(hist) != 2 || hist[0] != "SELECT 2;" || hist[1] != "SELECT 1;" {
		t.Fatalf("unexpected history: %v", hist)
	}

	if err := saveQueryHistory(hist); err != nil {
		t.Fatalf("saveQueryHistory() error = %v", err)
	}
	loaded := loadQueryHistory()
	if len(loaded) != 2 || loaded[0] != "SELECT 2;" || loaded[1] != "SELECT 1;" {
		t.Fatalf("loadQueryHistory() = %v, want %v", loaded, hist)
	}

	// Cap at maxQueryHistory
	for i := 0; i < maxQueryHistory+25; i++ {
		hist = recordQueryHistory(hist, "SELECT "+string(rune('A'+i%26))+string(rune('0'+i/26)))
	}
	if len(hist) > maxQueryHistory {
		t.Fatalf("history len = %d, want <= %d", len(hist), maxQueryHistory)
	}
}

func TestExportQueryResult_CSVAndJSON(t *testing.T) {
	dir := t.TempDir()
	res := &sqlQueryResult{
		query:    "SELECT id, name FROM users;",
		cols:     []string{"id", "name"},
		rows:     [][]string{{"1", "alice"}, {"2", "bob"}},
		isSelect: true,
		duration: 5 * time.Millisecond,
	}

	csvPath := filepath.Join(dir, "out.csv")
	absCSV, err := exportQueryResult(res, "csv", csvPath)
	if err != nil {
		t.Fatalf("exportQueryResult(csv) error = %v", err)
	}
	f, err := os.Open(absCSV)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[0][0] != "id" || records[1][1] != "alice" || records[2][1] != "bob" {
		t.Fatalf("unexpected CSV records: %v", records)
	}

	jsonPath := filepath.Join(dir, "out.json")
	absJSON, err := exportQueryResult(res, "json", jsonPath)
	if err != nil {
		t.Fatalf("exportQueryResult(json) error = %v", err)
	}
	raw, err := os.ReadFile(absJSON)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(decoded) != 2 || decoded[0]["name"] != "alice" || decoded[1]["id"] != "2" {
		t.Fatalf("unexpected JSON: %v", decoded)
	}

	if _, err := exportQueryResult(nil, "csv", csvPath); err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestBuildTableSelectQuery(t *testing.T) {
	if got := buildTableSelectQuery(utils.ProviderMySQL, "user`s"); got != "SELECT * FROM `user``s` LIMIT 50;" {
		t.Fatalf("mysql query = %q", got)
	}
	if got := buildTableSelectQuery(utils.ProviderPostgres, "app.users"); got != `SELECT * FROM "app"."users" LIMIT 50;` {
		t.Fatalf("postgres query = %q", got)
	}
}

func TestExportModalAndQueryHistoryNavigation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	st := newExportModalState("mydb")
	if st.format != "csv" || !strings.HasSuffix(st.input.Value(), ".csv") {
		t.Fatalf("unexpected initial export state: format=%s val=%s", st.format, st.input.Value())
	}
	st.toggleFormat()
	if st.format != "json" || !strings.HasSuffix(st.input.Value(), ".json") {
		t.Fatalf("unexpected toggled export state: format=%s val=%s", st.format, st.input.Value())
	}

	m := newDashboardModel()
	m.focusPane = 1
	m.activeTab = 3
	m.queryHistory = []string{"SELECT 1;", "SELECT 2;"}
	m.queryInput.Focus()
	m.queryInput.SetValue("SELECT draft")

	// Up -> newest history entry, preserving draft
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(dashboardModel)
	if m.queryInput.Value() != "SELECT 2;" {
		t.Fatalf("after 1st up = %q, want %q", m.queryInput.Value(), "SELECT 2;")
	}

	// Up -> older history entry
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(dashboardModel)
	if m.queryInput.Value() != "SELECT 1;" {
		t.Fatalf("after 2nd up = %q, want %q", m.queryInput.Value(), "SELECT 1;")
	}

	// Down -> back to newest, then Down -> back to draft
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(dashboardModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(dashboardModel)
	if m.queryInput.Value() != "SELECT draft" {
		t.Fatalf("after restoring draft = %q, want %q", m.queryInput.Value(), "SELECT draft")
	}

	// Table filter
	m.detailTables = []string{"users", "orders", "user_roles"}
	m.tableFilterInput.SetValue("user")
	vis := m.visibleTables()
	if len(vis) != 2 || vis[0] != "users" || vis[1] != "user_roles" {
		t.Fatalf("visibleTables() = %v, want [users user_roles]", vis)
	}

	// Multi-line Enter inserts newline instead of executing
	m.queryInput.SetValue("SELECT 1")
	m.queryInput.CursorEnd()
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(dashboardModel)
	if !strings.Contains(m.queryInput.Value(), "\n") {
		t.Fatalf("expected Enter in multi-line editor to insert newline, got %q", m.queryInput.Value())
	}

	// Horizontal table scrolling
	cols := []string{"id", "first_name", "last_name", "email_address"}
	rows := [][]string{{"1", "Alice", "Smith", "alice@example.com"}}
	out0, totalW := formatCompactTableWithOffset(cols, rows, 20, 0)
	out1, totalW2 := formatCompactTableWithOffset(cols, rows, 20, 28)
	if totalW <= 20 || totalW != totalW2 {
		t.Fatalf("unexpected totalW=%d totalW2=%d", totalW, totalW2)
	}
	if out0 == out1 {
		t.Fatalf("expected horizontal offset to change rendered table slice")
	}
	if !strings.Contains(out1, "email") {
		t.Fatalf("expected scrolled slice to reveal later columns, got:\n%s", out1)
	}

	// ERD hit testing & FK summary
	layout := &erdLayoutResult{
		rects: map[string]erdRect{
			"users": {x: 2, y: 2, w: 20, h: 6},
		},
	}
	if hit := erdHitTest(layout, 5, 4); hit != "users" {
		t.Fatalf("erdHitTest inside rect = %q, want users", hit)
	}
	if hit := erdHitTest(layout, 0, 0); hit != "" {
		t.Fatalf("erdHitTest outside rect = %q, want empty", hit)
	}

	// Smart sidebar filter (multi-token + status + group/provider prefixes)
	e1 := dbEntry{db: &state.Database{Name: "app_pg", Provider: "postgres", Group: "work", Port: 5432}, running: true}
	e2 := dbEntry{db: &state.Database{Name: "app_my", Provider: "mysql", Group: "work", Port: 3306}, running: false}
	if !matchesDBFilter(e1, "work postgres running") {
		t.Fatalf("expected e1 to match 'work postgres running'")
	}
	if matchesDBFilter(e2, "work running") {
		t.Fatalf("expected stopped e2 not to match 'work running'")
	}
	if !matchesDBFilter(e2, "g:work p:mysql stopped") {
		t.Fatalf("expected e2 to match 'g:work p:mysql stopped'")
	}

	// Tab & Shift+Tab switching in right pane
	m.focusPane = 1
	m.activeTab = 0
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(dashboardModel)
	if m.activeTab != 1 {
		t.Fatalf("after Tab activeTab = %d, want 1", m.activeTab)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(dashboardModel)
	if m.activeTab != 0 {
		t.Fatalf("after Shift+Tab activeTab = %d, want 0", m.activeTab)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(dashboardModel)
	if m.activeTab != 3 {
		t.Fatalf("after Shift+Tab wrap activeTab = %d, want 3", m.activeTab)
	}
}
