package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
