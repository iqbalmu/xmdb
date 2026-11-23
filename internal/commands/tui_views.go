package commands

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *dashboardModel) renderTableOverviewToVP() {
	if len(m.detailTables) == 0 {
		m.erdLayout = nil
		m.tableOverviewVP.SetContent(styleMutedText.Render("(no tables found)"))
		return
	}
	w := max(24, m.tableOverviewVP.Width)
	h := max(4, m.tableOverviewVP.Height)
	focused := m.focusPane == 1
	if m.tableViewDiagram {
		m.erdLayout = buildTableOverviewERDWithZoom(m.detailTables, m.tableSchema, m.tableCursor, focused, m.tableShowFields, m.erdZoom)
		if m.erdLayout != nil && m.erdLayout.canvas != nil {
			cr := m.erdLayout.cursorRect
			if cr.w > 0 && cr.h > 0 {
				if cr.x < m.erdXOffset {
					m.erdXOffset = max(0, cr.x-2)
				} else if cr.x+cr.w > m.erdXOffset+w {
					m.erdXOffset = max(0, cr.x+cr.w-w+2)
				}
				if cr.y < m.erdYOffset {
					m.erdYOffset = max(0, cr.y-1)
				} else if cr.y+cr.h > m.erdYOffset+h {
					m.erdYOffset = max(0, cr.y+cr.h-h+1)
				}
			}
			m.panERD(0, 0)
		}
		return
	}

	m.erdLayout = nil
	vis := m.visibleTables()
	if len(vis) == 0 {
		m.tableOverviewVP.SetContent(styleMutedText.Render("(no matching tables)"))
		return
	}
	content, cursorLine := formatTableOverviewList(vis, m.tableSchema, m.tableCursor, focused, m.tableShowFields, w)
	m.tableOverviewVP.SetContent(content)
	// Keep selected table/card visible within viewport.
	if m.tableOverviewVP.Height > 0 {
		if cursorLine < m.tableOverviewVP.YOffset {
			m.tableOverviewVP.SetYOffset(cursorLine)
		} else if cursorLine >= m.tableOverviewVP.YOffset+m.tableOverviewVP.Height {
			m.tableOverviewVP.SetYOffset(max(0, cursorLine-m.tableOverviewVP.Height+3))
		}
	}
}

func (m *dashboardModel) renderTableDetailToVP() {
	if m.tableDetail == nil {
		return
	}
	w := max(24, m.tableDetailVP.Width)
	var b strings.Builder

	if m.tableViewDiagram {
		b.WriteString(formatTableDetailDiagram(m.tableDetail, m.tableSchema, m.tableShowFields, w))
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Schema: "+m.tableDetail.tableName) + "\n")
		if m.tableShowFields {
			b.WriteString(formatCompactTable(m.tableDetail.schemaCols, m.tableDetail.schemaRows, w))
		} else {
			b.WriteString(styleMutedText.Render(fmt.Sprintf("(%d fields hidden — press 'f' to show)", len(m.tableDetail.schemaRows))) + "\n")
		}
	}

	b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Preview (up to 25 rows)") + "\n")
	if len(m.tableDetail.previewCols) == 0 {
		b.WriteString(styleMutedText.Render("(unable to preview rows or empty)") + "\n")
	} else {
		b.WriteString(formatCompactTable(m.tableDetail.previewCols, m.tableDetail.previewRows, w))
	}
	m.tableDetailVP.SetContent(b.String())
	m.tableDetailVP.GotoTop()
}

