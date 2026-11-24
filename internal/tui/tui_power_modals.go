package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
)

// --- Async messages for Phase 3 modals ---

type msgSnapshotsLoaded struct {
	dbName string
	snaps  []*service.SnapshotMeta
	err    error
}

type msgDoctorLoaded struct {
	report *service.DoctorReport
	err    error
}

// --- Modal states ---

type snapshotModalState struct {
	dbName       string
	snaps        []*service.SnapshotMeta
	cursor       int
	mode         string // "", "create", "clone", "confirm-restore", "confirm-delete"
	input        textinput.Model
	errMsg       string
	loading      bool
	selectedSnap *service.SnapshotMeta
}

func newSnapshotModalState(dbName string) snapshotModalState {
	ti := textinput.New()
	ti.CharLimit = 64
	ti.Width = 44
	return snapshotModalState{
		dbName:  dbName,
		input:   ti,
		loading: true,
	}
}

type doctorModalState struct {
	report  *service.DoctorReport
	errMsg  string
	loading bool
}

type editDBModalState struct {
	origName   string
	origGroup  string
	nameInput  textinput.Model
	groupInput textinput.Model
	focusField int // 0=name, 1=group
	errMsg     string
}

func newEditDBModalState(db *state.Database) editDBModalState {
	ni := textinput.New()
	ni.CharLimit = 50
	ni.Width = 42
	ni.SetValue(db.Name)
	ni.Focus()

	gi := textinput.New()
	gi.CharLimit = 30
	gi.Width = 42
	gi.Placeholder = "global (leave empty to ungroup)"
	gi.SetValue(db.Group)

	return editDBModalState{
		origName:   db.Name,
		origGroup:  db.Group,
		nameInput:  ni,
		groupInput: gi,
		focusField: 0,
	}
}

type rowInspectModalState struct {
	title   string
	jsonStr string
	vp      viewport.Model
}

func newRowInspectModalState(title string, cols, row []string, width, height int) rowInspectModalState {
	w := min(76, max(44, width-12))
	h := min(24, max(8, height-10))
	vp := viewport.New(w-6, h)
	vp.SetContent(formatRowInspectorText(cols, row))
	return rowInspectModalState{
		title:   title,
		jsonStr: formatRowAsJSON(cols, row),
		vp:      vp,
	}
}

type saveQueryModalState struct {
	input  textinput.Model
	errMsg string
}

func newSaveQueryModalState() saveQueryModalState {
	ti := textinput.New()
	ti.Placeholder = "e.g. active-users"
	ti.CharLimit = 64
	ti.Width = 42
	ti.Focus()
	return saveQueryModalState{input: ti}
}

type snippetsModalState struct {
	items  []SavedQuery
	cursor int
}

type paletteAction struct {
	id    string
	label string
	hint  string
}

type paletteModalState struct {
	input    textinput.Model
	all      []paletteAction
	filtered []paletteAction
	cursor   int
}

func newPaletteModalState() paletteModalState {
	ti := textinput.New()
	ti.Placeholder = "Type a command..."
	ti.CharLimit = 64
	ti.Width = 46
	ti.Focus()

	actions := []paletteAction{
		{id: "toggle", label: "Start / Stop selected database", hint: "s"},
		{id: "create", label: "Create new database", hint: "n"},
		{id: "clone", label: "Clone selected database", hint: "C"},
		{id: "snapshots", label: "Manage snapshots (create / rollback / clone)", hint: "S"},
		{id: "doctor", label: "Run Doctor diagnostics & quick-fix", hint: "D"},
		{id: "edit", label: "Edit database (rename / move group)", hint: "E"},
		{id: "shell", label: "Open interactive shell", hint: "x"},
		{id: "backup", label: "Backup database to file", hint: "b"},
		{id: "restore", label: "Restore database from file", hint: "L"},
		{id: "reset", label: "Reset database (wipe data)", hint: "R"},
		{id: "remove", label: "Remove database", hint: "d"},
		{id: "tab_info", label: "Go to Info & Live Metrics tab", hint: "1"},
		{id: "tab_logs", label: "Go to Logs tab", hint: "2"},
		{id: "tab_tables", label: "Go to Tables / Keys / Collections tab", hint: "3"},
		{id: "tab_query", label: "Go to Query / Console tab", hint: "4"},
		{id: "snippets", label: "Open saved queries / snippets", hint: "Ctrl+O"},
		{id: "explain", label: "Run EXPLAIN on active SQL query", hint: "E (Query)"},
		{id: "refresh", label: "Refresh databases & metrics", hint: "r"},
		{id: "help", label: "Show keyboard shortcuts", hint: "?"},
		{id: "quit", label: "Quit xmdb", hint: "q"},
	}

	st := paletteModalState{
		input: ti,
		all:   actions,
	}
	st.applyFilter()
	return st
}

