package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/iqbalmu/xmdb/internal/state"
)

func TestCreateModelStepNavigation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	m := newCreateModel(mgr)
	if m.step != stepName {
		t.Fatalf("initial step = %d, want %d", m.step, stepName)
	}

	// Empty name should fail validation and stay on stepName
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(createModel)
	if m.validationErr == "" {
		t.Fatalf("expected validation error for empty db name")
	}
	if m.step != stepName {
		t.Fatalf("expected to stay on stepName, got %d", m.step)
	}

	// Valid name advances to stepProvider
	m.input.SetValue("testdb")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(createModel)
	if m.step != stepProvider || m.dbName != "testdb" {
		t.Fatalf("got step=%d dbName=%q, want stepProvider and testdb", m.step, m.dbName)
	}

	// Esc goes back to stepName
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(createModel)
	if m.step != stepName {
		t.Fatalf("expected Esc to return to stepName, got %d", m.step)
	}

	// Esc on stepName sets quit=true
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(createModel)
	if !m.quit {
		t.Fatalf("expected Esc on first step to quit")
	}
}

func TestDashboardModelFilterAndTabs(t *testing.T) {
	m := newDashboardModel()
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432, Group: "work"}},
		{db: &state.Database{Name: "blog_mysql", Provider: "mysql", Port: 3306, Group: ""}},
	}
	m.applyFilter()
	if len(m.filtered) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.filtered))
	}

	// Filter by group "work"
	m.filterInput.SetValue("work")
	m.applyFilter()
	if len(m.filtered) != 1 || m.filtered[0].db.Name != "app_pg" {
		t.Fatalf("expected only app_pg, got %+v", m.filtered)
	}

	// Tab switching (requires right pane focus)
	m.focusPane = 1
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = next.(dashboardModel)
	if m.activeTab != 1 {
		t.Fatalf("expected activeTab=1, got %d", m.activeTab)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = next.(dashboardModel)
	if m.activeTab != 2 {
		t.Fatalf("expected activeTab=2, got %d", m.activeTab)
	}

	// Confirm mode for remove & cancel (requires left pane focus)
	m.focusPane = 0
	m.busy = ""
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(dashboardModel)
	if m.confirmMode != "remove" {
		t.Fatalf("expected confirmMode=remove, got %q", m.confirmMode)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = next.(dashboardModel)
	if m.confirmMode != "" {
		t.Fatalf("expected confirmMode cleared on cancel, got %q", m.confirmMode)
	}
}

func TestDashboardGroupCollapseAndPaneFocus(t *testing.T) {
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432, Group: "work"}, running: true},
		{db: &state.Database{Name: "api_mysql", Provider: "mysql", Port: 3306, Group: "work"}, running: false},
		{db: &state.Database{Name: "local_pg", Provider: "postgres", Port: 5433, Group: ""}},
	}
	m.applyFilter()

	// Rows: [group:work, api_mysql, app_pg, group:global, local_pg] -> 5 rows
	if len(m.rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(m.rows))
	}
	// Cursor should land on first DB item (index 1)
	if m.selected() == nil || m.selected().db.Name != "api_mysql" {
		t.Fatalf("expected api_mysql selected, got %+v", m.selected())
	}

	// Move up to group header and collapse it
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(dashboardModel)
	if !m.rows[m.cursor].isGroup || m.rows[m.cursor].group != "work" {
		t.Fatalf("expected cursor on 'work' group header")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(dashboardModel)
	if !m.collapsedGroups["work"] {
		t.Fatalf("expected 'work' group to be collapsed")
	}
	if len(m.rows) != 3 {
		t.Fatalf("expected 3 rows after collapse, got %d", len(m.rows))
	}

	// Switch pane focus right/left with single arrow
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(dashboardModel)
	if m.focusPane != 1 {
		t.Fatalf("expected focusPane=1, got %d", m.focusPane)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(dashboardModel)
	if m.focusPane != 0 {
		t.Fatalf("expected focusPane=0, got %d", m.focusPane)
	}
}

func TestDashboardSelectionSurvivesReload(t *testing.T) {
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432, Group: "work"}},
		{db: &state.Database{Name: "api_mysql", Provider: "mysql", Port: 3306, Group: "work"}},
		{db: &state.Database{Name: "local_pg", Provider: "postgres", Port: 5433, Group: ""}},
	}
	m.applyFilter()

	// Rest on the "work" group header (as after collapsing it with enter).
	m.cursor = 0
	m.applyFilter()
	if !m.rows[m.cursor].isGroup || m.rows[m.cursor].group != "work" {
		t.Fatalf("cursor on group header jumped to row %d (%s)", m.cursor, m.rows[m.cursor].group)
	}

	// Simulate a periodic reload: selection must follow the same db by name.
	for i, r := range m.rows {
		if !r.isGroup && r.entry.db.Name == "app_pg" {
			m.cursor = i
			break
		}
	}
	m.applyFilter()
	if sel := m.selected(); sel == nil || sel.db.Name != "app_pg" {
		if sel == nil {
			t.Fatalf("expected selection preserved, got group header row %d", m.cursor)
		} else {
			t.Fatalf("expected app_pg selected, got %s", sel.db.Name)
		}
	}

	// Second reload with identical entries: cursor must not drift.
	m.applyFilter()
	if sel := m.selected(); sel == nil || sel.db.Name != "app_pg" {
		t.Fatalf("cursor drifted after reload: row %d", m.cursor)
	}

	// Header selection also survives a reload while its group stays collapsed.
	m.collapsedGroups["work"] = true
	m.applyFilter()
	m.cursor = 0
	m.applyFilter()
	if !m.rows[m.cursor].isGroup || m.rows[m.cursor].group != "work" {
		t.Fatalf("cursor on collapsed group header jumped to row %d", m.cursor)
	}
}