func (m *dashboardModel) renderQueryResultToVP() {
	if m.queryErr != nil {
		m.queryVP.SetContent(styleErrorText.Render("SQL Error:\n" + m.queryErr.Error()))
		m.queryVP.GotoTop()
		return
	}
	if m.queryResult == nil {
		m.queryVP.SetContent(styleMutedText.Render("Press 'i' or Enter to type a SQL query."))
		return
	}
	dur := m.queryResult.duration.Round(time.Millisecond)
	if !m.queryResult.isSelect {
		m.queryVP.SetContent(styleToastOK.Render(fmt.Sprintf("✓ Query executed (%d row(s) affected) in %s", m.queryResult.affected, dur)))
		m.queryVP.GotoTop()
		return
	}
	w := max(24, m.queryVP.Width)
	tableStr, totalW := formatCompactTableWithOffset(m.queryResult.cols, m.queryResult.rows, w, m.queryXOffset)
	m.queryTotalW = totalW
	scrollInfo := ""
	if totalW > w {
		scrollInfo = fmt.Sprintf("  •  col offset %d (Shift+←/→ or </> to scroll, 0 to reset)", m.queryXOffset)
	}
	header := styleMutedText.Render(fmt.Sprintf("%d row(s) returned in %s (max 200 shown)%s", len(m.queryResult.rows), dur, scrollInfo))
	m.queryVP.SetContent(header + "\n\n" + tableStr)
	m.queryVP.GotoTop()
}

func (m dashboardModel) View() string {
	dockerBadge := styleBadgeRun.Render("● runtime ok")
	if m.dockerErr != nil {
		dockerBadge = styleErrorText.Render("✗ runtime unavailable")
	}
	header := fmt.Sprintf("%s   %s   %s",
		styleHeader.Render(" xmdb "),
		dockerBadge,
		styleMutedText.Render(fmt.Sprintf("%d database(s)", len(m.entries))),
	)

	stacked, leftW, leftH, rightW, rightH := m.layoutDimensions()
	leftPane := m.renderLeftPane(leftW, leftH)
	rightPane := m.renderRightPane(rightW, rightH)

	var body string
	if stacked {
		body = lipgloss.JoinVertical(lipgloss.Left, leftPane, rightPane)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
	}

	footer := m.renderFooter()
	base := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)

	switch m.modal {
	case modalHelp:
		return renderOverlay(base, renderHelpModal(m.width), m.width, m.height)
	case modalBackup:
		name := ""
		if sel := m.selected(); sel != nil {
			name = sel.db.Name
		}
		return renderOverlay(base, renderBackupModal(name, m.backupInput, m.width), m.width, m.height)
	case modalRestore:
		name := ""
		if sel := m.selected(); sel != nil {
			name = sel.db.Name
		}
		return renderOverlay(base, renderRestoreModal(name, m.restoreState, m.width), m.width, m.height)
	case modalCreate:
		if m.createWizard != nil {
			w := min(74, max(48, m.width-6))
			box := styleModal.Width(w).Render(m.createWizard.ViewContent(w - 6))
			return renderOverlay(base, box, m.width, m.height)
		}
	case modalClone:
		name := ""
		if sel := m.selected(); sel != nil {
			name = sel.db.Name
		}
		return renderOverlay(base, renderCloneModal(name, m.cloneState, m.width), m.width, m.height)
	case modalExport:
		name := ""
		if sel := m.selected(); sel != nil {
			name = sel.db.Name
		}
		return renderOverlay(base, renderExportModal(name, m.exportState, m.width), m.width, m.height)
	}
	return base
}

func (m dashboardModel) renderLeftPane(width, height int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("Databases") + "\n")

	if m.filtering {
		b.WriteString(m.filterInput.View() + "\n")
	} else if q := strings.TrimSpace(m.filterInput.Value()); q != "" {
		b.WriteString(styleMutedText.Render("Filter: "+q+" (Esc to clear)") + "\n")
	}
	b.WriteString("\n")

	if len(m.rows) == 0 {
		if len(m.entries) == 0 {
			b.WriteString(styleMutedText.Render("No databases yet.\nPress 'n' to create one!"))
		} else {
			b.WriteString(styleMutedText.Render("No databases match filter."))
		}
	} else {
		innerW := max(16, width-4)
		vis := m.leftVisibleLines()
		end := min(len(m.rows), m.leftOffset+vis)
		for i := m.leftOffset; i < end; i++ {
			r := m.rows[i]
			var line string
			if r.isGroup {
				arrow := "▼"
				if m.collapsedGroups[r.group] {
					arrow = "▶"
				}
				label := fmt.Sprintf("%s %s (%d/%d running)", arrow, r.group, r.runningCount, r.count)
				if lipgloss.Width(label) > innerW {
					label = ansi.Truncate(label, innerW-1, "…")
				}
				if i == m.cursor {
					line = styleSelectedRow.Width(innerW).Render(label)
				} else {
					line = styleGroupHeader.Render(label)
				}
			} else if r.entry != nil {
				e := r.entry
				badge := styleBadgeStop.Render("○")
				if e.running {
					badge = styleBadgeRun.Render("●")
				}
				txt := fmt.Sprintf("  %s %s (%s:%d)", badge, e.db.Name, e.db.Provider, e.db.Port)
				if lipgloss.Width(txt) > innerW {
					txt = ansi.Truncate(txt, innerW-1, "…")
				}
				if i == m.cursor {
					line = styleSelectedRow.Width(innerW).Render(txt)
				} else {
					line = txt
				}
			}
			b.WriteString(line + "\n")
		}
	}

	paneStyle := stylePane
	if m.focusPane == 0 {
		paneStyle = stylePaneFocused
	}
	return paneStyle.Width(width).Height(height).Render(b.String())
}

