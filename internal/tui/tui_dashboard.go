package tui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
)

const (
	dashboardRefreshInterval = 5 * time.Second
	toastDuration            = 4 * time.Second
	tabCount                 = 4
)

type dbEntry struct {
	db      *state.Database
	running bool
	connStr string
}

type listRow struct {
	isGroup      bool
	group        string
	count        int
	runningCount int
	entry        *dbEntry
}

// Messages
type msgDatabasesLoaded struct {
	entries   []dbEntry
	dockerErr error
}

type msgDetailLoaded struct {
	name        string
	tab         int
	logs        string
	tables      []string
	tableSchema *tableSchemaGraph
	err         error
}

type msgTableDetailLoaded struct {
	dbName string
	detail *tableDetail
	err    error
}

type msgQueryResult struct {
	dbName string
	result *sqlQueryResult
	err    error
}

type msgActionDone struct {
	toast string
	err   error
}

type msgTick time.Time

type msgClearToast struct {
	seq int
}

type shellExecCmd struct {
	name      string
	openShell func(string) error
}

func (s shellExecCmd) Run() error {
	var err error
	if s.openShell == nil {
		err = fmt.Errorf("shell runner not configured")
	} else {
		err = s.openShell(s.name)
	}
	if err != nil {
		fmt.Printf("\nShell error: %v\nPress Enter to return to dashboard...", err)
		var dummy string
		_, _ = fmt.Scanln(&dummy)
	}
	return nil
}
func (s shellExecCmd) SetStdin(io.Reader)  {}
func (s shellExecCmd) SetStdout(io.Writer) {}
func (s shellExecCmd) SetStderr(io.Writer) {}

type dashboardModel struct {
	runtimeMode string
	openShell   func(string) error

	width  int
	height int

	entries         []dbEntry
	filtered        []dbEntry
	rows            []listRow
	collapsedGroups map[string]bool
	cursor          int
	leftOffset      int
	focusPane       int // 0=Left, 1=Right
	dockerErr       error

	activeTab    int // 0=Info, 1=Logs, 2=Tables, 3=Query
	showPassword bool

	// Logs tab
	detailLogs      string
	detailErr       error
	logsVP          viewport.Model
	logFollow       bool
	logSearching    bool
	logSearchInput  textinput.Model
	logSearchQuery  string
	logMatchLines   []int
	logMatchCurrent int

	// Tables tab
	detailTables     []string
	tableSchema      *tableSchemaGraph
	tableCursor      int
	selectedTable    string
	tableDetail      *tableDetail
	tableOverviewVP  viewport.Model
	tableDetailVP    viewport.Model
	tableViewDiagram bool
	tableShowFields  bool
	tableFiltering   bool
	tableFilterInput textinput.Model
	erdXOffset        int
	erdYOffset        int
	erdZoom           int // 0=50% (out), 1=100% (normal), 2=150% (in)
	erdLayout         *erdLayoutResult
	erdDragging       bool
	erdDragLastX      int
	erdDragLastY      int
	erdDragMoved      bool
	lastERDClickTable string
	lastERDClickAt    time.Time

	// Query tab
	queryInput   textarea.Model
	queryVP      viewport.Model
	queryResult  *sqlQueryResult
	queryErr     error
	queryHistory []string
	queryHistIdx int
	queryDraft   string
	queryXOffset int
	queryTotalW  int

	// Filter
	filtering   bool
	filterInput textinput.Model

	// Modals & confirmations
	confirmMode  string // "", "remove", "reset"
	modal        modalKind
	backupInput  textinput.Model
	restoreState restoreModalState
	createWizard *createModel
	cloneState   cloneModalState
	exportState  exportModalState

	// Status & feedback
	spinner    spinner.Model
	busy       string
	toast      string
	toastIsErr bool
	toastSeq   int
}

func newDashboardModel() dashboardModel {
	fi := textinput.New()
	fi.Placeholder = "filter: name, group, postgres, running..."
	fi.CharLimit = 64
	fi.Width = 28

	lsi := textinput.New()
	lsi.Placeholder = "search logs..."
	lsi.CharLimit = 64
	lsi.Width = 28

	qi := textarea.New()
	qi.Placeholder = "SELECT * FROM users LIMIT 10;"
	qi.CharLimit = 4096
	qi.ShowLineNumbers = false
	qi.Prompt = "SQL> "
	qi.SetWidth(48)
	qi.SetHeight(4)

	tfi := textinput.New()
	tfi.Placeholder = "filter tables..."
	tfi.CharLimit = 64
	tfi.Width = 28

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorWarning)

	return dashboardModel{
		width:            100,
		height:           30,
		collapsedGroups:  make(map[string]bool),
		filterInput:      fi,
		logSearchInput:   lsi,
		tableFilterInput: tfi,
		queryInput:       qi,
		queryHistory:     loadQueryHistory(),
		queryHistIdx:     -1,
		logsVP:           viewport.New(50, 18),
		tableOverviewVP:  viewport.New(50, 18),
		tableDetailVP:    viewport.New(50, 18),
		tableViewDiagram: true,
		tableShowFields:  true,
		erdZoom:          1,
		queryVP:          viewport.New(50, 14),
		logFollow:        true,
		spinner:          sp,
		busy:             "Loading databases...",
	}
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		loadDatabasesCmd(m.runtimeMode),
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(dashboardRefreshInterval, func(t time.Time) tea.Msg {
		return msgTick(t)
	})
}