// The 5s tick reloads via msgDatabasesLoaded -> applyFilter; a cursor resting
// on a group header must not be hijacked to the first db row.
func TestDashboardTickReloadKeepsHeaderSelection(t *testing.T) {
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432, Group: "work"}},
		{db: &state.Database{Name: "blog_mysql", Provider: "mysql", Port: 3306, Group: ""}},
	}
	m.applyFilter()

	// Move cursor up onto the "work" group header.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(dashboardModel)
	if !m.rows[m.cursor].isGroup {
		t.Fatalf("expected cursor on group header")
	}

	// Deliver what the tick produces: a fresh load with identical entries.
	next, _ = m.Update(msgDatabasesLoaded{
		entries: []dbEntry{
			{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432, Group: "work"}},
			{db: &state.Database{Name: "blog_mysql", Provider: "mysql", Port: 3306, Group: ""}},
		},
	})
	m = next.(dashboardModel)
	if !m.rows[m.cursor].isGroup || m.rows[m.cursor].group != "work" {
		t.Fatalf("tick reload moved cursor off group header to row %d", m.cursor)
	}
}

func TestDashboardSelectionFilteredOut(t *testing.T) {
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432, Group: "work"}},
		{db: &state.Database{Name: "blog_mysql", Provider: "mysql", Port: 3306, Group: ""}},
	}
	m.applyFilter()

	// Select app_pg, then filter it out: cursor must land on a db row, not a header.
	m.cursor = 1
	m.filterInput.SetValue("blog")
	m.applyFilter()
	if sel := m.selected(); sel == nil || sel.db.Name != "blog_mysql" {
		t.Fatalf("expected fallback to blog_mysql, got cursor row %d", m.cursor)
	}
}

