package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/iqbalmu/xmdb/internal/state"
)

func (m dashboardModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

	// Right-pane Data Explorer filter input (WHERE / Mongo JSON filter)
	if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable != "" && m.explorerFiltering {
		switch msg.String() {
		case "esc":
			m.explorerFiltering = false
			m.explorerFilterInput.Blur()
			m.explorerFilterInput.SetValue(m.explorerOpts.Filter)
			return m, nil
		case "enter":
			m.explorerFiltering = false
			m.explorerFilterInput.Blur()
			m.explorerOpts.Filter = strings.TrimSpace(m.explorerFilterInput.Value())
			m.explorerOpts.Page = 0
			m.explorerRowCursor = 0
			if sel := m.selected(); sel != nil && sel.running {
				m.busy = fmt.Sprintf("Filtering '%s'...", m.selectedTable)
				return m, tea.Batch(m.spinner.Tick, loadTableDetailWithOptsCmd(m.runtimeMode, sel.db, m.selectedTable, m.explorerOpts))
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.explorerFilterInput, cmd = m.explorerFilterInput.Update(msg)
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
		case "ctrl+s":
			if strings.TrimSpace(m.queryInput.Value()) == "" {
				return m, m.setToast("Write a query before saving", true)
			}
			m.saveQueryState = newSaveQueryModalState()
			m.modal = modalSaveQuery
			return m, textinput.Blink
		case "ctrl+o":
			provName := ""
			if sel := m.selected(); sel != nil {
				provName = sel.db.Provider
			}
			m.snippetsState = snippetsModalState{
				items: filterSavedQueries(m.savedQueries, provName, ""),
			}
			m.modal = modalSnippets
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
			return m, tea.Batch(m.spinner.Tick, runQuickSQLCmd(m.runtimeMode, sel.db, q))
		}
		m.queryHistIdx = -1
		var cmd tea.Cmd
		m.queryInput, cmd = m.queryInput.Update(msg)
		return m, cmd
	}

	return m.handleNormalKey(msg)
}

func (m dashboardModel) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "ctrl+p":
		m.paletteState = newPaletteModalState()
		m.modal = modalPalette
		return m, textinput.Blink

	case "S":
		if sel := m.selected(); sel != nil && m.busy == "" {
			m.snapshotState = newSnapshotModalState(sel.db.Name)
			m.modal = modalSnapshots
			return m, loadSnapshotsCmd(sel.db.Name)
		}

	case "D":
		if m.busy == "" {
			m.doctorState = doctorModalState{loading: true}
			m.modal = modalDoctor
			return m, runDoctorCmd(m.runtimeMode)
		}

	case "E":
		if m.focusPane == 1 && m.activeTab == 3 && m.busy == "" {
			sel := m.selected()
			if sel == nil || !sel.running {
				return m, m.setToast("Database must be running to run EXPLAIN", true)
			}
			eq, err := buildExplainQuery(sel.db.Provider, m.queryInput.Value())
			if err != nil {
				return m, m.setToast(err.Error(), true)
			}
			m.busy = "Running EXPLAIN..."
			return m, tea.Batch(m.spinner.Tick, runQuickSQLCmd(m.runtimeMode, sel.db, eq))
		}
		if (m.focusPane == 0 || m.activeTab == 0) && m.busy == "" {
			if sel := m.selected(); sel != nil {
				m.editDBState = newEditDBModalState(sel.db)
				m.modal = modalEditDB
				return m, textinput.Blink
			}
		}

	case "ctrl+s":
		if m.focusPane == 1 && m.activeTab == 3 {
			if strings.TrimSpace(m.queryInput.Value()) == "" {
				return m, m.setToast("Write a query before saving", true)
			}
			m.saveQueryState = newSaveQueryModalState()
			m.modal = modalSaveQuery
			return m, textinput.Blink
		}

	case "ctrl+o", "O":
		if m.focusPane == 1 && m.activeTab == 3 {
			provName := ""
			if sel := m.selected(); sel != nil {
				provName = sel.db.Provider
			}
			m.snippetsState = snippetsModalState{
				items: filterSavedQueries(m.savedQueries, provName, ""),
			}
			m.modal = modalSnippets
			return m, nil
		}

	case "W":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable != "" {
			m.explorerFiltering = true
			m.explorerFilterInput.SetValue(m.explorerOpts.Filter)
			m.explorerFilterInput.Focus()
			return m, textinput.Blink
		}

	case "o":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable != "" && m.tableDetail != nil && len(m.tableDetail.previewCols) > 0 {
			cols := m.tableDetail.previewCols
			if m.explorerOpts.SortCol == "" {
				m.explorerOpts.SortCol = cols[0]
				m.explorerOpts.SortDesc = false
			} else if !m.explorerOpts.SortDesc {
				m.explorerOpts.SortDesc = true
			} else {
				idx := -1
				for i, c := range cols {
					if c == m.explorerOpts.SortCol {
						idx = i
						break
					}
				}
				if idx >= 0 && idx+1 < len(cols) {
					m.explorerOpts.SortCol = cols[idx+1]
					m.explorerOpts.SortDesc = false
				} else {
					m.explorerOpts.SortCol = ""
					m.explorerOpts.SortDesc = false
				}
			}
			m.explorerOpts.Page = 0
			m.explorerRowCursor = 0
			if sel := m.selected(); sel != nil && sel.running {
				m.busy = fmt.Sprintf("Sorting '%s'...", m.selectedTable)
				return m, tea.Batch(m.spinner.Tick, loadTableDetailWithOptsCmd(m.runtimeMode, sel.db, m.selectedTable, m.explorerOpts))
			}
			return m, nil
		}

	case ",":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable != "" && m.explorerOpts.Page > 0 {
			m.explorerOpts.Page--
			m.explorerRowCursor = 0
			if sel := m.selected(); sel != nil && sel.running {
				m.busy = fmt.Sprintf("Loading page %d...", m.explorerOpts.Page+1)
				return m, tea.Batch(m.spinner.Tick, loadTableDetailWithOptsCmd(m.runtimeMode, sel.db, m.selectedTable, m.explorerOpts))
			}
			return m, nil
		}

	case ".":
		if m.focusPane == 1 && m.activeTab == 2 && m.selectedTable != "" && m.tableDetail != nil {
			pageSize := max(1, m.tableDetail.pageSize)
			totalPages := max(1, int((m.tableDetail.totalRows+int64(pageSize)-1)/int64(pageSize)))
			if m.explorerOpts.Page+1 < totalPages {
				m.explorerOpts.Page++
				m.explorerRowCursor = 0
				if sel := m.selected(); sel != nil && sel.running {
					m.busy = fmt.Sprintf("Loading page %d...", m.explorerOpts.Page+1)
					return m, tea.Batch(m.spinner.Tick, loadTableDetailWithOptsCmd(m.runtimeMode, sel.db, m.selectedTable, m.explorerOpts))
				}
			}
			return m, nil
		}

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
			if m.activeTab == 0 {
				return m, loadMetricsCmd(m.runtimeMode, m.selected())
			}
			return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
		}

	case "shift+tab", "[":
		if m.focusPane == 1 {
			m.activeTab = (m.activeTab + tabCount - 1) % tabCount
			if m.activeTab == 0 {
				return m, loadMetricsCmd(m.runtimeMode, m.selected())
			}
			return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
		}

	case "1", "2", "3", "4":
		if m.focusPane == 1 {
			m.activeTab = int(msg.String()[0] - '1')
			if m.activeTab == 0 {
				return m, loadMetricsCmd(m.runtimeMode, m.selected())
			}
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
				if m.activeTab == 0 {
					return m, loadMetricsCmd(m.runtimeMode, m.selected())
				}
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
		} else {
			if m.activeTab == 2 && m.selectedTable != "" && m.tableDetail != nil && len(m.tableDetail.previewRows) > 0 {
				if m.explorerRowCursor > 0 {
					m.explorerRowCursor--
					m.renderTableDetailToVP()
					m.followExplorerCursor()
					return m, nil
				}
			}
			return m.scrollRight(-1)
		}

	case "down", "j":
		if m.focusPane == 0 {
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				m.ensureLeftVisible()
				m.resetRightSubstate()
				if m.activeTab == 0 {
					return m, loadMetricsCmd(m.runtimeMode, m.selected())
				}
				return m, loadDetailCmd(m.runtimeMode, m.selected(), m.activeTab)
			}
		} else {
			if m.activeTab == 2 && m.selectedTable != "" && m.tableDetail != nil && len(m.tableDetail.previewRows) > 0 {
				if m.explorerRowCursor < len(m.tableDetail.previewRows)-1 {
					m.explorerRowCursor++
					m.renderTableDetailToVP()
					m.followExplorerCursor()
					return m, nil
				}
			}
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
			if m.selectedTable != "" && msg.String() == "enter" && m.tableDetail != nil && len(m.tableDetail.previewRows) > 0 {
				idx := min(max(0, m.explorerRowCursor), len(m.tableDetail.previewRows)-1)
				rowNum := m.tableDetail.page*max(1, m.tableDetail.pageSize) + idx + 1
				title := fmt.Sprintf("Row #%d — %s", rowNum, m.selectedTable)
				m.rowInspectState = newRowInspectModalState(title, m.tableDetail.previewCols, m.tableDetail.previewRows[idx], m.width, m.height)
				m.modal = modalRowInspect
				return m, nil
			}
			vis := m.visibleTables()
			if m.selectedTable == "" && len(vis) > 0 && m.tableCursor < len(vis) {
				sel := m.selected()
				if sel != nil && sel.running {
					tName := vis[m.tableCursor]
					m.selectedTable = tName
					m.explorerOpts = tableExplorerOpts{PageSize: defaultExplorerPageSize}
					m.explorerRowCursor = 0
					m.busy = fmt.Sprintf("Inspecting '%s'...", tName)
					return m, tea.Batch(m.spinner.Tick, loadTableDetailWithOptsCmd(m.runtimeMode, sel.db, tName, m.explorerOpts))
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
			return m, tea.Batch(m.spinner.Tick, runQuickSQLCmd(m.runtimeMode, sel.db, q))
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
			if m.queryResult == nil {
				return m, m.setToast("Run a query before copying", true)
			}
			if m.queryResult.rawOutput != "" {
				if err := clipboard.WriteAll(m.queryResult.rawOutput); err != nil {
					return m, m.setToast(fmt.Sprintf("Clipboard error: %v", err), true)
				}
				return m, m.setToast("Copied command output to clipboard", false)
			}
			if !m.queryResult.isSelect {
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