func (m *dashboardModel) setToast(msg string, isErr bool) tea.Cmd {
	m.toast = msg
	m.toastIsErr = isErr
	m.toastSeq++
	seq := m.toastSeq
	return tea.Tick(toastDuration, func(time.Time) tea.Msg {
		return msgClearToast{seq: seq}
	})
}

func matchesDBFilter(e dbEntry, query string) bool {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(tokens) == 0 {
		return true
	}
	name := strings.ToLower(e.db.Name)
	group := strings.ToLower(service.GroupLabel(e.db.Group))
	provider := strings.ToLower(e.db.Provider)
	port := fmt.Sprintf("%d", e.db.Port)
	status := "stopped"
	if e.running {
		status = "running"
	}

	for _, tok := range tokens {
		switch {
		case tok == "running" || tok == "up" || tok == "status:running" || tok == "status:up":
			if !e.running {
				return false
			}
		case tok == "stopped" || tok == "down" || tok == "status:stopped" || tok == "status:down":
			if e.running {
				return false
			}
		case strings.HasPrefix(tok, "g:"):
			if !strings.Contains(group, strings.TrimPrefix(tok, "g:")) {
				return false
			}
		case strings.HasPrefix(tok, "group:"):
			if !strings.Contains(group, strings.TrimPrefix(tok, "group:")) {
				return false
			}
		case strings.HasPrefix(tok, "p:"):
			if !strings.Contains(provider, strings.TrimPrefix(tok, "p:")) {
				return false
			}
		case strings.HasPrefix(tok, "provider:"):
			if !strings.Contains(provider, strings.TrimPrefix(tok, "provider:")) {
				return false
			}
		default:
			if !strings.Contains(name, tok) &&
				!strings.Contains(group, tok) &&
				!strings.Contains(provider, tok) &&
				!strings.Contains(port, tok) &&
				!strings.Contains(status, tok) {
				return false
			}
		}
	}
	return true
}

func (m *dashboardModel) applyFilter() {
	// Remember the current selection so the cursor follows the same row
	// across filter changes and periodic reloads.
	prevGroup, prevDB := "", ""
	if row := m.selectedRow(); row != nil {
		if row.isGroup {
			prevGroup = row.group
		} else if row.entry != nil {
			prevDB = row.entry.db.Name
		}
	}

	q := strings.TrimSpace(m.filterInput.Value())
	if q == "" {
		m.filtered = m.entries
	} else {
		var out []dbEntry
		for _, e := range m.entries {
			if matchesDBFilter(e, q) {
				out = append(out, e)
			}
		}
		m.filtered = out
	}
	m.rebuildRows()

	if prevDB != "" && m.selectDBByName(prevDB) {
		return
	}
	if prevGroup != "" && m.selectGroupByName(prevGroup) {
		return
	}
	// The selection vanished (filtered out) or nothing was selected yet:
	// land on the first database row.
	for i, r := range m.rows {
		if !r.isGroup && r.entry != nil {
			m.cursor = i
			m.ensureLeftVisible()
			return
		}
	}
}

