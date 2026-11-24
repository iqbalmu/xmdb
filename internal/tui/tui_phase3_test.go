package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

func TestSavedQueriesCRUDAndFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.json")

	var list []SavedQuery
	list = upsertSavedQuery(list, SavedQuery{Name: "users", Provider: "postgres", Query: "SELECT * FROM users;"})
	list = upsertSavedQuery(list, SavedQuery{Name: "keys", Provider: "redis", Query: "KEYS *"})
	// Upsert same name+provider replaces existing query
	list = upsertSavedQuery(list, SavedQuery{Name: "users", Provider: "postgres", Query: "SELECT id FROM users LIMIT 10;"})

	if len(list) != 2 {
		t.Fatalf("expected 2 saved queries, got %d", len(list))
	}
	if err := saveSavedQueriesTo(path, list); err != nil {
		t.Fatalf("saveSavedQueriesTo: %v", err)
	}
	loaded := loadSavedQueriesFrom(path)
	if len(loaded) != 2 {
		t.Fatalf("loaded %d queries, want 2", len(loaded))
	}

	pgOnly := filterSavedQueries(loaded, "postgres", "")
	if len(pgOnly) != 1 || !strings.Contains(pgOnly[0].Query, "LIMIT 10") {
		t.Fatalf("unexpected postgres filtered snippets: %+v", pgOnly)
	}

	loaded = deleteSavedQuery(loaded, 0)
	if len(loaded) != 1 {
		t.Fatalf("expected 1 query after delete, got %d", len(loaded))
	}
}

func TestBuildExplainQueryAndSQLHighlight(t *testing.T) {
	pgExplain, err := buildExplainQuery("postgres", "SELECT * FROM users")
	if err != nil || !strings.HasPrefix(pgExplain, "EXPLAIN (ANALYZE, BUFFERS)") {
		t.Errorf("postgres explain = %q, err=%v", pgExplain, err)
	}

	myExplain, err := buildExplainQuery("mariadb", "SELECT 1")
	if err != nil || myExplain != "EXPLAIN SELECT 1" {
		t.Errorf("mariadb explain = %q, err=%v", myExplain, err)
	}

	if _, err := buildExplainQuery("redis", "GET foo"); err == nil {
		t.Errorf("expected error running EXPLAIN on redis")
	}

	hl := highlightSQL("SELECT id, 'alice' FROM users WHERE age > 21 -- filter")
	if !strings.Contains(hl, "SELECT") || !strings.Contains(hl, "alice") || !strings.Contains(hl, "21") {
		t.Errorf("highlightSQL lost tokens: %q", hl)
	}
}

func TestRowInspectorFormattingAndPrettyJSON(t *testing.T) {
	cols := []string{"id", "meta", "deleted_at"}
	row := []string{"42", `{"role":"admin","tags":["a","b"]}`, "NULL"}

	js := formatRowAsJSON(cols, row)
	if !strings.Contains(js, `"role": "admin"`) || !strings.Contains(js, `"deleted_at": null`) {
		t.Errorf("formatRowAsJSON unexpected:\n%s", js)
	}

	txt := formatRowInspectorText(cols, row)
	if !strings.Contains(txt, "id:") || !strings.Contains(txt, `"role": "admin"`) {
		t.Errorf("formatRowInspectorText unexpected:\n%s", txt)
	}
}