func TestDashboardModalsAndResponsiveLayout(t *testing.T) {
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432}, running: true},
	}
	m.applyFilter()

	// Help modal
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(dashboardModel)
	if m.modal != modalHelp {
		t.Fatalf("expected modalHelp, got %v", m.modal)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(dashboardModel)
	if m.modal != modalNone {
		t.Fatalf("expected modalNone after Esc, got %v", m.modal)
	}

	// Backup modal on running DB
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(dashboardModel)
	if m.modal != modalBackup {
		t.Fatalf("expected modalBackup, got %v", m.modal)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(dashboardModel)
	if m.modal != modalNone {
		t.Fatalf("expected modalNone after Esc, got %v", m.modal)
	}

	// Responsive stacked layout when width < 80
	next, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 28})
	m = next.(dashboardModel)
	stacked, _, _, _, _ := m.layoutDimensions()
	if !stacked {
		t.Fatalf("expected stacked=true for width=70")
	}
	if v := m.View(); v == "" {
		t.Fatalf("expected non-empty View()")
	}
}

func TestTablesViewAndFieldsToggles(t *testing.T) {
	graph := &tableSchemaGraph{
		tables: []tableOverview{
			{
				name: "users",
				columns: []tableColumnInfo{
					{name: "id", dataType: "bigint", key: "PK"},
					{name: "email", dataType: "varchar(255)"},
				},
			},
			{
				name: "orders",
				columns: []tableColumnInfo{
					{name: "id", dataType: "bigint", key: "PK"},
					{name: "user_id", dataType: "bigint", key: "FK"},
				},
			},
		},
		relations: []tableRelation{
			{fromTable: "orders", fromCol: "user_id", toTable: "users", toCol: "id"},
		},
	}
	graph.byName = map[string]*tableOverview{
		"users":  &graph.tables[0],
		"orders": &graph.tables[1],
	}

	tables := []string{"users", "orders"}

	// Overview list with fields ON vs OFF
	listWithFields, _ := formatTableOverviewList(tables, graph, 0, true, true, 80)
	if !strings.Contains(listWithFields, "email") || !strings.Contains(listWithFields, "user_id") {
		t.Fatalf("expected fields in list view, got:\n%s", listWithFields)
	}
	listNoFields, _ := formatTableOverviewList(tables, graph, 0, true, false, 80)
	if strings.Contains(listNoFields, "email") || strings.Contains(listNoFields, "user_id") {
		t.Fatalf("expected fields hidden in list view, got:\n%s", listNoFields)
	}
	if !strings.Contains(listNoFields, "users") || !strings.Contains(listNoFields, "orders") {
		t.Fatalf("expected table names in list view without fields, got:\n%s", listNoFields)
	}

	// Overview 2D ERD diagram with fields ON vs OFF
	diagWithFields, _ := formatTableOverviewDiagram(tables, graph, 0, true, true, 90)
	if !strings.Contains(diagWithFields, "email") || !strings.Contains(diagWithFields, "user_id") {
		t.Fatalf("expected fields in 2D ERD diagram view, got:\n%s", diagWithFields)
	}
	if !strings.Contains(diagWithFields, "╭") || (!strings.Contains(diagWithFields, ">") && !strings.Contains(diagWithFields, "<")) {
		t.Fatalf("expected ERD boxes and crow's foot cardinality connector in diagram view, got:\n%s", diagWithFields)
	}
	diagNoFields, _ := formatTableOverviewDiagram(tables, graph, 0, true, false, 90)
	if strings.Contains(diagNoFields, "email") || strings.Contains(diagNoFields, "user_id") {
		t.Fatalf("expected fields hidden in 2D ERD diagram view, got:\n%s", diagNoFields)
	}
	if !strings.Contains(diagNoFields, "(2 fields)") || (!strings.Contains(diagNoFields, ">") && !strings.Contains(diagNoFields, "<")) {
		t.Fatalf("expected field count and ERD connector when fields=off, got:\n%s", diagNoFields)
	}

	// Detail diagram with fields ON vs OFF
	detail := &tableDetail{
		tableName:   "orders",
		schemaCols:  []string{"Field", "Type", "Null"},
		schemaRows:  [][]string{{"id", "bigint", "NO"}, {"user_id", "bigint", "NO"}},
		previewCols: []string{"id", "user_id"},
		previewRows: [][]string{{"1", "42"}},
	}
	detDiagOn := formatTableDetailDiagram(detail, graph, true, 80)
	if !strings.Contains(detDiagOn, "user_id") || !strings.Contains(detDiagOn, "orders.user_id ──▶ users.id") {
		t.Fatalf("expected fields & FK in detail diagram, got:\n%s", detDiagOn)
	}
	detDiagOff := formatTableDetailDiagram(detail, graph, false, 80)
	if strings.Contains(detDiagOff, "user_id") || !strings.Contains(detDiagOff, "fields hidden") {
		t.Fatalf("expected fields hidden in detail diagram, got:\n%s", detDiagOff)
	}

	// Keybinding toggles in dashboardModel on Tables tab
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432}, running: true},
	}
	m.applyFilter()
	m.activeTab = 2
	m.focusPane = 1
	m.detailTables = tables
	m.tableSchema = graph
	m.tableDetail = detail
	m.renderTableOverviewToVP()

	if !m.tableViewDiagram {
		t.Fatalf("expected tableViewDiagram=true initially")
	}
	if !m.tableShowFields {
		t.Fatalf("expected tableShowFields=true initially")
	}
	if m.erdZoom != 1 {
		t.Fatalf("expected initial erdZoom=1, got %d", m.erdZoom)
	}

	// Press 'v' -> list/table, then 'v' -> diagram
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(dashboardModel)
	if m.tableViewDiagram {
		t.Fatalf("expected tableViewDiagram=false after 'v'")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(dashboardModel)
	if !m.tableViewDiagram {
		t.Fatalf("expected tableViewDiagram=true after second 'v'")
	}

	// Zoom in ('+'), zoom out ('-'), reset ('0')
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = next.(dashboardModel)
	if m.erdZoom != 2 {
		t.Fatalf("expected erdZoom=2 after '+', got %d", m.erdZoom)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m = next.(dashboardModel)
	if m.erdZoom != 1 {
		t.Fatalf("expected erdZoom=1 after '-', got %d", m.erdZoom)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m = next.(dashboardModel)
	if m.erdZoom != 0 {
		t.Fatalf("expected erdZoom=0 after second '-', got %d", m.erdZoom)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = next.(dashboardModel)
	if m.erdZoom != 1 {
		t.Fatalf("expected erdZoom=1 after '0', got %d", m.erdZoom)
	}

	// Press 'f' -> fields off
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = next.(dashboardModel)
	if m.tableShowFields {
		t.Fatalf("expected tableShowFields=false after 'f'")
	}

	// Also works when inspecting a single table
	m.selectedTable = "orders"
	m.renderTableDetailToVP()
	if !strings.Contains(m.tableDetailVP.View(), "fields hidden") {
		t.Fatalf("expected detail diagram to hide fields when tableShowFields=false")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = next.(dashboardModel)
	if !m.tableShowFields {
		t.Fatalf("expected tableShowFields=true after second 'f'")
	}
	if !strings.Contains(m.tableDetailVP.View(), "user_id") {
		t.Fatalf("expected detail diagram to show fields after toggling 'f' back on")
	}
}

func TestDynamicPaneKeybinds(t *testing.T) {
	m := newDashboardModel()
	m.busy = ""
	m.entries = []dbEntry{
		{db: &state.Database{Name: "app_pg", Provider: "postgres", Port: 5432}, running: true},
	}
	m.applyFilter()

	// Left pane active (focusPane == 0):
	if !strings.Contains(m.footerHelpText(), "[Sidebar]") {
		t.Fatalf("expected [Sidebar] footer when focusPane=0, got: %s", m.footerHelpText())
	}
	if strings.Contains(m.footerHelpText(), "1-4") {
		t.Fatalf("expected no tab shortcut in [Sidebar] footer, got: %s", m.footerHelpText())
	}
	// Right-pane-only keys ('2', 'v', 'f', 'p') must be ignored when focusPane == 0.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = next.(dashboardModel)
	if m.activeTab != 0 {
		t.Fatalf("expected '2' ignored when focusPane=0, got activeTab=%d", m.activeTab)
	}
	m.activeTab = 2
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(dashboardModel)
	if !m.tableViewDiagram {
		t.Fatalf("expected 'v' ignored when focusPane=0")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = next.(dashboardModel)
	if !m.tableShowFields {
		t.Fatalf("expected 'f' ignored when focusPane=0")
	}
	m.activeTab = 0
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = next.(dashboardModel)
	if m.showPassword {
		t.Fatalf("expected 'p' ignored when focusPane=0")
	}

	// Switch to right pane with Right arrow and back with Left arrow:
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(dashboardModel)
	if m.focusPane != 1 {
		t.Fatalf("expected focusPane=1 after Right, got %d", m.focusPane)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(dashboardModel)
	if m.focusPane != 0 {
		t.Fatalf("expected focusPane=0 after Left, got %d", m.focusPane)
	}

	// Switch to right pane (focusPane == 1):
	m.focusPane = 1
	if !strings.Contains(m.footerHelpText(), "[Info]") {
		t.Fatalf("expected [Info] footer when focusPane=1 & activeTab=0, got: %s", m.footerHelpText())
	}
	// Left-pane-only keys ('d', 'R', 'b', 's', 'n') must be ignored when focusPane == 1.
	for _, k := range []rune{'d', 'R', 'b', 's', 'n'} {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
		m = next.(dashboardModel)
		if m.confirmMode != "" || m.modal != modalNone || m.busy != "" {
			t.Fatalf("expected left-pane key %q ignored when focusPane=1", string(k))
		}
	}
	// 'p' works on Info tab when focusPane == 1.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = next.(dashboardModel)
	if !m.showPassword {
		t.Fatalf("expected 'p' to toggle password when focusPane=1 & activeTab=0")
	}

	// Footer updates per tab on right pane.
	m.activeTab = 1
	if !strings.Contains(m.footerHelpText(), "[Logs]") {
		t.Fatalf("expected [Logs] footer, got: %s", m.footerHelpText())
	}
	m.activeTab = 2
	if !strings.Contains(m.footerHelpText(), "[Tables ERD]") {
		t.Fatalf("expected [Tables ERD] footer, got: %s", m.footerHelpText())
	}
	m.selectedTable = "users"
	if !strings.Contains(m.footerHelpText(), "[Table Detail]") {
		t.Fatalf("expected [Table Detail] footer, got: %s", m.footerHelpText())
	}
	m.activeTab = 3
	if !strings.Contains(m.footerHelpText(), "[Query]") {
		t.Fatalf("expected [Query] footer, got: %s", m.footerHelpText())
	}
}

func TestCreateModel_SeedStepAndCloneModal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr, err := state.NewManager()
	if err != nil {
		t.Fatalf("state.NewManager() error = %v", err)
	}

	cm := newCreateModel(mgr)
	cm.step = stepSeed
	cm.prepareStep()

	// Empty seed path skips cleanly to review.
	next, _ := cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm = next.(createModel)
	if cm.step != stepReview {
		t.Fatalf("step = %d, want stepReview (%d)", cm.step, stepReview)
	}

	// Non-existent seed path shows validation error and stays on stepSeed.
	cm.step = stepSeed
	cm.prepareStep()
	cm.input.SetValue("/nonexistent/seed.sql")
	next, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cm = next.(createModel)
	if cm.step != stepSeed || cm.validationErr == "" {
		t.Fatalf("expected validationErr on stepSeed, got step=%d err=%q", cm.step, cm.validationErr)
	}

	cloneSt := newCloneModalState("appdb")
	if got := cloneSt.input.Value(); got != "appdb_copy" {
		t.Fatalf("newCloneModalState value = %q, want %q", got, "appdb_copy")
	}
}