func (m *dashboardModel) rebuildRows() {
	grouped := make(map[string][]dbEntry)
	for _, e := range m.filtered {
		g := service.GroupLabel(e.db.Group)
		grouped[g] = append(grouped[g], e)
	}

	var groups []string
	hasGlobal := false
	for g := range grouped {
		if g == "global" {
			hasGlobal = true
			continue
		}
		groups = append(groups, g)
	}
	sort.Strings(groups)
	if hasGlobal {
		groups = append(groups, "global")
	}

	m.rows = nil
	for _, g := range groups {
		items := grouped[g]
		sort.Slice(items, func(i, j int) bool {
			return items[i].db.Name < items[j].db.Name
		})
		runCount := 0
		for _, it := range items {
			if it.running {
				runCount++
			}
		}
		m.rows = append(m.rows, listRow{
			isGroup:      true,
			group:        g,
			count:        len(items),
			runningCount: runCount,
		})
		if !m.collapsedGroups[g] {
			for i := range items {
				entryCopy := items[i]
				m.rows = append(m.rows, listRow{
					group: g,
					entry: &entryCopy,
				})
			}
		}
	}

	if len(m.rows) == 0 {
		m.cursor = 0
		m.leftOffset = 0
		return
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	// If cursor is on a group header and there is a db item right below it on initial load,
	// keep it usable; if user navigated onto a group header intentionally, allow it.
	m.ensureLeftVisible()
}

func (m *dashboardModel) ensureLeftVisible() {
	vis := m.leftVisibleLines()
	if vis <= 0 {
		return
	}
	if m.cursor < m.leftOffset {
		m.leftOffset = m.cursor
	} else if m.cursor >= m.leftOffset+vis {
		m.leftOffset = m.cursor - vis + 1
	}
	if m.leftOffset < 0 {
		m.leftOffset = 0
	}
}

func (m dashboardModel) layoutDimensions() (stacked bool, leftW, leftH, rightW, rightH int) {
	bodyH := max(10, m.height-5)
	if m.width < 80 {
		stacked = true
		w := max(30, m.width-2)
		leftH = max(5, bodyH*32/100)
		rightH = max(8, bodyH-leftH-2)
		return true, w, leftH, w, rightH
	}
	leftW = min(38, max(26, m.width*26/100))
	rightW = max(36, m.width-leftW-4)
	return false, leftW, bodyH, rightW, bodyH
}

func (m dashboardModel) leftVisibleLines() int {
	_, _, leftH, _, _ := m.layoutDimensions()
	overhead := 3 // header + blank + border
	if m.filtering || strings.TrimSpace(m.filterInput.Value()) != "" {
		overhead++
	}
	return max(3, leftH-overhead)
}

func (m *dashboardModel) syncViewportSizes() {
	_, _, _, rightW, rightH := m.layoutDimensions()
	innerW := max(20, rightW-4)
	innerH := max(5, rightH-5)

	m.logsVP.Width = innerW
	m.logsVP.Height = max(4, innerH-1)
	m.tableOverviewVP.Width = innerW
	m.tableOverviewVP.Height = max(4, innerH-2)
	m.tableDetailVP.Width = innerW
	m.tableDetailVP.Height = max(4, innerH-2)
	m.queryInput.SetWidth(max(20, innerW-2))
	m.queryVP.Width = innerW
	m.queryVP.Height = max(4, innerH-8)
}

func (m *dashboardModel) refreshLogsViewport() {
	if m.detailErr != nil {
		m.logsVP.SetContent(styleErrorText.Render("Error loading logs: " + m.detailErr.Error()))
		return
	}
	if strings.TrimSpace(m.detailLogs) == "" {
		m.logsVP.SetContent(styleMutedText.Render("(no logs available)"))
		return
	}
	rawLines := strings.Split(strings.TrimRight(m.detailLogs, "\n"), "\n")
	innerW := max(20, m.logsVP.Width)
	q := strings.ToLower(strings.TrimSpace(m.logSearchQuery))
	m.logMatchLines = nil

	var wrapped []string
	for _, line := range rawLines {
		chunks := wrapLine(line, innerW)
		for _, c := range chunks {
			if q != "" && strings.Contains(strings.ToLower(c), q) {
				m.logMatchLines = append(m.logMatchLines, len(wrapped))
				c = highlightSearch(c, q)
			}
			wrapped = append(wrapped, c)
		}
	}
	m.logsVP.SetContent(strings.Join(wrapped, "\n"))
	if m.logFollow && !m.logSearching && q == "" {
		m.logsVP.GotoBottom()
	}
}

func wrapLine(s string, width int) []string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return []string{s}
	}
	var out []string
	rest := s
	for lipgloss.Width(rest) > width {
		part := ansi.Truncate(rest, width, "")
		if len(part) == 0 {
			break
		}
		out = append(out, part)
		rest = rest[len(part):]
	}
	if rest != "" {
		out = append(out, rest)
	}
	return out
}

func highlightSearch(line, q string) string {
	lower := strings.ToLower(line)
	idx := strings.Index(lower, q)
	if idx < 0 {
		return line
	}
	return line[:idx] + styleSearchMatch.Render(line[idx:idx+len(q)]) + line[idx+len(q):]
}

func (m dashboardModel) selected() *dbEntry {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].entry
}

