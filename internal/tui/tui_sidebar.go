package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/iqbalmu/xmdb/internal/service"
)

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