func (m dashboardModel) renderRightPane(width, height int) string {
	tabs := []string{"[1] Info", "[2] Logs", "[3] Tables", "[4] Query"}
	var tabParts []string
	for i, t := range tabs {
		if i == m.activeTab {
			tabParts = append(tabParts, styleTabActive.Render(t))
		} else {
			tabParts = append(tabParts, styleTabInactive.Render(t))
		}
	}
	tabBar := strings.Join(tabParts, " ")

	paneStyle := stylePane
	if m.focusPane == 1 {
		paneStyle = stylePaneFocused
	}

	if row := m.selectedRow(); row != nil && row.isGroup {
		var gb strings.Builder
		gb.WriteString(tabBar + "\n\n")
		gb.WriteString(styleGroupHeader.Render("Group: "+row.group) + "\n")
		gb.WriteString(fmt.Sprintf("Databases: %d (%d running)\n\n", row.count, row.runningCount))
		for _, e := range m.groupEntries(row.group) {
			badge := styleBadgeStop.Render("○ stopped")
			if e.running {
				badge = styleBadgeRun.Render("● running")
			}
			gb.WriteString(fmt.Sprintf("  %s  %s  (%s:%d)\n", badge, e.db.Name, e.db.Provider, e.db.Port))
		}
		gb.WriteString("\n" + styleMutedText.Render("Enter/Space: expand/collapse  •  s: start/stop all in group"))
		return paneStyle.Width(width).Height(height).Render(gb.String())
	}

	sel := m.selected()
	if sel == nil {
		content := tabBar + "\n\n" + styleMutedText.Render("Select a database from the list.")
		return paneStyle.Width(width).Height(height).Render(content)
	}

	var body string
	switch m.activeTab {
	case 0:
		body = m.renderInfoTab(sel)
	case 1:
		body = m.renderLogsTab()
	case 2:
		body = m.renderTablesTab()
	case 3:
		body = m.renderQueryTab()
	}

	content := tabBar + "\n\n" + body
	return paneStyle.Width(width).Height(height).Render(content)
}

func (m dashboardModel) renderInfoTab(e *dbEntry) string {
	db := e.db
	statusStr := styleBadgeStop.Render("○ stopped")
	if e.running {
		statusStr = styleBadgeRun.Render("● running")
	}

	pass := "•••••••••••• (press 'p' to reveal)"
	if m.showPassword {
		pass = db.GetPassword()
	}

	rows := [][2]string{
		{"Name", db.Name},
		{"Provider", fmt.Sprintf("%s (%s)", db.Provider, db.Version)},
		{"Group", groupLabel(db.Group)},
		{"Status", statusStr},
		{"Port", fmt.Sprintf("127.0.0.1:%d", db.Port)},
		{"Database", db.DatabaseName},
		{"User", db.GetUsername()},
		{"Password", pass},
		{"Container", db.ContainerName},
		{"Volume", db.VolumeName},
		{"Created", db.CreatedAt.Format("2006-01-02 15:04")},
	}

	var b strings.Builder
	for _, r := range rows {
		b.WriteString(styleLabel.Render(r[0]+":") + " " + r[1] + "\n")
	}
	b.WriteString("\n" + styleLabel.Render("Conn String:") + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Render(e.connStr) + "\n")
	return b.String()
}