func (p *paletteModalState) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(p.input.Value()))
	p.filtered = nil
	for _, a := range p.all {
		if q == "" || strings.Contains(strings.ToLower(a.label), q) || strings.Contains(strings.ToLower(a.id), q) {
			p.filtered = append(p.filtered, a)
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

// --- Async commands ---

func loadSnapshotsCmd(dbName string) tea.Cmd {
	return func() tea.Msg {
		mgr, err := service.NewSnapshotManager()
		if err != nil {
			return msgSnapshotsLoaded{dbName: dbName, err: err}
		}
		snaps, err := mgr.List(dbName)
		return msgSnapshotsLoaded{dbName: dbName, snaps: snaps, err: err}
	}
}

func createSnapshotCmd(runtimeMode, dbName, snapName string) tea.Cmd {
	return func() tea.Msg {
		meta, err := service.CreateSnapshot(context.Background(), runtimeMode, dbName, snapName, "", false)
		if err != nil {
			return msgActionDone{err: err}
		}
		return msgActionDone{toast: fmt.Sprintf("Snapshot '%s' created (%s)", meta.Name, formatMetricBytes(uint64(max(0, meta.SizeBytes))))}
	}
}

func restoreSnapshotCmd(runtimeMode, dbName, ref string) tea.Cmd {
	return func() tea.Msg {
		meta, err := service.RestoreSnapshot(context.Background(), runtimeMode, dbName, ref, service.SnapshotRestoreOptions{}, nil)
		if err != nil {
			return msgActionDone{err: err}
		}
		return msgActionDone{toast: fmt.Sprintf("Rolled back '%s' to snapshot '%s'", dbName, meta.Name)}
	}
}

func deleteSnapshotCmd(dbName, id string) tea.Cmd {
	return func() tea.Msg {
		mgr, err := service.NewSnapshotManager()
		if err != nil {
			return msgActionDone{err: err}
		}
		meta, err := mgr.Delete(dbName, id)
		if err != nil {
			return msgActionDone{err: err}
		}
		return msgActionDone{toast: fmt.Sprintf("Deleted snapshot '%s'", meta.Name)}
	}
}

func runDoctorCmd(runtimeMode string) tea.Cmd {
	return func() tea.Msg {
		report, err := service.AnalyzeDoctor(context.Background(), runtimeMode)
		return msgDoctorLoaded{report: report, err: err}
	}
}

func fixDoctorCmd(runtimeMode string, report *service.DoctorReport) tea.Cmd {
	return func() tea.Msg {
		res, err := service.FixDoctor(context.Background(), runtimeMode, report)
		if err != nil {
			return msgActionDone{err: err}
		}
		if len(res.Errors) > 0 {
			return msgActionDone{err: fmt.Errorf("doctor fix completed with %d error(s): %s", len(res.Errors), res.Errors[0])}
		}
		return msgActionDone{toast: fmt.Sprintf("Doctor fixed: %d container(s), %d volume(s), %d stale state(s)", res.RemovedContainers, res.RemovedVolumes, res.RemovedStates)}
	}
}

func editDBCmd(runtimeMode, origName, newName, origGroup, newGroup string) tea.Cmd {
	return func() tea.Msg {
		stateMgr, err := state.NewManager()
		if err != nil {
			return msgActionDone{err: err}
		}
		curName := origName
		var changes []string
		if newName != "" && newName != origName {
			if _, err := service.RenameDatabase(context.Background(), runtimeMode, stateMgr, origName, newName); err != nil {
				return msgActionDone{err: err}
			}
			curName = newName
			changes = append(changes, fmt.Sprintf("renamed to '%s'", newName))
		}
		normNewGroup := strings.TrimSpace(newGroup)
		if strings.EqualFold(normNewGroup, service.GlobalGroupLabel) {
			normNewGroup = ""
		}
		if normNewGroup != origGroup {
			if _, err := service.MoveDatabase(stateMgr, curName, normNewGroup); err != nil {
				return msgActionDone{err: err}
			}
			changes = append(changes, fmt.Sprintf("group -> '%s'", service.GroupLabel(normNewGroup)))
		}
		if len(changes) == 0 {
			return msgActionDone{toast: "No changes made"}
		}
		return msgActionDone{toast: fmt.Sprintf("Updated '%s': %s", curName, strings.Join(changes, ", "))}
	}
}

// --- Renderers ---

func renderSnapshotsModal(st snapshotModalState, width int) string {
	w := min(76, max(50, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render(fmt.Sprintf("Snapshots — %s", st.dbName)) + "\n\n")

	if st.mode == "create" {
		b.WriteString("Snapshot name (leave empty for timestamp):\n")
		b.WriteString(st.input.View() + "\n")
		if st.errMsg != "" {
			b.WriteString("\n" + styleErrorText.Render(st.errMsg) + "\n")
		}
		b.WriteString("\n" + styleMutedText.Render("Enter: create snapshot  •  Esc: back"))
		return styleModal.Width(w).Render(b.String())
	}

	if st.mode == "clone" && st.selectedSnap != nil {
		b.WriteString(fmt.Sprintf("Clone snapshot '%s' into new database name:\n", st.selectedSnap.Name))
		b.WriteString(st.input.View() + "\n")
		if st.errMsg != "" {
			b.WriteString("\n" + styleErrorText.Render(st.errMsg) + "\n")
		}
		b.WriteString("\n" + styleMutedText.Render("Enter: clone from snapshot  •  Esc: back"))
		return styleModal.Width(w).Render(b.String())
	}

	if st.mode == "confirm-restore" && st.selectedSnap != nil {
		b.WriteString(styleWarningText.Render(fmt.Sprintf("Roll back '%s' to snapshot '%s'?", st.dbName, st.selectedSnap.Name)) + "\n")
		b.WriteString(styleMutedText.Render("A safety auto-snapshot will be saved first.") + "\n\n")
		b.WriteString(styleMutedText.Render("Press [y] to confirm, any other key to cancel."))
		return styleModal.Width(w).Render(b.String())
	}

	if st.mode == "confirm-delete" && st.selectedSnap != nil {
		b.WriteString(styleErrorText.Render(fmt.Sprintf("Delete snapshot '%s' (%s)?", st.selectedSnap.Name, st.selectedSnap.ID)) + "\n\n")
		b.WriteString(styleMutedText.Render("Press [y] to confirm, any other key to cancel."))
		return styleModal.Width(w).Render(b.String())
	}

	if st.loading {
		b.WriteString(styleMutedText.Render("Loading snapshots...") + "\n")
	} else if len(st.snaps) == 0 {
		b.WriteString(styleMutedText.Render("(no snapshots saved yet)") + "\n")
	} else {
		maxShow := min(len(st.snaps), 10)
		for i := 0; i < maxShow; i++ {
			s := st.snaps[i]
			tag := "manual"
			if s.Auto {
				tag = "auto"
			}
			line := fmt.Sprintf("%-24s  [%-6s]  %-8s  %s",
				s.Name, tag, formatMetricBytes(uint64(max(0, s.SizeBytes))),
				s.CreatedAt.Local().Format("2006-01-02 15:04"))
			if i == st.cursor {
				b.WriteString(styleSelectedRow.Width(w-6).Render("▸ "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
	}
	if st.errMsg != "" {
		b.WriteString("\n" + styleErrorText.Render(st.errMsg) + "\n")
	}
	b.WriteString("\n" + styleMutedText.Render("c: create  •  Enter/r: rollback  •  C: clone from snap  •  d: delete  •  Esc: close"))
	return styleModal.Width(w).Render(b.String())
}

func renderDoctorModal(st doctorModalState, width int) string {
	w := min(74, max(48, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render("xmdb Doctor Diagnostics") + "\n\n")

	if st.loading {
		b.WriteString(styleMutedText.Render("Analyzing runtime, containers, volumes, and ports...") + "\n")
	} else if st.errMsg != "" {
		b.WriteString(styleErrorText.Render("Error: "+st.errMsg) + "\n")
	} else if st.report != nil {
		r := st.report
		rtBadge := styleBadgeRun.Render("● reachable")
		if !r.RuntimeOK {
			rtBadge = styleErrorText.Render("✗ unreachable")
		}
		healthBadge := styleBadgeRun.Render("HEALTHY")
		if !r.Healthy {
			healthBadge = styleWarningText.Render("ISSUES FOUND")
		}
		b.WriteString(fmt.Sprintf("%s %s   %s %s\n", styleLabel.Render("Runtime:"), rtBadge, styleLabel.Render("Overall:"), healthBadge))
		b.WriteString(fmt.Sprintf("%s %d\n\n", styleLabel.Render("Managed DBs:"), r.DatabasesCount))

		if r.RuntimeError != "" {
			b.WriteString(styleErrorText.Render("  "+r.RuntimeError) + "\n\n")
		}
		b.WriteString(fmt.Sprintf("  Stale states:       %d\n", len(r.StaleStates)))
		for _, s := range r.StaleStates {
			b.WriteString(styleMutedText.Render(fmt.Sprintf("    - %s (missing container %s)\n", s.Name, s.ContainerName)))
		}
		b.WriteString(fmt.Sprintf("  Orphan containers:  %d\n", len(r.OrphanContainers)))
		for _, c := range r.OrphanContainers {
			b.WriteString(styleMutedText.Render(fmt.Sprintf("    - %s (%s)\n", c.Name, c.State)))
		}
		b.WriteString(fmt.Sprintf("  Orphan volumes:     %d\n", len(r.OrphanVolumes)))
		for _, v := range r.OrphanVolumes {
			b.WriteString(styleMutedText.Render(fmt.Sprintf("    - %s\n", v)))
		}
		b.WriteString(fmt.Sprintf("  Port conflicts:     %d\n", len(r.PortConflicts)))
		for _, pc := range r.PortConflicts {
			b.WriteString(styleWarningText.Render(fmt.Sprintf("    - port %d (%s): %s\n", pc.Port, pc.DatabaseName, pc.Reason)))
		}
	}

	hint := "r: re-scan  •  Esc: close"
	if st.report != nil && st.report.HasFixableIssues() {
		hint = "F: quick-fix orphans & stale states  •  " + hint
	}
	b.WriteString("\n" + styleMutedText.Render(hint))
	return styleModal.Width(w).Render(b.String())
}

func renderEditDBModal(st editDBModalState, width int) string {
	w := min(62, max(44, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render(fmt.Sprintf("Edit Database '%s'", st.origName)) + "\n\n")
	nameLbl := "Name:"
	grpLbl := "Group:"
	if st.focusField == 0 {
		nameLbl = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("▸ Name:")
	} else {
		grpLbl = lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render("▸ Group:")
	}
	b.WriteString(nameLbl + "\n" + st.nameInput.View() + "\n\n")
	b.WriteString(grpLbl + "\n" + st.groupInput.View() + "\n")
	if st.errMsg != "" {
		b.WriteString("\n" + styleErrorText.Render(st.errMsg) + "\n")
	}
	b.WriteString("\n" + styleMutedText.Render("Tab: switch field  •  Enter: save  •  Esc: cancel"))
	return styleModal.Width(w).Render(b.String())
}

func renderRowInspectModal(st rowInspectModalState, width int) string {
	w := min(78, max(46, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render(st.title) + "\n\n")
	b.WriteString(st.vp.View() + "\n\n")
	b.WriteString(styleMutedText.Render("↑/↓: scroll  •  c: copy JSON  •  Esc: close"))
	return styleModal.Width(w).Render(b.String())
}

func renderSaveQueryModal(st saveQueryModalState, width int) string {
	w := min(60, max(42, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render("Save Query Snippet") + "\n\n")
	b.WriteString("Snippet name:\n")
	b.WriteString(st.input.View() + "\n")
	if st.errMsg != "" {
		b.WriteString("\n" + styleErrorText.Render(st.errMsg) + "\n")
	}
	b.WriteString("\n" + styleMutedText.Render("Enter: save  •  Esc: cancel"))
	return styleModal.Width(w).Render(b.String())
}

func renderSnippetsModal(st snippetsModalState, width int) string {
	w := min(76, max(48, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render("Saved Queries & Snippets") + "\n\n")
	if len(st.items) == 0 {
		b.WriteString(styleMutedText.Render("(no saved snippets yet — press Ctrl+S in Query tab to save one)") + "\n")
	} else {
		maxShow := min(len(st.items), 8)
		for i := 0; i < maxShow; i++ {
			q := st.items[i]
			preview := strings.ReplaceAll(strings.TrimSpace(q.Query), "\n", " ")
			if len(preview) > 40 {
				preview = preview[:39] + "…"
			}
			line := fmt.Sprintf("%-20s  %s", q.Name, styleMutedText.Render(preview))
			if i == st.cursor {
				b.WriteString(styleSelectedRow.Width(w-6).Render("▸ "+q.Name+"  "+preview) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		if st.cursor >= 0 && st.cursor < len(st.items) {
			b.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Preview:") + "\n")
			b.WriteString(highlightSQL(st.items[st.cursor].Query) + "\n")
		}
	}
	b.WriteString("\n" + styleMutedText.Render("↑/↓: select  •  Enter: load into editor  •  d: delete  •  Esc: close"))
	return styleModal.Width(w).Render(b.String())
}

func renderPaletteModal(st paletteModalState, width int) string {
	w := min(68, max(46, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render("Command Palette") + "\n\n")
	b.WriteString(st.input.View() + "\n\n")
	if len(st.filtered) == 0 {
		b.WriteString(styleMutedText.Render("(no matching commands)") + "\n")
	} else {
		maxShow := min(len(st.filtered), 10)
		for i := 0; i < maxShow; i++ {
			a := st.filtered[i]
			hint := styleMutedText.Render("[" + a.hint + "]")
			if i == st.cursor {
				b.WriteString(styleSelectedRow.Width(w-6).Render(fmt.Sprintf("▸ %-44s [%s]", a.label, a.hint)) + "\n")
			} else {
				b.WriteString(fmt.Sprintf("  %-44s %s\n", a.label, hint))
			}
		}
	}
	b.WriteString("\n" + styleMutedText.Render("↑/↓: select  •  Enter: execute  •  Esc: close"))
	return styleModal.Width(w).Render(b.String())
}

// --- Modal Key Handlers ---

func (m dashboardModel) updatePowerModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalSnapshots:
		st := &m.snapshotState
		if st.mode == "confirm-restore" {
			snap := st.selectedSnap
			st.mode = ""
			st.selectedSnap = nil
			if strings.ToLower(msg.String()) == "y" && snap != nil {
				m.modal = modalNone
				m.busy = fmt.Sprintf("Rolling back '%s' to '%s'...", st.dbName, snap.Name)
				return m, tea.Batch(m.spinner.Tick, restoreSnapshotCmd(m.runtimeMode, st.dbName, snap.ID))
			}
			return m, nil
		}
		if st.mode == "confirm-delete" {
			snap := st.selectedSnap
			st.mode = ""
			st.selectedSnap = nil
			if strings.ToLower(msg.String()) == "y" && snap != nil {
				m.modal = modalNone
				m.busy = fmt.Sprintf("Deleting snapshot '%s'...", snap.Name)
				return m, tea.Batch(m.spinner.Tick, deleteSnapshotCmd(st.dbName, snap.ID))
			}
			return m, nil
		}
		if st.mode == "create" || st.mode == "clone" {
			switch msg.String() {
			case "esc":
				st.mode = ""
				st.errMsg = ""
				st.input.Blur()
				return m, nil
			case "enter":
				val := strings.TrimSpace(st.input.Value())
				if st.mode == "create" {
					if val != "" {
						if err := service.ValidateSnapshotName(val); err != nil {
							st.errMsg = err.Error()
							return m, nil
						}
					}
					m.modal = modalNone
					m.busy = fmt.Sprintf("Creating snapshot for '%s'...", st.dbName)
					return m, tea.Batch(m.spinner.Tick, createSnapshotCmd(m.runtimeMode, st.dbName, val))
				}
				// Clone from snapshot
				stateMgr, err := state.NewManager()
				if err != nil {
					st.errMsg = err.Error()
					return m, nil
				}
				if err := service.DBNameValidator(stateMgr)(val); err != nil {
					st.errMsg = err.Error()
					return m, nil
				}
				snap := st.selectedSnap
				m.modal = modalNone
				m.busy = fmt.Sprintf("Cloning '%s' from snapshot '%s'...", val, snap.Name)
				return m, tea.Batch(m.spinner.Tick, cloneDBCmd(m.runtimeMode, service.CloneOptions{
					SourceName:   st.dbName,
					TargetName:   val,
					WithVolume:   true,
					FromSnapshot: snap.ID,
				}))
			}
			var cmd tea.Cmd
			st.input, cmd = st.input.Update(msg)
			st.errMsg = ""
			return m, cmd
		}

		switch msg.String() {
		case "esc", "q":
			m.modal = modalNone
			return m, nil
		case "up", "k":
			if st.cursor > 0 {
				st.cursor--
			}
		case "down", "j":
			if st.cursor < len(st.snaps)-1 {
				st.cursor++
			}
		case "c":
			if sel := m.selected(); sel == nil || !sel.running {
				st.errMsg = "Database must be running to create a snapshot"
				return m, nil
			}
			st.mode = "create"
			st.errMsg = ""
			st.input.SetValue("")
			st.input.Placeholder = "e.g. before-migration"
			st.input.Focus()
			return m, textinput.Blink
		case "enter", "r":
			if len(st.snaps) > 0 && st.cursor < len(st.snaps) {
				if sel := m.selected(); sel == nil || !sel.running {
					st.errMsg = "Database must be running to roll back"
					return m, nil
				}
				st.selectedSnap = st.snaps[st.cursor]
				st.mode = "confirm-restore"
			}
		case "d":
			if len(st.snaps) > 0 && st.cursor < len(st.snaps) {
				st.selectedSnap = st.snaps[st.cursor]
				st.mode = "confirm-delete"
			}
		case "C":
			if len(st.snaps) > 0 && st.cursor < len(st.snaps) {
				st.selectedSnap = st.snaps[st.cursor]
				st.mode = "clone"
				st.errMsg = ""
				st.input.SetValue(st.dbName + "_branch")
				st.input.Focus()
				return m, textinput.Blink
			}
		}
		return m, nil

	case modalDoctor:
		switch msg.String() {
		case "esc", "q":
			m.modal = modalNone
			return m, nil
		case "r":
			m.doctorState.loading = true
			return m, runDoctorCmd(m.runtimeMode)
		case "F":
			if m.doctorState.report != nil && m.doctorState.report.HasFixableIssues() {
				report := m.doctorState.report
				m.modal = modalNone
				m.busy = "Applying doctor fixes..."
				return m, tea.Batch(m.spinner.Tick, fixDoctorCmd(m.runtimeMode, report))
			}
		}
		return m, nil

	case modalEditDB:
		st := &m.editDBState
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, m.setToast("Edit cancelled", false)
		case "tab", "shift+tab":
			if st.focusField == 0 {
				st.focusField = 1
				st.nameInput.Blur()
				st.groupInput.Focus()
			} else {
				st.focusField = 0
				st.groupInput.Blur()
				st.nameInput.Focus()
			}
			return m, textinput.Blink
		case "enter":
			newName := strings.TrimSpace(st.nameInput.Value())
			newGroup := strings.TrimSpace(st.groupInput.Value())
			if newName != st.origName {
				stateMgr, err := state.NewManager()
				if err != nil {
					st.errMsg = err.Error()
					return m, nil
				}
				if err := service.DBNameValidator(stateMgr)(newName); err != nil {
					st.errMsg = err.Error()
					return m, nil
				}
			}
			if newGroup != "" && !strings.EqualFold(newGroup, service.GlobalGroupLabel) {
				if err := service.GroupValidator(newGroup); err != nil {
					st.errMsg = err.Error()
					return m, nil
				}
			}
			m.modal = modalNone
			m.busy = fmt.Sprintf("Updating '%s'...", st.origName)
			return m, tea.Batch(m.spinner.Tick, editDBCmd(m.runtimeMode, st.origName, newName, st.origGroup, newGroup))
		}
		var cmd tea.Cmd
		if st.focusField == 0 {
			st.nameInput, cmd = st.nameInput.Update(msg)
		} else {
			st.groupInput, cmd = st.groupInput.Update(msg)
		}
		st.errMsg = ""
		return m, cmd

	case modalRowInspect:
		switch msg.String() {
		case "esc", "q", "enter":
			m.modal = modalNone
			return m, nil
		case "up", "k":
			m.rowInspectState.vp.LineUp(1)
		case "down", "j":
			m.rowInspectState.vp.LineDown(1)
		case "pgup":
			m.rowInspectState.vp.LineUp(8)
		case "pgdown":
			m.rowInspectState.vp.LineDown(8)
		case "c":
			if err := clipboard.WriteAll(m.rowInspectState.jsonStr); err != nil {
				return m, m.setToast(fmt.Sprintf("Clipboard error: %v", err), true)
			}
			m.modal = modalNone
			return m, m.setToast("Copied row JSON to clipboard", false)
		}
		return m, nil

	case modalSaveQuery:
		st := &m.saveQueryState
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, nil
		case "enter":
			name := strings.TrimSpace(st.input.Value())
			if name == "" {
				st.errMsg = "Snippet name is required"
				return m, nil
			}
			q := strings.TrimSpace(m.queryInput.Value())
			if q == "" {
				st.errMsg = "Query editor is empty"
				return m, nil
			}
			provName := ""
			if sel := m.selected(); sel != nil {
				provName = sel.db.Provider
			}
			m.savedQueries = upsertSavedQuery(m.savedQueries, SavedQuery{
				Name:     name,
				Provider: provName,
				Query:    q,
			})
			_ = saveSavedQueries(m.savedQueries)
			m.modal = modalNone
			return m, m.setToast(fmt.Sprintf("Saved query snippet '%s'", name), false)
		}
		var cmd tea.Cmd
		st.input, cmd = st.input.Update(msg)
		st.errMsg = ""
		return m, cmd

	case modalSnippets:
		st := &m.snippetsState
		switch msg.String() {
		case "esc", "q":
			m.modal = modalNone
			return m, nil
		case "up", "k":
			if st.cursor > 0 {
				st.cursor--
			}
		case "down", "j":
			if st.cursor < len(st.items)-1 {
				st.cursor++
			}
		case "enter":
			if len(st.items) > 0 && st.cursor < len(st.items) {
				chosen := st.items[st.cursor]
				m.queryInput.SetValue(chosen.Query)
				m.activeTab = 3
				m.focusPane = 1
				m.modal = modalNone
				return m, m.setToast(fmt.Sprintf("Loaded snippet '%s'", chosen.Name), false)
			}
		case "d":
			if len(st.items) > 0 && st.cursor < len(st.items) {
				victim := st.items[st.cursor]
				for i, sq := range m.savedQueries {
					if sq.Name == victim.Name && sq.Provider == victim.Provider {
						m.savedQueries = deleteSavedQuery(m.savedQueries, i)
						break
					}
				}
				_ = saveSavedQueries(m.savedQueries)
				provName := ""
				if sel := m.selected(); sel != nil {
					provName = sel.db.Provider
				}
				st.items = filterSavedQueries(m.savedQueries, provName, "")
				if st.cursor >= len(st.items) {
					st.cursor = max(0, len(st.items)-1)
				}
				return m, m.setToast(fmt.Sprintf("Deleted snippet '%s'", victim.Name), false)
			}
		}
		return m, nil

	case modalPalette:
		st := &m.paletteState
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, nil
		case "up":
			if st.cursor > 0 {
				st.cursor--
			}
			return m, nil
		case "down":
			if st.cursor < len(st.filtered)-1 {
				st.cursor++
			}
			return m, nil
		case "enter":
			if len(st.filtered) == 0 || st.cursor >= len(st.filtered) {
				m.modal = modalNone
				return m, nil
			}
			action := st.filtered[st.cursor].id
			m.modal = modalNone
			return m.executePaletteAction(action)
		}
		var cmd tea.Cmd
		st.input, cmd = st.input.Update(msg)
		st.applyFilter()
		return m, cmd
	}
	return m, nil
}

func (m dashboardModel) executePaletteAction(action string) (tea.Model, tea.Cmd) {
	sel := m.selected()
	switch action {
	case "toggle":
		if sel != nil && m.busy == "" {
			if sel.running {
				m.busy = fmt.Sprintf("Stopping '%s'...", sel.db.Name)
			} else {
				m.busy = fmt.Sprintf("Starting '%s'...", sel.db.Name)
			}
			return m, tea.Batch(m.spinner.Tick, toggleDBCmd(m.runtimeMode, *sel))
		}
	case "create":
		stateMgr, err := state.NewManager()
		if err != nil {
			return m, m.setToast(err.Error(), true)
		}
		cm := newCreateModel(stateMgr)
		m.createWizard = &cm
		m.modal = modalCreate
		return m, textinput.Blink
	case "clone":
		if sel != nil {
			m.cloneState = newCloneModalState(sel.db.Name)
			m.modal = modalClone
			return m, textinput.Blink
		}
	case "snapshots":
		if sel != nil {
			m.snapshotState = newSnapshotModalState(sel.db.Name)
			m.modal = modalSnapshots
			return m, loadSnapshotsCmd(sel.db.Name)
		}
	case "doctor":
		m.doctorState = doctorModalState{loading: true}
		m.modal = modalDoctor
		return m, runDoctorCmd(m.runtimeMode)
	case "edit":
		if sel != nil {
			m.editDBState = newEditDBModalState(sel.db)
			m.modal = modalEditDB
			return m, textinput.Blink
		}
	case "shell":
		if sel != nil && sel.running {
			return m, tea.Exec(shellExecCmd{name: sel.db.Name, openShell: m.openShell}, func(err error) tea.Msg {
				return loadDatabasesCmd(m.runtimeMode)()
			})
		}
		return m, m.setToast("Select a running database to open shell", true)
	case "backup":
		if sel != nil && sel.running {
			m.backupInput = newBackupInput(sel.db.Name)
			m.modal = modalBackup
			return m, textinput.Blink
		}
		return m, m.setToast("Select a running database to back up", true)
	case "restore":
		if sel != nil && sel.running {
			m.restoreState = newRestoreModalState()
			m.modal = modalRestore
			return m, nil
		}
		return m, m.setToast("Select a running database to restore", true)
	case "reset":
		if sel != nil {
			m.confirmMode = "reset"
		}
	case "remove":
		if sel != nil {
			m.confirmMode = "remove"
		}
	case "tab_info":
		m.focusPane = 1
		m.activeTab = 0
		return m, loadMetricsCmd(m.runtimeMode, sel)
	case "tab_logs":
		m.focusPane = 1
		m.activeTab = 1
		return m, loadDetailCmd(m.runtimeMode, sel, 1)
	case "tab_tables":
		m.focusPane = 1
		m.activeTab = 2
		return m, loadDetailCmd(m.runtimeMode, sel, 2)
	case "tab_query":
		m.focusPane = 1
		m.activeTab = 3
		return m, nil
	case "snippets":
		provName := ""
		if sel != nil {
			provName = sel.db.Provider
		}
		m.snippetsState = snippetsModalState{
			items: filterSavedQueries(m.savedQueries, provName, ""),
		}
		m.modal = modalSnippets
		return m, nil
	case "explain":
		if sel == nil || !sel.running {
			return m, m.setToast("Select a running SQL database for EXPLAIN", true)
		}
		eq, err := buildExplainQuery(sel.db.Provider, m.queryInput.Value())
		if err != nil {
			return m, m.setToast(err.Error(), true)
		}
		m.focusPane = 1
		m.activeTab = 3
		m.busy = "Running EXPLAIN..."
		return m, tea.Batch(m.spinner.Tick, runQuickSQLCmd(m.runtimeMode, sel.db, eq))
	case "refresh":
		m.busy = "Refreshing..."
		return m, tea.Batch(m.spinner.Tick, loadDatabasesCmd(m.runtimeMode))
	case "help":
		m.modal = modalHelp
	case "quit":
		return m, tea.Quit
	}
	return m, nil
}