func (m dashboardModel) selectedRow() *listRow {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

func (m dashboardModel) groupEntries(group string) []dbEntry {
	var out []dbEntry
	for _, e := range m.filtered {
		if service.GroupLabel(e.db.Group) == group {
			out = append(out, e)
		}
	}
	return out
}

func (m *dashboardModel) selectDBByName(name string) bool {
	for i, r := range m.rows {
		if !r.isGroup && r.entry != nil && r.entry.db.Name == name {
			m.cursor = i
			m.ensureLeftVisible()
			return true
		}
	}
	return false
}

func (m *dashboardModel) selectGroupByName(group string) bool {
	for i, r := range m.rows {
		if r.isGroup && r.group == group {
			m.cursor = i
			m.ensureLeftVisible()
			return true
		}
	}
	return false
}

func (m dashboardModel) visibleTables() []string {
	q := strings.ToLower(strings.TrimSpace(m.tableFilterInput.Value()))
	if q == "" {
		return m.detailTables
	}
	var out []string
	for _, tbl := range m.detailTables {
		if strings.Contains(strings.ToLower(tbl), q) {
			out = append(out, tbl)
		}
	}
	return out
}

func (m *dashboardModel) resetRightSubstate() {
	m.selectedTable = ""
	m.tableDetail = nil
	m.tableSchema = nil
	m.tableCursor = 0
	m.tableFiltering = false
	m.tableFilterInput.Blur()
	m.tableFilterInput.SetValue("")
	m.erdXOffset = 0
	m.erdYOffset = 0
	m.erdLayout = nil
	m.detailErr = nil
	m.queryResult = nil
	m.queryErr = nil
	m.queryHistIdx = -1
	m.queryDraft = ""
	m.queryXOffset = 0
	m.queryTotalW = 0
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncViewportSizes()
		m.ensureLeftVisible()
		if m.activeTab == 1 {
			m.refreshLogsViewport()
		}
		if m.activeTab == 2 {
			if m.selectedTable != "" {
				m.renderTableDetailToVP()
			} else {
				m.renderTableOverviewToVP()
			}
		}
		return m, nil

	case spinner.TickMsg:
		if m.busy != "" {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case msgClearToast:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
		return m, nil

	case msgTick:
		return m, tea.Batch(loadDatabasesCmd(m.runtimeMode), tickCmd())

	case msgDatabasesLoaded:
		m.busy = ""
		m.dockerErr = msg.dockerErr
		m.entries = msg.entries
		m.applyFilter()
		return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)

	case msgDetailLoaded:
		if sel := m.selected(); sel != nil && sel.db.Name == msg.name && m.activeTab == msg.tab {
			m.detailLogs = msg.logs
			m.detailTables = msg.tables
			m.tableSchema = msg.tableSchema
			m.detailErr = msg.err
			if msg.tab == 1 {
				m.refreshLogsViewport()
			}
			if msg.tab == 2 {
				if m.tableCursor >= len(m.detailTables) {
					m.tableCursor = max(0, len(m.detailTables)-1)
				}
				m.renderTableOverviewToVP()
			}
		}
		return m, nil

	case msgTableDetailLoaded:
		m.busy = ""
		if sel := m.selected(); sel != nil && sel.db.Name == msg.dbName {
			if msg.err != nil {
				return m, m.setToast(msg.err.Error(), true)
			}
			m.tableDetail = msg.detail
			m.renderTableDetailToVP()
		}
		return m, nil

	case msgQueryResult:
		m.busy = ""
		if sel := m.selected(); sel != nil && sel.db.Name == msg.dbName {
			m.queryErr = msg.err
			m.queryResult = msg.result
			m.queryXOffset = 0
			m.renderQueryResultToVP()
		}
		return m, nil

	case msgActionDone:
		m.busy = ""
		var toastCmd tea.Cmd
		if msg.err != nil {
			toastCmd = m.setToast(msg.err.Error(), true)
		} else if msg.toast != "" {
			toastCmd = m.setToast(msg.toast, false)
		}
		return m, tea.Batch(toastCmd, loadDatabasesCmd(m.runtimeMode))

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		// Modal takes priority
		if m.modal != modalNone {
			return m.updateModalKey(msg)
		}

		// Confirmation prompt
		if m.confirmMode != "" {
			sel := m.selected()
			mode := m.confirmMode
			m.confirmMode = ""
			if strings.ToLower(msg.String()) == "y" && sel != nil {
				if mode == "remove" {
					m.busy = fmt.Sprintf("Removing '%s'...", sel.db.Name)
					return m, tea.Batch(m.spinner.Tick, removeDBCmd(m.runtimeMode, *sel))
				}
				if mode == "reset" {
					m.busy = fmt.Sprintf("Resetting '%s'...", sel.db.Name)
					return m, tea.Batch(m.spinner.Tick, resetDBCmd(m.runtimeMode, *sel))
				}
			}
			return m, m.setToast("Cancelled", false)
		}

		// Left-pane filter input
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.applyFilter()
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			case "enter":
				m.filtering = false
				m.filterInput.Blur()
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
			var cmd tea.Cmd
			m.filterInput, cmd = m.filterInput.Update(msg)
			m.applyFilter()
			return m, cmd
		}

		// Right-pane log search input
		if m.logSearching {
			switch msg.String() {
			case "esc":
				m.logSearching = false
				m.logSearchInput.SetValue("")
				m.logSearchQuery = ""
				m.logSearchInput.Blur()
				m.refreshLogsViewport()
				return m, nil
			case "enter":
				m.logSearching = false
				m.logSearchInput.Blur()
				m.logSearchQuery = m.logSearchInput.Value()
				m.logMatchCurrent = 0
				m.refreshLogsViewport()
				if len(m.logMatchLines) > 0 {
					m.logsVP.SetYOffset(max(0, m.logMatchLines[0]-2))
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.logSearchInput, cmd = m.logSearchInput.Update(msg)
			m.logSearchQuery = m.logSearchInput.Value()
			m.refreshLogsViewport()
			return m, cmd
		}

		// Right-pane table filter active
		if m.focusPane == 1 && m.activeTab == 2 && m.tableFiltering {
			switch msg.String() {
			case "esc":
				m.tableFiltering = false
				m.tableFilterInput.Blur()
				m.tableFilterInput.SetValue("")
				m.tableCursor = 0
				m.renderTableOverviewToVP()
				return m, nil
			case "enter":
				m.tableFiltering = false
				m.tableFilterInput.Blur()
				m.renderTableOverviewToVP()
				return m, nil
			}
			var cmd tea.Cmd
			m.tableFilterInput, cmd = m.tableFilterInput.Update(msg)
			vis := m.visibleTables()
			if m.tableCursor >= len(vis) {
				m.tableCursor = max(0, len(vis)-1)
			}
			m.renderTableOverviewToVP()
			return m, cmd
		}

		// Right-pane Query input focused
		if m.focusPane == 1 && m.activeTab == 3 && m.queryInput.Focused() {
			prevHistory := func() {
				if len(m.queryHistory) > 0 {
					if m.queryHistIdx == -1 {
						m.queryDraft = m.queryInput.Value()
						m.queryHistIdx = len(m.queryHistory) - 1
					} else if m.queryHistIdx > 0 {
						m.queryHistIdx--
					}
					m.queryInput.SetValue(m.queryHistory[m.queryHistIdx])
					m.queryInput.CursorEnd()
				}
			}
			nextHistory := func() {
				if m.queryHistIdx != -1 {
					if m.queryHistIdx < len(m.queryHistory)-1 {
						m.queryHistIdx++
						m.queryInput.SetValue(m.queryHistory[m.queryHistIdx])
					} else {
						m.queryHistIdx = -1
						m.queryInput.SetValue(m.queryDraft)
					}
					m.queryInput.CursorEnd()
				}
			}

			switch msg.String() {
			case "esc":
				m.queryInput.Blur()
				return m, nil
			case "ctrl+p", "alt+up":
				prevHistory()
				return m, nil
			case "ctrl+n", "alt+down":
				nextHistory()
				return m, nil
			case "up":
				if m.queryInput.Line() == 0 && (m.queryInput.LineCount() <= 1 || m.queryHistIdx != -1) {
					prevHistory()
					return m, nil
				}
			case "down":
				if m.queryInput.Line() == m.queryInput.LineCount()-1 && m.queryHistIdx != -1 {
					nextHistory()
					return m, nil
				}
			case "ctrl+r", "f5", "alt+enter":
				sel := m.selected()
				q := strings.TrimSpace(m.queryInput.Value())
				if sel == nil || !sel.running {
					return m, m.setToast("Database must be running to execute SQL", true)
				}
				if q == "" {
					return m, nil
				}
				m.queryHistory = recordQueryHistory(m.queryHistory, q)
				_ = saveQueryHistory(m.queryHistory)
				m.queryHistIdx = -1
				m.queryDraft = ""
				m.busy = "Running query..."
				return m, tea.Batch(m.spinner.Tick, runQuickSQLCmd(sel.db, q))
			}
			m.queryHistIdx = -1
			var cmd tea.Cmd
			m.queryInput, cmd = m.queryInput.Update(msg)
			return m, cmd
		}

		return m.handleNormalKey(msg)
	}

	return m, nil
}

func (m dashboardModel) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "?":
		m.modal = modalHelp
		return m, nil

	case "left", "h":
		m.focusPane = 0
		if m.activeTab == 2 && m.selectedTable == "" {
			m.renderTableOverviewToVP()
		}
		return m, nil

	case "right", "l":
		m.focusPane = 1
		if m.activeTab == 2 && m.selectedTable == "" {
			m.renderTableOverviewToVP()
		}
		return m, nil

	case "tab", "]":
		if m.focusPane == 1 {
			m.activeTab = (m.activeTab + 1) % tabCount
			return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
		}

	case "shift+tab", "[":
		if m.focusPane == 1 {
			m.activeTab = (m.activeTab + tabCount - 1) % tabCount
			return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
		}

	case "1", "2", "3", "4":
		if m.focusPane == 1 {
			m.activeTab = int(msg.String()[0] - '1')
			return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
		}

	case "/":
		if m.focusPane == 1 {
			if m.activeTab == 1 {
				m.logSearching = true
				m.logSearchInput.Focus()
				return m, textinput.Blink
			}
			if m.activeTab == 2 && m.selectedTable == "" {
				if m.tableViewDiagram {
					m.tableViewDiagram = false
				}
				m.tableFiltering = true
				m.tableFilterInput.Focus()
				m.renderTableOverviewToVP()
				return m, textinput.Blink
			}
			return m, nil
		}
		m.filtering = true
		m.filterInput.Focus()
		return m, textinput.Blink

	case "esc":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable != "" {
			m.selectedTable = ""
			m.tableDetail = nil
			m.renderTableOverviewToVP()
			return m, nil
		}
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && strings.TrimSpace(m.tableFilterInput.Value()) != "" {
			m.tableFilterInput.SetValue("")
			m.tableCursor = 0
			m.renderTableOverviewToVP()
			return m, nil
		}
		if m.focusPane == 1 && m.activeTab == 1 && m.logSearchQuery != "" {
			m.logSearchQuery = ""
			m.logSearchInput.SetValue("")
			m.refreshLogsViewport()
			return m, nil
		}
		if m.focusPane == 1 {
			m.focusPane = 0
			if m.activeTab == 2 && m.selectedTable == "" {
				m.renderTableOverviewToVP()
			}
			return m, nil
		}
		if strings.TrimSpace(m.filterInput.Value()) != "" {
			m.filterInput.SetValue("")
			m.applyFilter()
			return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
		}

	case "up", "k":
		if m.focusPane == 0 {
			if m.cursor > 0 {
				m.cursor--
				m.ensureLeftVisible()
				m.resetRightSubstate()
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
		} else {
			return m.scrollRight(-1)
		}

	case "down", "j":
		if m.focusPane == 0 {
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.ensureLeftVisible()
				m.resetRightSubstate()
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
		} else {
			return m.scrollRight(1)
		}

	case "pgup", "ctrl+u":
		return m.scrollRight(-8)

	case "pgdown", "ctrl+d":
		return m.scrollRight(8)

	case "shift+left", "<":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram && msg.String() == "shift+left" {
			m.panERD(-8, 0)
			return m, nil
		}
		if m.focusPane == 1 && m.activeTab == 3 && m.queryResult != nil && m.queryResult.isSelect {
			m.queryXOffset = max(0, m.queryXOffset-8)
			m.renderQueryResultToVP()
			return m, nil
		}

	case "shift+right", ">":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram && msg.String() == "shift+right" {
			m.panERD(8, 0)
			return m, nil
		}
		if m.focusPane == 1 && m.activeTab == 3 && m.queryResult != nil && m.queryResult.isSelect {
			w := max(24, m.queryVP.Width)
			maxX := max(0, m.queryTotalW-w)
			m.queryXOffset = min(maxX, m.queryXOffset+8)
			m.renderQueryResultToVP()
			return m, nil
		}

	case "shift+up":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			m.panERD(0, -3)
			return m, nil
		}

	case "shift+down":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			m.panERD(0, 3)
			return m, nil
		}

	case "+", "=":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			if m.erdZoom < 2 {
				m.erdZoom++
				m.renderTableOverviewToVP()
			}
			return m, nil
		}

	case "-":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			if m.erdZoom > 0 {
				m.erdZoom--
				m.renderTableOverviewToVP()
			}
			return m, nil
		}

	case "0":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			m.erdZoom = 1
			m.erdXOffset = 0
			m.erdYOffset = 0
			m.renderTableOverviewToVP()
			return m, nil
		}
		if m.focusPane == 1 && m.activeTab == 3 && m.queryResult != nil && m.queryResult.isSelect {
			m.queryXOffset = 0
			m.renderQueryResultToVP()
			return m, nil
		}

	case "enter", " ":
		if m.focusPane == 0 {
			if row := m.selectedRow(); row != nil && row.isGroup {
				m.collapsedGroups[row.group] = !m.collapsedGroups[row.group]
				m.rebuildRows()
				return m, nil
			}
			if m.selected() != nil && msg.String() == "enter" {
				m.focusPane = 1
				if m.activeTab == 2 && m.selectedTable == "" {
					m.renderTableOverviewToVP()
				}
				return m, nil
			}
		} else if m.activeTab == 2 {
			vis := m.visibleTables()
			if m.selectedTable == "" && len(vis) > 0 && m.tableCursor < len(vis) {
				sel := m.selected()
				if sel != nil && sel.running {
					tName := vis[m.tableCursor]
					m.selectedTable = tName
					m.busy = fmt.Sprintf("Inspecting '%s'...", tName)
					return m, tea.Batch(m.spinner.Tick, loadTableDetailCmd(sel.db, tName))
				}
			}
		} else if m.activeTab == 3 && msg.String() == "enter" {
			m.queryInput.Focus()
			return m, textinput.Blink
		}

	case "i":
		if m.focusPane == 1 && m.activeTab == 3 {
			m.queryInput.Focus()
			return m, textinput.Blink
		}

	case "v":
		if m.focusPane == 1 && m.activeTab == 2 {
			m.tableViewDiagram = !m.tableViewDiagram
			if m.selectedTable != "" {
				m.renderTableDetailToVP()
			} else {
				m.renderTableOverviewToVP()
			}
			modeLabel := "list/table"
			if m.tableViewDiagram {
				modeLabel = "diagram"
			}
			return m, m.setToast("Tables view: "+modeLabel, false)
		}

	case "f":
		if m.focusPane == 1 && m.activeTab == 1 {
			m.logFollow = !m.logFollow
			if m.logFollow {
				m.logsVP.GotoBottom()
			}
			stateLabel := "off"
			if m.logFollow {
				stateLabel = "on"
			}
			return m, m.setToast("Log auto-follow: "+stateLabel, false)
		}
		if m.focusPane == 1 && m.activeTab == 2 {
			m.tableShowFields = !m.tableShowFields
			if m.selectedTable != "" {
				m.renderTableDetailToVP()
			} else {
				m.renderTableOverviewToVP()
			}
			stateLabel := "hidden"
			if m.tableShowFields {
				stateLabel = "shown"
			}
			return m, m.setToast("Table fields: "+stateLabel, false)
		}

	case "N":
		if m.focusPane == 1 && m.activeTab == 1 && len(m.logMatchLines) > 0 {
			m.logMatchCurrent = (m.logMatchCurrent + len(m.logMatchLines) - 1) % len(m.logMatchLines)
			m.logsVP.SetYOffset(max(0, m.logMatchLines[m.logMatchCurrent]-2))
			return m, nil
		}

	case "n":
		if m.focusPane == 1 {
			if m.activeTab == 1 && m.logSearchQuery != "" && len(m.logMatchLines) > 0 {
				m.logMatchCurrent = (m.logMatchCurrent + 1) % len(m.logMatchLines)
				m.logsVP.SetYOffset(max(0, m.logMatchLines[m.logMatchCurrent]-2))
			}
			return m, nil
		}
		stateMgr, err := state.NewManager()
		if err != nil {
			return m, m.setToast(err.Error(), true)
		}
		cm := newCreateModel(stateMgr)
		m.createWizard = &cm
		m.modal = modalCreate
		return m, textinput.Blink

	case "p":
		if m.focusPane == 1 && m.activeTab == 0 {
			m.showPassword = !m.showPassword
		}

	case "r":
		m.busy = "Refreshing..."
		return m, tea.Batch(m.spinner.Tick, loadDatabasesCmd(m.runtimeMode))

	case "s":
		if m.focusPane != 0 || m.busy != "" {
			return m, nil
		}
		if row := m.selectedRow(); row != nil && row.isGroup {
			entries := m.groupEntries(row.group)
			if len(entries) > 0 {
				m.busy = fmt.Sprintf("Toggling group '%s'...", row.group)
				return m, tea.Batch(m.spinner.Tick, toggleGroupCmd(m.runtimeMode, row.group, entries))
			}
		}
		if sel := m.selected(); sel != nil {
			if sel.running {
				m.busy = fmt.Sprintf("Stopping '%s'...", sel.db.Name)
			} else {
				m.busy = fmt.Sprintf("Starting '%s'...", sel.db.Name)
			}
			return m, tea.Batch(m.spinner.Tick, toggleDBCmd(m.runtimeMode, *sel))
		}

	case "Q":
		if m.focusPane == 1 && m.activeTab == 2 {
			sel := m.selected()
			if sel == nil || !sel.running {
				return m, m.setToast("Database must be running to query table", true)
			}
			tName := m.selectedTable
			if tName == "" {
				vis := m.visibleTables()
				if len(vis) > 0 && m.tableCursor < len(vis) {
					tName = vis[m.tableCursor]
				}
			}
			if tName == "" {
				return m, nil
			}
			q := buildTableSelectQuery(sel.db.Provider, tName)
			m.activeTab = 3
			m.queryInput.SetValue(q)
			m.queryInput.Blur()
			m.queryHistory = recordQueryHistory(m.queryHistory, q)
			_ = saveQueryHistory(m.queryHistory)
			m.queryHistIdx = -1
			m.queryDraft = ""
			m.busy = "Running query..."
			return m, tea.Batch(m.spinner.Tick, runQuickSQLCmd(sel.db, q))
		}

	case "e":
		if m.focusPane == 1 && m.activeTab == 3 && m.busy == "" {
			sel := m.selected()
			if sel == nil || m.queryResult == nil || !m.queryResult.isSelect {
				return m, m.setToast("Run a SELECT query before exporting", true)
			}
			m.exportState = newExportModalState(sel.db.Name)
			m.modal = modalExport
			return m, textinput.Blink
		}

	case "c":
		if m.focusPane != 1 {
			return m, nil
		}
		if m.activeTab == 0 {
			if sel := m.selected(); sel != nil && sel.connStr != "" {
				if err := clipboard.WriteAll(sel.connStr); err != nil {
					return m, m.setToast(fmt.Sprintf("Clipboard error: %v", err), true)
				}
				return m, m.setToast(fmt.Sprintf("Copied connection string for '%s'", sel.db.Name), false)
			}
		} else if m.activeTab == 3 {
			if m.queryResult == nil || !m.queryResult.isSelect {
				return m, m.setToast("Run a SELECT query before copying", true)
			}
			data, err := formatQueryResultJSON(m.queryResult)
			if err != nil {
				return m, m.setToast(err.Error(), true)
			}
			if err := clipboard.WriteAll(string(data)); err != nil {
				return m, m.setToast(fmt.Sprintf("Clipboard error: %v", err), true)
			}
			return m, m.setToast(fmt.Sprintf("Copied %d row(s) as JSON", len(m.queryResult.rows)), false)
		}

	case "x":
		if m.focusPane != 0 {
			return m, nil
		}
		if sel := m.selected(); sel != nil {
			if !sel.running {
				return m, m.setToast(fmt.Sprintf("'%s' is stopped — press 's' to start it first", sel.db.Name), true)
			}
			return m, tea.Exec(shellExecCmd{name: sel.db.Name, openShell: m.openShell}, func(err error) tea.Msg {
				return loadDatabasesCmd(m.runtimeMode)()
			})
		}

	case "b":
		if m.focusPane != 0 {
			return m, nil
		}
		if sel := m.selected(); sel != nil && m.busy == "" {
			if !sel.running {
				return m, m.setToast(fmt.Sprintf("'%s' is stopped — start it before backing up", sel.db.Name), true)
			}
			m.backupInput = newBackupInput(sel.db.Name)
			m.modal = modalBackup
			return m, textinput.Blink
		}

	case "C":
		if m.focusPane != 0 {
			return m, nil
		}
		if sel := m.selected(); sel != nil && m.busy == "" {
			m.cloneState = newCloneModalState(sel.db.Name)
			m.modal = modalClone
			return m, textinput.Blink
		}

	case "L":
		if m.focusPane != 0 {
			return m, nil
		}
		if sel := m.selected(); sel != nil && m.busy == "" {
			if !sel.running {
				return m, m.setToast(fmt.Sprintf("'%s' is stopped — start it before restoring", sel.db.Name), true)
			}
			m.restoreState = newRestoreModalState()
			m.modal = modalRestore
			if m.restoreState.customMode {
				m.restoreState.input.Focus()
				return m, textinput.Blink
			}
			return m, nil
		}

	case "d":
		if m.focusPane == 0 && m.selected() != nil && m.busy == "" {
			m.confirmMode = "remove"
		}

	case "R":
		if m.focusPane == 0 && m.selected() != nil && m.busy == "" {
			m.confirmMode = "reset"
		}
	}

	return m, nil
}