func (m dashboardModel) renderLogsTab() string {
	var b strings.Builder
	followBadge := styleMutedText.Render("follow:off")
	if m.logFollow {
		followBadge = styleBadgeRun.Render("follow:on")
	}
	status := fmt.Sprintf("%s  •  f: follow  •  /: search", followBadge)
	if m.logSearching {
		status = m.logSearchInput.View()
	} else if m.logSearchQuery != "" {
		status = fmt.Sprintf("search: %q (%d matches, n/N: jump, Esc: clear)", m.logSearchQuery, len(m.logMatchLines))
	}
	b.WriteString(styleMutedText.Render(status) + "\n")
	b.WriteString(m.logsVP.View())
	return b.String()
}

func (m dashboardModel) renderTablesTab() string {
	if m.detailErr != nil {
		return styleMutedText.Render(m.detailErr.Error())
	}

	viewBadge := styleMutedText.Render("view:list")
	if m.tableViewDiagram {
		viewBadge = styleBadgeRun.Render("view:diagram")
	}
	fieldsBadge := styleMutedText.Render("fields:off")
	if m.tableShowFields {
		fieldsBadge = styleBadgeRun.Render("fields:on")
	}

	if m.selectedTable != "" {
		var b strings.Builder
		status := fmt.Sprintf("%s  %s  •  Esc: back  •  v: view  •  f: fields  •  ↑/↓: scroll", viewBadge, fieldsBadge)
		b.WriteString(styleMutedText.Render(status) + "\n")
		if m.tableDetail == nil {
			b.WriteString(styleMutedText.Render("Loading table details..."))
		} else {
			b.WriteString(m.tableDetailVP.View())
		}
		return b.String()
	}
	if len(m.detailTables) == 0 {
		return styleMutedText.Render("(no tables found)")
	}
	var b strings.Builder
	if m.tableViewDiagram {
		zoomLabel := "100%"
		switch m.erdZoom {
		case 0:
			zoomLabel = "50%"
		case 2:
			zoomLabel = "150%"
		}
		zoomBadge := styleBadgeRun.Render("zoom:" + zoomLabel)
		status := fmt.Sprintf("%s  %s  %s  •  +/-: zoom  •  Shift+Arrows: pan  •  v: view  •  f: fields", viewBadge, fieldsBadge, zoomBadge)
		b.WriteString(styleMutedText.Render(status) + "\n")
		if m.erdLayout != nil && m.erdLayout.canvas != nil {
			w := max(24, m.tableOverviewVP.Width)
			h := max(4, m.tableOverviewVP.Height)
			selTbl := ""
			if m.tableCursor >= 0 && m.tableCursor < len(m.detailTables) {
				selTbl = m.detailTables[m.tableCursor]
			}
			fkLine := formatTableRelationsSummary(selTbl, m.tableSchema, w)
			canvasH := h
			if fkLine != "" && h > 5 {
				canvasH = h - 1
			}
			b.WriteString(m.erdLayout.canvas.renderWindow(m.erdXOffset, m.erdYOffset, w, canvasH))
			if fkLine != "" && h > 5 {
				b.WriteString("\n" + fkLine)
			}
		}
	} else {
		status := fmt.Sprintf("%s  %s  •  Enter: inspect  •  Q: query  •  /: filter  •  v: view  •  f: fields", viewBadge, fieldsBadge)
		b.WriteString(styleMutedText.Render(status) + "\n")
		if m.tableFiltering {
			b.WriteString("Filter: " + m.tableFilterInput.View() + "\n")
		} else if q := strings.TrimSpace(m.tableFilterInput.Value()); q != "" {
			b.WriteString(styleMutedText.Render("Filter: "+q+" (Esc to clear)") + "\n")
		}
		b.WriteString(m.tableOverviewVP.View())
	}
	return b.String()
}

