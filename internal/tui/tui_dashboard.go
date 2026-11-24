package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/iqbalmu/xmdb/internal/providers"
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
	name           string
	tab            int
	logs           string
	tables         []string
	tableSchema    *tableSchemaGraph
	kvKeys         []providers.KVKeyInfo
	docCollections []docCollectionInfo
	err            error
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

func (s shellExecCmd) SetStdin(io.Reader) {}

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

	// Metrics cache (keyed by database name)
	metrics map[string]*dbMetrics

	// Tables / Keys / Collections tab & Data Explorer
	detailTables        []string
	tableSchema         *tableSchemaGraph
	kvKeys              []providers.KVKeyInfo
	docCollections      []docCollectionInfo
	tableCursor         int
	selectedTable       string
	tableDetail         *tableDetail
	tableOverviewVP     viewport.Model
	tableDetailVP       viewport.Model
	tableViewDiagram    bool
	tableShowFields     bool
	tableFiltering      bool
	tableFilterInput    textinput.Model
	explorerOpts        tableExplorerOpts
	explorerRowCursor   int
	explorerCursorLine  int // rendered line index of the selected preview row (-1 when none)
	explorerFiltering   bool
	explorerFilterInput textinput.Model
	erdXOffset          int
	erdYOffset          int
	erdZoom             int // 0=50% (out), 1=100% (normal), 2=150% (in)
	erdLayout           *erdLayoutResult
	erdDragging         bool
	erdDragLastX        int
	erdDragLastY        int
	erdDragMoved        bool
	lastERDClickTable   string
	lastERDClickAt      time.Time

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
	savedQueries []SavedQuery

	// Filter
	filtering   bool
	filterInput textinput.Model

	// Modals & confirmations
	confirmMode     string // "", "remove", "reset"
	modal           modalKind
	backupInput     textinput.Model
	restoreState    restoreModalState
	createWizard    *createModel
	cloneState      cloneModalState
	exportState     exportModalState
	snapshotState   snapshotModalState
	doctorState     doctorModalState
	editDBState     editDBModalState
	rowInspectState rowInspectModalState
	saveQueryState  saveQueryModalState
	snippetsState   snippetsModalState
	paletteState    paletteModalState

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

	efi := textinput.New()
	efi.Placeholder = "WHERE clause (e.g. status = 'active') or Mongo JSON filter"
	efi.CharLimit = 256
	efi.Width = 44

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorWarning)

	return dashboardModel{
		width:               100,
		height:              30,
		collapsedGroups:     make(map[string]bool),
		metrics:             make(map[string]*dbMetrics),
		filterInput:         fi,
		logSearchInput:      lsi,
		tableFilterInput:    tfi,
		explorerFilterInput: efi,
		explorerOpts:        tableExplorerOpts{PageSize: defaultExplorerPageSize},
		queryInput:          qi,
		queryHistory:        loadQueryHistory(),
		savedQueries:        loadSavedQueries(),
		queryHistIdx:        -1,
		logsVP:              viewport.New(50, 18),
		tableOverviewVP:     viewport.New(50, 18),
		tableDetailVP:       viewport.New(50, 18),
		tableViewDiagram:    true,
		tableShowFields:     true,
		erdZoom:             1,
		queryVP:             viewport.New(50, 14),
		logFollow:           true,
		spinner:             sp,
		busy:                "Loading databases...",
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

func (m dashboardModel) selectedEngineKind() providers.EngineKind {
	sel := m.selected()
	if sel == nil {
		return providers.EngineSQL
	}
	prov, err := providers.For(sel.db.Provider)
	if err != nil {
		return providers.EngineSQL
	}
	return prov.Kind()
}

func (m *dashboardModel) syncQueryPrompt() {
	switch m.selectedEngineKind() {
	case providers.EngineKV:
		m.queryInput.Prompt = "KV> "
		m.queryInput.Placeholder = "GET mykey  |  KEYS *  |  INFO server"
	case providers.EngineDocument:
		m.queryInput.Prompt = "MONGO> "
		m.queryInput.Placeholder = "db.users.find().limit(10).toArray()"
	default:
		m.queryInput.Prompt = "SQL> "
		m.queryInput.Placeholder = "SELECT * FROM users LIMIT 10;"
	}
}

func (m *dashboardModel) resetRightSubstate() {
	m.selectedTable = ""
	m.tableDetail = nil
	m.tableSchema = nil
	m.kvKeys = nil
	m.docCollections = nil
	m.tableCursor = 0
	m.tableFiltering = false
	m.tableFilterInput.Blur()
	m.tableFilterInput.SetValue("")
	m.explorerOpts = tableExplorerOpts{PageSize: defaultExplorerPageSize}
	m.explorerRowCursor = 0
	m.explorerFiltering = false
	m.explorerFilterInput.Blur()
	m.explorerFilterInput.SetValue("")
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
	m.syncQueryPrompt()
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
		m.syncQueryPrompt()
		cmds := []tea.Cmd{loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)}
		if m.activeTab == 0 {
			cmds = append(cmds, loadMetricsCmd(m.runtimeMode, m.selected()))
		}
		return m, tea.Batch(cmds...)

	case msgMetricsLoaded:
		if msg.metrics != nil {
			if m.metrics == nil {
				m.metrics = make(map[string]*dbMetrics)
			}
			m.metrics[msg.name] = msg.metrics
		}
		return m, nil

	case msgSnapshotsLoaded:
		if m.modal == modalSnapshots && m.snapshotState.dbName == msg.dbName {
			m.snapshotState.loading = false
			m.snapshotState.snaps = msg.snaps
			if msg.err != nil {
				m.snapshotState.errMsg = msg.err.Error()
			}
			if m.snapshotState.cursor >= len(msg.snaps) {
				m.snapshotState.cursor = max(0, len(msg.snaps)-1)
			}
		}
		return m, nil

	case msgDoctorLoaded:
		if m.modal == modalDoctor {
			m.doctorState.loading = false
			m.doctorState.report = msg.report
			if msg.err != nil {
				m.doctorState.errMsg = msg.err.Error()
			}
		}
		return m, nil

	case msgDetailLoaded:
		if sel := m.selected(); sel != nil && sel.db.Name == msg.name && m.activeTab == msg.tab {
			m.detailLogs = msg.logs
			m.detailTables = msg.tables
			m.tableSchema = msg.tableSchema
			m.kvKeys = msg.kvKeys
			m.docCollections = msg.docCollections
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
			if m.tableDetail != nil && m.explorerRowCursor >= len(m.tableDetail.previewRows) {
				m.explorerRowCursor = max(0, len(m.tableDetail.previewRows)-1)
			}
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
		return m.handleKeyMsg(msg)
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
