package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/service"
)

func (m *dashboardModel) renderTableOverviewToVP() {
	kind := m.selectedEngineKind()
	if len(m.detailTables) == 0 {
		m.erdLayout = nil
		switch kind {
		case providers.EngineKV:
			m.tableOverviewVP.SetContent(styleMutedText.Render("(no keys found)"))
		case providers.EngineDocument:
			m.tableOverviewVP.SetContent(styleMutedText.Render("(no collections found)"))
		default:
			m.tableOverviewVP.SetContent(styleMutedText.Render("(no tables found)"))
		}
		return
	}
	w := max(24, m.tableOverviewVP.Width)
	h := max(4, m.tableOverviewVP.Height)
	focused := m.focusPane == 1

	if kind == providers.EngineKV {
		m.erdLayout = nil
		vis := m.visibleTables()
		if len(vis) == 0 {
			m.tableOverviewVP.SetContent(styleMutedText.Render("(no matching keys)"))
			return
		}
		infoByKey := make(map[string]providers.KVKeyInfo, len(m.kvKeys))
		for _, k := range m.kvKeys {
			infoByKey[k.Key] = k
		}
		var lines []string
		for i, keyName := range vis {
			meta := infoByKey[keyName]
			ttl := "∞"
			if meta.TTL >= 0 {
				ttl = fmt.Sprintf("%ds", meta.TTL)
			}
			row := fmt.Sprintf("  %-28s  %-8s  ttl:%s", keyName, meta.Type, ttl)
			if lipgloss.Width(row) > w {
				row = ansi.Truncate(row, w-1, "…")
			}
			if i == m.tableCursor && focused {
				row = styleSelectedRow.Width(w).Render("▸ " + strings.TrimPrefix(row, "  "))
			}
			lines = append(lines, row)
		}
		m.tableOverviewVP.SetContent(strings.Join(lines, "\n"))
		return
	}

	if kind == providers.EngineDocument {
		m.erdLayout = nil
		vis := m.visibleTables()
		if len(vis) == 0 {
			m.tableOverviewVP.SetContent(styleMutedText.Render("(no matching collections)"))
			return
		}
		countByCol := make(map[string]int64, len(m.docCollections))
		for _, c := range m.docCollections {
			countByCol[c.Name] = c.Count
		}
		var lines []string
		for i, colName := range vis {
			row := fmt.Sprintf("  %-32s  (%d docs)", colName, countByCol[colName])
			if lipgloss.Width(row) > w {
				row = ansi.Truncate(row, w-1, "…")
			}
			if i == m.tableCursor && focused {
				row = styleSelectedRow.Width(w).Render("▸ " + strings.TrimPrefix(row, "  "))
			}
			lines = append(lines, row)
		}
		m.tableOverviewVP.SetContent(strings.Join(lines, "\n"))
		return
	}

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
	if m.tableDetail.kind != "" && m.tableDetail.kind != providers.EngineSQL {
		title := "Key: " + m.tableDetail.tableName
		if m.tableDetail.kind == providers.EngineDocument {
			pageSize := max(1, m.tableDetail.pageSize)
			totalPages := max(1, int((m.tableDetail.totalRows+int64(pageSize)-1)/int64(pageSize)))
			title = fmt.Sprintf("Collection: %s  •  Page %d/%d (%d docs)", m.tableDetail.tableName, m.tableDetail.page+1, totalPages, m.tableDetail.totalRows)
			if m.tableDetail.filter != "" {
				title += "  •  filter: " + m.tableDetail.filter
			}
		}
		content := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(title) + "\n\n" + m.tableDetail.rawText
		m.tableDetailVP.SetContent(content)
		m.tableDetailVP.GotoTop()
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

	pageSize := max(1, m.tableDetail.pageSize)
	totalPages := max(1, int((m.tableDetail.totalRows+int64(pageSize)-1)/int64(pageSize)))
	previewTitle := fmt.Sprintf("Rows — Page %d/%d (%d total)", m.tableDetail.page+1, totalPages, m.tableDetail.totalRows)
	if m.tableDetail.sortCol != "" {
		dir := "ASC"
		if m.tableDetail.sortDesc {
			dir = "DESC"
		}
		previewTitle += fmt.Sprintf("  •  ORDER BY %s %s", m.tableDetail.sortCol, dir)
	}
	if m.tableDetail.filter != "" {
		previewTitle += fmt.Sprintf("  •  WHERE %s", m.tableDetail.filter)
	}
	b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(previewTitle) + "\n")
	if len(m.tableDetail.previewCols) == 0 || len(m.tableDetail.previewRows) == 0 {
		b.WriteString(styleMutedText.Render("(no matching rows)") + "\n")
	} else {
		// Prepend row index column with selection arrow so users see which row Enter inspects.
		dispCols := append([]string{"#"}, m.tableDetail.previewCols...)
		dispRows := make([][]string, len(m.tableDetail.previewRows))
		startRow := m.tableDetail.page*pageSize + 1
		for i, r := range m.tableDetail.previewRows {
			marker := fmt.Sprintf("%d", startRow+i)
			if i == m.explorerRowCursor {
				marker = "▸" + marker
			}
			rowCopy := make([]string, 0, len(r)+1)
			rowCopy = append(rowCopy, marker)
			rowCopy = append(rowCopy, r...)
			dispRows[i] = rowCopy
		}
		b.WriteString(formatCompactTable(dispCols, dispRows, w))
	}
	m.tableDetailVP.SetContent(b.String())
	m.tableDetailVP.GotoTop()
}