func (m dashboardModel) renderQueryTab() string {
	var b strings.Builder
	hint := "Press 'i' or Enter to write SQL"
	if m.queryResult != nil && m.queryResult.isSelect {
		hint += "  •  e: export CSV/JSON  •  c: copy JSON"
		if m.queryTotalW > max(24, m.queryVP.Width) {
			hint += "  •  </>: h-scroll"
		}
	}
	if m.queryInput.Focused() {
		hint = "Ctrl+R / F5: execute SQL  •  Enter: newline  •  Ctrl+P/N (or ↑/↓): history  •  Esc: blur"
	}
	b.WriteString(m.queryInput.View() + "\n")
	b.WriteString(styleMutedText.Render(hint) + "\n\n")
	if m.queryResult == nil && m.queryErr == nil {
		b.WriteString(styleMutedText.Render("Results will appear here."))
	} else {
		b.WriteString(m.queryVP.View())
	}
	return b.String()
}

func (m dashboardModel) renderFooter() string {
	if m.confirmMode != "" && m.selected() != nil {
		action := "Remove"
		if m.confirmMode == "reset" {
			action = "Reset (wipe all data)"
		}
		return styleErrorText.Render(fmt.Sprintf("  %s '%s'? Press [y] to confirm, any other key to cancel.", action, m.selected().db.Name))
	}

	var statusLine string
	if m.busy != "" {
		statusLine = lipgloss.NewStyle().Foreground(colorWarning).Render("  " + m.spinner.View() + " " + m.busy)
	} else if m.toast != "" {
		if m.toastIsErr {
			statusLine = styleToastErr.Render("  ✗ " + m.toast)
		} else {
			statusLine = styleToastOK.Render("  ✓ " + m.toast)
		}
	}

	helpText := m.footerHelpText()
	if m.width > 0 && lipgloss.Width(helpText) > m.width {
		helpText = ansi.Truncate(helpText, max(20, m.width-1), "…")
	}
	help := styleHelpBar.Render(helpText)
	if statusLine != "" {
		return statusLine + "\n" + help
	}
	return help
}

func (m dashboardModel) footerHelpText() string {
	if m.focusPane == 0 {
		return "  [Sidebar] ↑/↓: nav  •  →/Enter: pane  •  /: filter  •  s: start/stop  •  n: new  •  C: clone  •  x: shell  •  b/L: backup/restore  •  R/d: reset/del  •  ?: help  •  q: quit"
	}

	switch m.activeTab {
	case 0:
		return "  [Info] c: copy conn  •  p: password  •  ←/Esc: sidebar  •  1-4/Tab: tab  •  r: refresh  •  ?: help  •  q: quit"
	case 1:
		return "  [Logs] ↑/↓: scroll  •  f: follow  •  /: search  •  n/N: match  •  ←/Esc: sidebar  •  1-4/Tab: tab  •  ?: help  •  q: quit"
	case 2:
		if m.selectedTable != "" {
			return "  [Table Detail] ↑/↓: scroll  •  Q: query  •  v: view  •  f: fields  •  Esc: back  •  1-4/Tab: tab  •  ?: help  •  q: quit"
		}
		if m.tableViewDiagram {
			return "  [Tables ERD] ↑/↓: select  •  +/-: zoom  •  Shift+Arrows: pan  •  Enter: inspect  •  Q: query  •  v: view  •  f: fields  •  ?: help  •  q: quit"
		}
		return "  [Tables] ↑/↓: select  •  /: filter  •  Enter: inspect  •  Q: query  •  v: view  •  f: fields  •  ←/Esc: sidebar  •  ?: help  •  q: quit"
	case 3:
		return "  [Query] i/Enter: write SQL  •  Shift+←/→: h-scroll  •  e: export  •  c: copy JSON  •  ↑/↓: scroll  •  ?: help  •  q: quit"
	default:
		return "  ←/Esc: sidebar  •  1-4/Tab: tab  •  ?: help  •  q: quit"
	}
}

func runDashboardTUI() {
	p := tea.NewProgram(
		newDashboardModel(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running TUI dashboard: %v\n", err)
	}
}