func (m dashboardModel) scrollRight(delta int) (tea.Model, tea.Cmd) {
	switch m.activeTab {
	case 1:
		if delta < 0 {
			m.logsVP.LineUp(-delta)
			m.logFollow = false
		} else {
			m.logsVP.LineDown(delta)
		}
	case 2:
		if m.selectedTable != "" {
			if delta < 0 {
				m.tableDetailVP.LineUp(-delta)
			} else {
				m.tableDetailVP.LineDown(delta)
			}
		} else if vis := m.visibleTables(); len(vis) > 0 {
			m.tableCursor = max(0, min(len(vis)-1, m.tableCursor+delta))
			m.renderTableOverviewToVP()
		}
	case 3:
		if delta < 0 {
			m.queryVP.LineUp(-delta)
		} else {
			m.queryVP.LineDown(delta)
		}
	}
	return m, nil
}

func (m dashboardModel) erdCanvasOrigin() (int, int) {
	stacked, leftW, leftH, _, _ := m.layoutDimensions()
	originX := leftW + 4
	originY := 5
	if stacked {
		originX = 2
		originY = leftH + 6
	}
	return originX, originY
}

func (m dashboardModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.modal != modalNone {
		return m, nil
	}
	stacked, leftW, leftH, _, _ := m.layoutDimensions()

	if m.erdDragging {
		if msg.Action == tea.MouseActionMotion {
			dx := m.erdDragLastX - msg.X
			dy := m.erdDragLastY - msg.Y
			if dx != 0 || dy != 0 {
				m.erdDragMoved = true
				m.panERD(dx, dy)
				m.erdDragLastX = msg.X
				m.erdDragLastY = msg.Y
			}
			return m, nil
		}
		if msg.Action == tea.MouseActionRelease {
			wasMoved := m.erdDragMoved
			m.erdDragging = false
			m.erdDragMoved = false
			if !wasMoved && m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
				originX, originY := m.erdCanvasOrigin()
				cx := msg.X - originX + m.erdXOffset
				cy := msg.Y - originY + m.erdYOffset
				if hit := erdHitTest(m.erdLayout, cx, cy); hit != "" {
					for i, t := range m.detailTables {
						if t == hit {
							m.tableCursor = i
							break
						}
					}
					if m.lastERDClickTable == hit && time.Since(m.lastERDClickAt) < 450*time.Millisecond {
						m.lastERDClickTable = ""
						sel := m.selected()
						if sel != nil && sel.running {
							m.selectedTable = hit
							m.busy = fmt.Sprintf("Inspecting '%s'...", hit)
							return m, tea.Batch(m.spinner.Tick, loadTableDetailCmd(sel.db, hit))
						}
					}
					m.lastERDClickTable = hit
					m.lastERDClickAt = time.Now()
					m.renderTableOverviewToVP()
				}
			}
			return m, nil
		}
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if (!stacked && msg.X < leftW+2) || (stacked && msg.Y < leftH+2) {
			if m.cursor > 0 {
				m.cursor--
				m.ensureLeftVisible()
				m.resetRightSubstate()
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
			return m, nil
		}
		if m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			m.panERD(0, -3)
			return m, nil
		}
		return m.scrollRight(-3)

	case tea.MouseButtonWheelDown:
		if (!stacked && msg.X < leftW+2) || (stacked && msg.Y < leftH+2) {
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.ensureLeftVisible()
				m.resetRightSubstate()
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
			return m, nil
		}
		if m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			m.panERD(0, 3)
			return m, nil
		}
		return m.scrollRight(3)

	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		// Click in left pane
		inLeft := (!stacked && msg.X < leftW+2) || (stacked && msg.Y >= 1 && msg.Y < leftH+2)
		if inLeft {
			m.focusPane = 0
			headerLines := 3 // top header + "Databases" + blank
			if m.filtering || strings.TrimSpace(m.filterInput.Value()) != "" {
				headerLines++
			}
			idx := m.leftOffset + (msg.Y - headerLines)
			if idx >= 0 && idx < len(m.rows) {
				if idx == m.cursor && m.rows[idx].isGroup {
					m.collapsedGroups[m.rows[idx].group] = !m.collapsedGroups[m.rows[idx].group]
					m.rebuildRows()
					return m, nil
				}
				m.cursor = idx
				m.ensureLeftVisible()
				m.resetRightSubstate()
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
			return m, nil
		}
		// Click in right pane
		m.focusPane = 1
		tabY := 2
		if stacked {
			tabY = leftH + 3
		}
		if msg.Y == tabY {
			relX := msg.X
			if !stacked {
				relX -= leftW + 4
			}
			if relX >= 0 {
				tabIdx := relX / 13
				if tabIdx >= 0 && tabIdx < tabCount {
					m.activeTab = tabIdx
					return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
				}
			}
			return m, nil
		}
		if m.activeTab == 2 && m.selectedTable == "" && m.tableViewDiagram {
			_, originY := m.erdCanvasOrigin()
			if msg.Y >= originY {
				m.erdDragging = true
				m.erdDragLastX = msg.X
				m.erdDragLastY = msg.Y
				m.erdDragMoved = false
			}
		}
	}
	return m, nil
}

func (m *dashboardModel) panERD(dx, dy int) {
	if m.erdLayout == nil || m.erdLayout.canvas == nil {
		return
	}
	viewW := max(24, m.tableOverviewVP.Width)
	viewH := max(4, m.tableOverviewVP.Height)
	maxX := max(0, m.erdLayout.canvas.w-viewW)
	maxY := max(0, m.erdLayout.canvas.h-viewH)
	m.erdXOffset = max(0, min(maxX, m.erdXOffset+dx))
	m.erdYOffset = max(0, min(maxY, m.erdYOffset+dy))
}
