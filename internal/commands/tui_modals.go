package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/iqbalmu/xmdb/internal/state"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalHelp
	modalBackup
	modalRestore
	modalCreate
	modalClone
	modalExport
)

type exportModalState struct {
	format string // "csv" or "json"
	input  textinput.Model
	errMsg string
}

func newExportModalState(dbName string) exportModalState {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 48
	ti.SetValue(fmt.Sprintf("export_%s_%s.csv", dbName, time.Now().Format("20060102_150405")))
	ti.Focus()
	return exportModalState{
		format: "csv",
		input:  ti,
	}
}

func (s *exportModalState) toggleFormat() {
	val := strings.TrimSpace(s.input.Value())
	if s.format == "csv" {
		s.format = "json"
		if strings.HasSuffix(strings.ToLower(val), ".csv") {
			s.input.SetValue(val[:len(val)-4] + ".json")
		}
	} else {
		s.format = "csv"
		if strings.HasSuffix(strings.ToLower(val), ".json") {
			s.input.SetValue(val[:len(val)-5] + ".csv")
		}
	}
	s.errMsg = ""
}

type cloneModalState struct {
	input  textinput.Model
	errMsg string
}

func newCloneModalState(sourceName string) cloneModalState {
	ti := textinput.New()
	ti.CharLimit = 50
	ti.Width = 44
	ti.SetValue(sourceName + "_copy")
	ti.Focus()
	return cloneModalState{input: ti}
}

type restoreModalState struct {
	files      []string
	cursor     int
	customMode bool
	input      textinput.Model
	confirming string // selected file path awaiting 'y' confirmation
	errMsg     string
}

func newBackupInput(dbName string) textinput.Model {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 48
	ti.SetValue(fmt.Sprintf("backup_%s_%s.sql", dbName, time.Now().Format("20060102_150405")))
	ti.Focus()
	return ti
}

func newRestoreModalState() restoreModalState {
	ti := textinput.New()
	ti.Placeholder = "/path/to/backup.sql"
	ti.CharLimit = 256
	ti.Width = 48

	files := listSQLFiles(".")
	return restoreModalState{
		files:      files,
		customMode: len(files) == 0,
		input:      ti,
	}
}

func listSQLFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".sql") {
			out = append(out, e.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	return out
}

func renderOverlay(baseView, modalBox string, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalBox,
		lipgloss.WithWhitespaceChars(" "),
	)
}