func (m *dashboardModel) renderQueryResultToVP() {
	if m.queryErr != nil {
		m.queryVP.SetContent(styleErrorText.Render("Error:\n" + m.queryErr.Error()))
		m.queryVP.GotoTop()
		return
	}
	if m.queryResult == nil {
		m.queryVP.SetContent(styleMutedText.Render("Press 'i' or Enter to write a query/command."))
		return
	}
	dur := m.queryResult.duration.Round(time.Millisecond)
	if m.queryResult.rawOutput != "" {
		header := styleMutedText.Render(fmt.Sprintf("Executed in %s  •  c: copy output", dur))
		m.queryVP.SetContent(header + "\n\n" + m.queryResult.rawOutput)
		m.queryVP.GotoTop()
		return
	}
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
	case modalSnapshots:
		return renderOverlay(base, renderSnapshotsModal(m.snapshotState, m.width), m.width, m.height)
	case modalDoctor:
		return renderOverlay(base, renderDoctorModal(m.doctorState, m.width), m.width, m.height)
	case modalEditDB:
		return renderOverlay(base, renderEditDBModal(m.editDBState, m.width), m.width, m.height)
	case modalRowInspect:
		return renderOverlay(base, renderRowInspectModal(m.rowInspectState, m.width), m.width, m.height)
	case modalSaveQuery:
		return renderOverlay(base, renderSaveQueryModal(m.saveQueryState, m.width), m.width, m.height)
	case modalSnippets:
		return renderOverlay(base, renderSnippetsModal(m.snippetsState, m.width), m.width, m.height)
	case modalPalette:
		return renderOverlay(base, renderPaletteModal(m.paletteState, m.width), m.width, m.height)
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

func (m dashboardModel) tabLabels() []string {
	switch m.selectedEngineKind() {
	case providers.EngineKV:
		return []string{"[1] Info", "[2] Logs", "[3] Keys", "[4] Console"}
	case providers.EngineDocument:
		return []string{"[1] Info", "[2] Logs", "[3] Collections", "[4] Query"}
	default:
		return []string{"[1] Info", "[2] Logs", "[3] Tables", "[4] Query"}
	}
}

func (m dashboardModel) renderRightPane(width, height int) string {
	tabs := m.tabLabels()
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

	provLabel := fmt.Sprintf("%s (%s)", db.Provider, db.Version)
	if db.Preset != "" {
		provLabel += fmt.Sprintf(" [%s]", db.Preset)
	}
	rows := [][2]string{
		{"Name", db.Name},
		{"Provider", provLabel},
		{"Group", service.GroupLabel(db.Group)},
		{"Status", statusStr},
		{"Port", fmt.Sprintf("127.0.0.1:%d", db.Port)},
	}
	if db.CustomImage != "" {
		rows = append(rows, [2]string{"Image", db.CustomImage})
	}
	if m.selectedEngineKind() != providers.EngineKV {
		rows = append(rows, [2]string{"Database", db.DatabaseName})
	}
	rows = append(rows,
		[2]string{"User", db.GetUsername()},
		[2]string{"Password", pass},
		[2]string{"Container", db.ContainerName},
		[2]string{"Volume", db.VolumeName},
		[2]string{"Created", db.CreatedAt.Format("2006-01-02 15:04")},
	)

	var b strings.Builder
	for _, r := range rows {
		b.WriteString(styleLabel.Render(r[0]+":") + " " + r[1] + "\n")
	}
	b.WriteString("\n" + styleLabel.Render("Conn String:") + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(colorSuccess).Render(e.connStr) + "\n")
	if e.running {
		b.WriteString("\n" + renderLiveMetricsCard(m.metrics[db.Name]))
	}
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

	kind := m.selectedEngineKind()
	if kind != providers.EngineSQL {
		var b strings.Builder
		if m.selectedTable != "" {
			status := "Esc: back  •  ↑/↓: scroll  •  Q: query"
			if kind == providers.EngineDocument {
				status = "Esc: back  •  ,/.: page  •  W: JSON filter  •  Q: query"
			}
			b.WriteString(styleMutedText.Render(status) + "\n")
			if m.explorerFiltering {
				b.WriteString("Filter: " + m.explorerFilterInput.View() + "\n")
			}
			if m.tableDetail == nil {
				b.WriteString(styleMutedText.Render("Loading details..."))
			} else {
				b.WriteString(m.tableDetailVP.View())
			}
			return b.String()
		}
		itemLabel := "keys"
		if kind == providers.EngineDocument {
			itemLabel = "collections"
		}
		if len(m.detailTables) == 0 {
			return styleMutedText.Render(fmt.Sprintf("(no %s found)", itemLabel))
		}
		b.WriteString(styleMutedText.Render("Enter: inspect  •  Q: query  •  /: filter  •  r: refresh") + "\n")
		if m.tableFiltering {
			b.WriteString("Filter: " + m.tableFilterInput.View() + "\n")
		} else if q := strings.TrimSpace(m.tableFilterInput.Value()); q != "" {
			b.WriteString(styleMutedText.Render("Filter: "+q+" (Esc to clear)") + "\n")
		}
		b.WriteString(m.tableOverviewVP.View())
		return b.String()
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
		status := fmt.Sprintf("%s  %s  •  Enter: inspect row  •  ,/.: page  •  W: filter  •  o: sort  •  Esc: back", viewBadge, fieldsBadge)
		b.WriteString(styleMutedText.Render(status) + "\n")
		if m.explorerFiltering {
			b.WriteString("WHERE: " + m.explorerFilterInput.View() + "\n")
		}
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
	kind := m.selectedEngineKind()
	hint := "i/Enter: write query  •  Ctrl+S: save  •  Ctrl+O: snippets"
	if kind == providers.EngineSQL {
		hint += "  •  E: EXPLAIN"
	}
	if m.queryResult != nil && m.queryResult.isSelect {
		hint += "  •  e: export  •  c: copy JSON"
		if m.queryTotalW > max(24, m.queryVP.Width) {
			hint += "  •  </>: h-scroll"
		}
	}
	if m.queryInput.Focused() {
		hint = "Ctrl+R / F5: execute  •  Ctrl+S: save snippet  •  Ctrl+O: snippets  •  ↑/↓: history  •  Esc: blur"
		b.WriteString(m.queryInput.View() + "\n")
	} else if kind == providers.EngineSQL && strings.TrimSpace(m.queryInput.Value()) != "" {
		// Show syntax-highlighted preview box when editor is blurred
		b.WriteString(lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorSubtle).Padding(0, 1).Render(highlightSQL(m.queryInput.Value())) + "\n")
	} else {
		b.WriteString(m.queryInput.View() + "\n")
	}
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
		return "  [Sidebar] ↑/↓: nav  •  s: start/stop  •  n: new  •  S: snap  •  E: edit  •  D: doctor  •  C: clone  •  x: shell  •  Ctrl+P: palette  •  ?: help  •  q: quit"
	}

	switch m.activeTab {
	case 0:
		return "  [Info] c: copy conn  •  p: password  •  S: snapshots  •  E: edit  •  D: doctor  •  Ctrl+P: palette  •  1-4/Tab: tab  •  ?: help"
	case 1:
		return "  [Logs] ↑/↓: scroll  •  f: follow  •  /: search  •  n/N: match  •  Ctrl+P: palette  •  1-4/Tab: tab  •  ?: help"
	case 2:
		if m.selectedTable != "" {
			return "  [Table Detail] ↑/↓: row  •  Enter: inspect row  •  ,/.: page  •  W: filter  •  o: sort  •  Q: query  •  Esc: back  •  ?: help"
		}
		if m.tableViewDiagram && m.selectedEngineKind() == providers.EngineSQL {
			return "  [Tables ERD] ↑/↓: select  •  +/-: zoom  •  Shift+Arrows: pan  •  Enter: explore  •  Q: query  •  v: view  •  ?: help"
		}
		return "  [Tables] ↑/↓: select  •  /: filter  •  Enter: explore  •  Q: query  •  Ctrl+P: palette  •  ←/Esc: sidebar  •  ?: help"
	case 3:
		return "  [Query] i/Enter: edit  •  Ctrl+S: save  •  Ctrl+O: snippets  •  E: EXPLAIN  •  e: export  •  c: copy  •  Ctrl+P: palette  •  ?: help"
	default:
		return "  ←/Esc: sidebar  •  1-4/Tab: tab  •  Ctrl+P: palette  •  ?: help  •  q: quit"
	}
}

// RunDashboard launches the interactive TUI dashboard.
func RunDashboard(runtimeMode string, openShell func(string) error) {
	m := newDashboardModel()
	m.runtimeMode = runtimeMode
	m.openShell = openShell
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running TUI dashboard: %v\n", err)
	}
}