func TestLiveMetricsCardAndCommandPalette(t *testing.T) {
	card := renderLiveMetricsCard(&dbMetrics{
		CPUPercent:    24.5,
		MemUsage:      128 * 1024 * 1024,
		MemLimit:      1024 * 1024 * 1024,
		MemPercent:    12.5,
		Uptime:        2*time.Hour + 15*time.Minute,
		ActiveConns:   4,
		SnapshotCount: 3,
	})
	for _, want := range []string{"Live Metrics", "24.5%", "128.0 MB", "2h 15m", "Connections:", "Snapshots:"} {
		if !strings.Contains(card, want) {
			t.Errorf("metrics card missing %q:\n%s", want, card)
		}
	}

	// Open Command Palette via Ctrl+P, filter for "doctor", and press Enter
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "appdb", Provider: "postgres", Port: 5432}, running: true},
	}
	m.applyFilter()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(dashboardModel)
	if m.modal != modalPalette {
		t.Fatalf("expected modalPalette after Ctrl+P, got %d", m.modal)
	}

	m.paletteState.input.SetValue("doctor")
	m.paletteState.applyFilter()
	if len(m.paletteState.filtered) != 1 || m.paletteState.filtered[0].id != "doctor" {
		t.Fatalf("expected palette filtered to doctor, got %+v", m.paletteState.filtered)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(dashboardModel)
	if m.modal != modalDoctor {
		t.Fatalf("expected modalDoctor after selecting doctor in palette, got %d", m.modal)
	}

	// Open Row Inspector Modal when Enter is pressed on a table detail row
	m.modal = modalNone
	m.focusPane = 1
	m.activeTab = 2
	m.selectedTable = "users"
	m.tableDetail = &tableDetail{
		tableName:   "users",
		kind:        providers.EngineSQL,
		previewCols: []string{"id", "name"},
		previewRows: [][]string{{"1", "Alice"}, {"2", "Bob"}},
		totalRows:   2,
		pageSize:    50,
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(dashboardModel)
	if m.explorerRowCursor != 1 {
		t.Fatalf("expected explorerRowCursor=1, got %d", m.explorerRowCursor)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(dashboardModel)
	if m.modal != modalRowInspect || !strings.Contains(m.rowInspectState.jsonStr, "Bob") {
		t.Fatalf("expected Row Inspector modal for Bob, got modal=%d json=%q", m.modal, m.rowInspectState.jsonStr)
	}
}

func TestExplorerRowCursorScrollsIntoView(t *testing.T) {
	m := newDashboardModel()
	m.tableDetailVP = viewport.New(40, 5) // 5 visible lines, 12 rows
	rows := make([][]string, 12)
	for i := range rows {
		rows[i] = []string{"v"}
	}
	m.tableDetail = &tableDetail{
		tableName:   "t",
		kind:        providers.EngineSQL,
		previewCols: []string{"a"},
		previewRows: rows,
		totalRows:   int64(len(rows)),
		pageSize:    defaultExplorerPageSize,
	}
	m.focusPane = 1
	m.activeTab = 2
	m.selectedTable = "t"
	m.tableViewDiagram = false
	m.tableShowFields = false
	m.renderTableDetailToVP()

	// Walk to the last row; viewport must scroll so the selected row stays visible.
	for range 11 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(dashboardModel)
	}
	if m.explorerRowCursor != 11 {
		t.Fatalf("expected cursor at last row, got %d", m.explorerRowCursor)
	}
	if m.tableDetailVP.YOffset+m.tableDetailVP.Height <= m.explorerCursorLine {
		t.Fatalf("selected row %d below viewport: YOffset=%d height=%d",
			m.explorerCursorLine, m.tableDetailVP.YOffset, m.tableDetailVP.Height)
	}

	// Walk back to the first row; it must be visible again.
	for range 11 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(dashboardModel)
	}
	if m.tableDetailVP.YOffset > m.explorerCursorLine || m.explorerCursorLine >= m.tableDetailVP.YOffset+m.tableDetailVP.Height {
		t.Fatalf("row 0 (line %d) not visible: YOffset=%d height=%d",
			m.explorerCursorLine, m.tableDetailVP.YOffset, m.tableDetailVP.Height)
	}

	// Fresh detail load resets the view to the top (schema visible).
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(dashboardModel)
	m.renderTableDetailToVP()
	if m.tableDetailVP.YOffset != 0 {
		t.Fatalf("expected fresh render at top, got YOffset=%d", m.tableDetailVP.YOffset)
	}
}