func renderHelpModal(width int) string {
	w := min(74, max(48, width-8))
	sections := []struct {
		title string
		items [][2]string
	}{
		{
			title: "Navigation & Layout",
			items: [][2]string{
				{"↑/k, ↓/j", "Move selection / scroll"},
				{"←/h, →/l", "Switch focus between Left & Right panes"},
				{"Enter / Space", "Expand/collapse group (or inspect table)"},
				{"1-4, Tab/S-Tab", "Switch Right pane tab (Info/Logs/Tables/Query; also [/])"},
				{"/", "Filter databases (Left) or search logs (Logs)"},
			},
		},
		{
			title: "Database & Group Actions",
			items: [][2]string{
				{"s", "Start/stop selected database (or entire group)"},
				{"n", "Create new database"},
				{"C", "Clone selected database"},
				{"x", "Open interactive SQL shell"},
				{"b / L", "Backup to SQL file / Restore from SQL file"},
				{"c / p", "Copy connection string / Toggle password"},
				{"R / d", "Reset (wipe data) / Remove database"},
			},
		},
		{
			title: "Logs, Tables & Query",
			items: [][2]string{
				{"f", "Toggle log auto-follow (Logs) / table fields (Tables)"},
				{"v", "Toggle table view: list/table vs diagram (Tables tab)"},
				{"/", "Filter tables (Tables list) or search logs (Logs)"},
				{"Q", "Query selected table (Tables -> Query tab)"},
				{"n / N", "Next / previous log search match"},
				{"Esc", "Back to table list / exit search / clear filter"},
				{"i / Enter", "Focus SQL editor (Query tab; Ctrl+R/F5 run, Ctrl+P/N history)"},
				{"Shift+←/→, </>", "Horizontal scroll wide query results (0 to reset)"},
				{"e / c", "Export CSV/JSON or copy JSON result (Query tab)"},
				{"r / ? / q", "Refresh / Toggle help / Quit"},
			},
		},
	}

	var b strings.Builder
	b.WriteString(styleModalTitle.Render("xmdb Keyboard Shortcuts") + "\n\n")
	for _, sec := range sections {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(sec.title) + "\n")
		for _, it := range sec.items {
			k := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Width(16).Render(it[0])
			b.WriteString("  " + k + it[1] + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(styleMutedText.Render("Press ? or Esc to close"))
	return styleModal.Width(w).Render(b.String())
}

func renderBackupModal(dbName string, input textinput.Model, width int) string {
	w := min(64, max(44, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render(fmt.Sprintf("Backup '%s'", dbName)) + "\n\n")
	b.WriteString("Output SQL file path:\n")
	b.WriteString(input.View() + "\n\n")
	b.WriteString(styleMutedText.Render("Enter: start backup  •  Esc: cancel"))
	return styleModal.Width(w).Render(b.String())
}

func renderCloneModal(sourceName string, st cloneModalState, width int) string {
	w := min(64, max(44, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render(fmt.Sprintf("Clone '%s'", sourceName)) + "\n\n")
	b.WriteString("Target database name (inherits provider, version, group; auto-picks free port):\n")
	b.WriteString(st.input.View() + "\n")
	if st.errMsg != "" {
		b.WriteString(styleErrorText.Render(st.errMsg) + "\n")
	}
	b.WriteString("\n" + styleMutedText.Render("Enter: start clone  •  Esc: cancel"))
	return styleModal.Width(w).Render(b.String())
}

func renderExportModal(dbName string, st exportModalState, width int) string {
	w := min(68, max(46, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render(fmt.Sprintf("Export Query Result (%s)", dbName)) + "\n\n")

	csvPill := "[ CSV ]"
	jsonPill := "[ JSON ]"
	if st.format == "csv" {
		csvPill = styleSelectedRow.Render(" ▸ CSV ")
		jsonPill = styleMutedText.Render("   JSON ")
	} else {
		csvPill = styleMutedText.Render("   CSV ")
		jsonPill = styleSelectedRow.Render(" ▸ JSON ")
	}
	b.WriteString("Format: " + csvPill + "  " + jsonPill + "\n\n")
	b.WriteString("Output file path:\n")
	b.WriteString(st.input.View() + "\n")
	if st.errMsg != "" {
		b.WriteString(styleErrorText.Render(st.errMsg) + "\n")
	}
	b.WriteString("\n" + styleMutedText.Render("Tab: switch format  •  Enter: export  •  Esc: cancel"))
	return styleModal.Width(w).Render(b.String())
}

func renderRestoreModal(dbName string, st restoreModalState, width int) string {
	w := min(68, max(46, width-8))
	var b strings.Builder
	b.WriteString(styleModalTitle.Render(fmt.Sprintf("Restore '%s'", dbName)) + "\n\n")

	if st.confirming != "" {
		b.WriteString(styleWarningText.Render("WARNING: This will overwrite data in '"+dbName+"'.") + "\n")
		b.WriteString("File: " + st.confirming + "\n\n")
		b.WriteString(styleErrorText.Render("Press [y] to confirm restore, any other key to cancel."))
		return styleModal.Width(w).Render(b.String())
	}

	if st.customMode {
		b.WriteString("Enter path to .sql backup file:\n")
		b.WriteString(st.input.View() + "\n")
		if st.errMsg != "" {
			b.WriteString(styleErrorText.Render(st.errMsg) + "\n")
		}
		b.WriteString("\n")
		hint := "Enter: select  •  Esc: cancel"
		if len(st.files) > 0 {
			hint = "Enter: select  •  Tab: pick from list  •  Esc: cancel"
		}
		b.WriteString(styleMutedText.Render(hint))
		return styleModal.Width(w).Render(b.String())
	}

	b.WriteString("Select a .sql file in current directory:\n\n")
	maxShow := min(len(st.files), 10)
	start := 0
	if st.cursor >= maxShow {
		start = st.cursor - maxShow + 1
	}
	end := min(len(st.files), start+maxShow)
	for i := start; i < end; i++ {
		prefix := "  "
		line := st.files[i]
		if i == st.cursor {
			prefix = "▸ "
			b.WriteString(styleSelectedRow.Render(prefix+line) + "\n")
		} else {
			b.WriteString(prefix + line + "\n")
		}
	}
	if st.errMsg != "" {
		b.WriteString("\n" + styleErrorText.Render(st.errMsg) + "\n")
	}
	b.WriteString("\n" + styleMutedText.Render("↑/↓: select  •  Enter: restore  •  t: type path  •  Esc: cancel"))
	return styleModal.Width(w).Render(b.String())
}

func (m dashboardModel) updateModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalHelp:
		if msg.String() == "?" || msg.String() == "esc" || msg.String() == "q" || msg.String() == "enter" {
			m.modal = modalNone
		}
		return m, nil

	case modalBackup:
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, m.setToast("Backup cancelled", false)
		case "enter":
			sel := m.selected()
			outPath := strings.TrimSpace(m.backupInput.Value())
			m.modal = modalNone
			if sel == nil || outPath == "" {
				return m, m.setToast("Backup path cannot be empty", true)
			}
			m.busy = fmt.Sprintf("Backing up '%s'...", sel.db.Name)
			return m, tea.Batch(m.spinner.Tick, backupDBCmd(sel.db.Name, outPath))
		}
		var cmd tea.Cmd
		m.backupInput, cmd = m.backupInput.Update(msg)
		return m, cmd

	case modalRestore:
		if m.restoreState.confirming != "" {
			path := m.restoreState.confirming
			m.restoreState.confirming = ""
			if strings.ToLower(msg.String()) == "y" {
				sel := m.selected()
				m.modal = modalNone
				if sel != nil {
					m.busy = fmt.Sprintf("Restoring '%s'...", sel.db.Name)
					return m, tea.Batch(m.spinner.Tick, restoreDBCmd(sel.db.Name, path))
				}
				return m, nil
			}
			return m, nil
		}

		if m.restoreState.customMode {
			switch msg.String() {
			case "esc":
				m.modal = modalNone
				return m, m.setToast("Restore cancelled", false)
			case "tab":
				if len(m.restoreState.files) > 0 {
					m.restoreState.customMode = false
					m.restoreState.input.Blur()
					m.restoreState.errMsg = ""
				}
				return m, nil
			case "enter":
				p := strings.TrimSpace(m.restoreState.input.Value())
				if p == "" {
					m.restoreState.errMsg = "Please enter a file path"
					return m, nil
				}
				if _, err := os.Stat(p); err != nil {
					m.restoreState.errMsg = fmt.Sprintf("File not found: %s", p)
					return m, nil
				}
				m.restoreState.errMsg = ""
				m.restoreState.confirming = p
				return m, nil
			}
			var cmd tea.Cmd
			m.restoreState.input, cmd = m.restoreState.input.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, m.setToast("Restore cancelled", false)
		case "t":
			m.restoreState.customMode = true
			m.restoreState.input.Focus()
			return m, textinput.Blink
		case "up", "k":
			if m.restoreState.cursor > 0 {
				m.restoreState.cursor--
			}
			return m, nil
		case "down", "j":
			if m.restoreState.cursor < len(m.restoreState.files)-1 {
				m.restoreState.cursor++
			}
			return m, nil
		case "enter":
			if len(m.restoreState.files) > 0 {
				m.restoreState.confirming = m.restoreState.files[m.restoreState.cursor]
			}
			return m, nil
		}
		return m, nil

	case modalCreate:
		if m.createWizard == nil {
			m.modal = modalNone
			return m, nil
		}
		next, cmd := m.createWizard.Update(msg)
		cm := next.(createModel)
		m.createWizard = &cm
		if cm.quit {
			m.modal = modalNone
			m.createWizard = nil
			return m, m.setToast("Create cancelled", false)
		}
		if cm.done {
			opts := createOptions{
				Name:         cm.dbName,
				Provider:     cm.provider,
				Version:      cm.version,
				Port:         cm.port,
				Group:        cm.group,
				RootPassword: cm.rootPassword,
				CreateUser:   cm.createUser,
				Username:     cm.username,
				UserPassword: cm.userPassword,
				WithVolume:   true,
				SeedPath:     cm.seedPath,
			}
			m.modal = modalNone
			m.createWizard = nil
			m.busy = fmt.Sprintf("Creating '%s' (%s)...", opts.Name, opts.Provider)
			return m, tea.Batch(m.spinner.Tick, createDBCmd(opts))
		}
		return m, cmd

	case modalClone:
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, m.setToast("Clone cancelled", false)
		case "enter":
			sel := m.selected()
			targetName := strings.TrimSpace(m.cloneState.input.Value())
			if sel == nil {
				m.modal = modalNone
				return m, nil
			}
			stateMgr, err := state.NewManager()
			if err != nil {
				m.cloneState.errMsg = err.Error()
				return m, nil
			}
			if err := dbNameValidator(stateMgr)(targetName); err != nil {
				m.cloneState.errMsg = err.Error()
				return m, nil
			}
			m.modal = modalNone
			m.busy = fmt.Sprintf("Cloning '%s' -> '%s'...", sel.db.Name, targetName)
			return m, tea.Batch(m.spinner.Tick, cloneDBCmd(cloneOptions{
				SourceName: sel.db.Name,
				TargetName: targetName,
				WithVolume: true,
			}))
		}
		var cmd tea.Cmd
		m.cloneState.input, cmd = m.cloneState.input.Update(msg)
		m.cloneState.errMsg = ""
		return m, cmd

	case modalExport:
		switch msg.String() {
		case "esc":
			m.modal = modalNone
			return m, m.setToast("Export cancelled", false)
		case "tab", "left", "right":
			m.exportState.toggleFormat()
			return m, nil
		case "enter":
			outPath := strings.TrimSpace(m.exportState.input.Value())
			absPath, err := exportQueryResult(m.queryResult, m.exportState.format, outPath)
			if err != nil {
				m.exportState.errMsg = err.Error()
				return m, nil
			}
			m.modal = modalNone
			return m, m.setToast(fmt.Sprintf("Exported %s to %s", strings.ToUpper(m.exportState.format), absPath), false)
		}
		var cmd tea.Cmd
		m.exportState.input, cmd = m.exportState.input.Update(msg)
		m.exportState.errMsg = ""
		return m, cmd
	}
	return m, nil
}
