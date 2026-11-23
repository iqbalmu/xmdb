package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

const (
	createOptionCustomVersion = "Other (enter tag manually)"
	createOptionGlobalGroup   = "Global (no group)"
	createOptionNewGroup      = "New group..."
)

const (
	stepName = iota
	stepProvider
	stepVersion
	stepPort
	stepGroup
	stepRootPassword
	stepAskUser
	stepUsername
	stepUserPassword
	stepSeed
	stepReview
	stepDone
)

type createModel struct {
	stateMgr *state.Manager

	step int
	done bool
	quit bool

	// Collected values
	dbName       string
	provider     string
	version      string
	port         int
	group        string
	rootPassword string
	createUser   bool
	username     string
	userPassword string
	seedPath     string

	// UI state
	input         textinput.Model
	customMode    bool // true when entering manual version or new group
	options       []string
	cursor        int
	validationErr string
	portHints     []string
	suggestPort   int
	defaultPort   int
	width         int
}

func newCreateModel(stateMgr *state.Manager) createModel {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 128
	ti.Width = 40

	m := createModel{
		stateMgr: stateMgr,
		step:     stepName,
		provider: utils.ProviderMySQL,
		username: service.DefaultUsername,
		input:    ti,
		width:    72,
	}
	m.prepareStep()
	return m
}

func (m *createModel) prepareStep() {
	m.validationErr = ""
	m.customMode = false
	m.cursor = 0
	m.options = nil
	m.portHints = nil
	m.input.EchoMode = textinput.EchoNormal
	m.input.SetValue("")

	switch m.step {
	case stepName:
		m.input.Placeholder = "e.g. appdb"
		m.input.SetValue(m.dbName)
		m.input.Focus()

	case stepProvider:
		m.options = []string{utils.ProviderMySQL, utils.ProviderPostgres}
		for i, o := range m.options {
			if o == m.provider {
				m.cursor = i
			}
		}

	case stepVersion:
		defVer := providers.DefaultMySQLVersion
		if m.provider == utils.ProviderPostgres {
			defVer = providers.DefaultPostgresVersion
		}
		known := providers.KnownVersions(m.provider)
		for _, v := range known {
			if v == defVer {
				m.options = append(m.options, v+" (default)")
			} else {
				m.options = append(m.options, v)
			}
		}
		m.options = append(m.options, createOptionCustomVersion)
		for i, v := range known {
			if v == m.version || (m.version == "" && v == defVer) {
				m.cursor = i
				break
			}
		}

	case stepPort:
		m.defaultPort = providers.DefaultMySQLPort
		if m.provider == utils.ProviderPostgres {
			m.defaultPort = providers.DefaultPostgresPort
		}
		m.suggestPort = service.NextFreePort(m.stateMgr, m.defaultPort)
		m.portHints = service.PortAvailabilityRows(m.stateMgr, m.defaultPort, m.suggestPort)
		defStr := strconv.Itoa(m.suggestPort)
		if m.port != 0 {
			defStr = strconv.Itoa(m.port)
		}
		m.input.Placeholder = defStr
		m.input.SetValue("")
		m.input.Focus()

	case stepGroup:
		m.options = append(m.options, createOptionGlobalGroup)
		existing := service.KnownGroups(m.stateMgr)
		m.options = append(m.options, existing...)
		m.options = append(m.options, createOptionNewGroup)
		if m.group != "" {
			for i, o := range m.options {
				if o == m.group {
					m.cursor = i
					break
				}
			}
		}

	case stepRootPassword:
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '*'
		m.input.Placeholder = "leave empty to auto-generate"
		m.input.Focus()

	case stepAskUser:
		m.options = []string{"Yes (recommended)", "No (root/admin only)"}
		if !m.createUser && m.username == "" {
			m.cursor = 1
		}

	case stepUsername:
		m.input.Placeholder = service.DefaultUsername
		if m.username != "" && m.username != service.DefaultUsername {
			m.input.SetValue(m.username)
		}
		m.input.Focus()

	case stepUserPassword:
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '*'
		m.input.Placeholder = "leave empty to auto-generate"
		m.input.Focus()

	case stepSeed:
		m.input.Placeholder = "optional: ./seed.sql or ./migrations (Enter to skip)"
		m.input.SetValue(m.seedPath)
		m.input.Focus()
	}
}

func (m createModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m createModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quit = true
			return m, tea.Quit

		case "esc":
			if m.customMode {
				m.prepareStep()
				return m, nil
			}
			if m.step == stepName {
				m.quit = true
				return m, tea.Quit
			}
			m.step--
			if (m.step == stepUserPassword || m.step == stepUsername) && !m.createUser {
				m.step = stepAskUser
			}
			m.prepareStep()
			return m, nil

		case "up", "k":
			if m.isSelectStep() && m.cursor > 0 {
				m.cursor--
				return m, nil
			}

		case "down", "j":
			if m.isSelectStep() && m.cursor < len(m.options)-1 {
				m.cursor++
				return m, nil
			}

		case "enter":
			return m.handleEnter()
		}
	}

	if !m.isSelectStep() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m createModel) isSelectStep() bool {
	if m.customMode {
		return false
	}
	switch m.step {
	case stepProvider, stepVersion, stepGroup, stepAskUser:
		return true
	}
	return false
}

func (m createModel) handleEnter() (tea.Model, tea.Cmd) {
	m.validationErr = ""

	switch m.step {
	case stepName:
		val := strings.TrimSpace(m.input.Value())
		if err := service.DBNameValidator(m.stateMgr)(val); err != nil {
			m.validationErr = err.Error()
			return m, nil
		}
		m.dbName = val
		m.step = stepProvider
		m.prepareStep()

	case stepProvider:
		chosen := m.options[m.cursor]
		if chosen != m.provider {
			m.provider = chosen
			m.version = ""
			m.port = 0
		}
		m.step = stepVersion
		m.prepareStep()

	case stepVersion:
		if m.customMode {
			val := strings.TrimSpace(m.input.Value())
			if err := service.VersionValidator(val); err != nil {
				m.validationErr = err.Error()
				return m, nil
			}
			m.version = val
			m.step = stepPort
			m.prepareStep()
			return m, nil
		}
		chosen := m.options[m.cursor]
		if chosen == createOptionCustomVersion {
			m.customMode = true
			m.input.SetValue("")
			m.input.Placeholder = "e.g. 8.0.36 or 16-alpine"
			m.input.Focus()
			return m, textinput.Blink
		}
		m.version = strings.TrimSuffix(chosen, " (default)")
		m.step = stepPort
		m.prepareStep()

	case stepPort:
		val := strings.TrimSpace(m.input.Value())
		if val == "" {
			val = m.input.Placeholder
		}
		if err := service.PortValidator(m.stateMgr)(val); err != nil {
			m.validationErr = err.Error()
			return m, nil
		}
		p, _ := strconv.Atoi(val)
		m.port = p
		m.step = stepGroup
		m.prepareStep()

	case stepGroup:
		if m.customMode {
			val := strings.TrimSpace(m.input.Value())
			if err := service.GroupValidator(val); err != nil {
				m.validationErr = err.Error()
				return m, nil
			}
			m.group = val
			m.step = stepRootPassword
			m.prepareStep()
			return m, nil
		}
		chosen := m.options[m.cursor]
		switch chosen {
		case createOptionGlobalGroup:
			m.group = ""
		case createOptionNewGroup:
			m.customMode = true
			m.input.SetValue("")
			m.input.Placeholder = "e.g. work, personal"
			m.input.Focus()
			return m, textinput.Blink
		default:
			m.group = chosen
		}
		m.step = stepRootPassword
		m.prepareStep()

	case stepRootPassword:
		val := m.input.Value()
		if val == "" {
			gen, err := utils.GeneratePassword(utils.DefaultPasswordLength)
			if err != nil {
				m.validationErr = err.Error()
				return m, nil
			}
			val = gen
		}
		m.rootPassword = val
		m.step = stepAskUser
		m.prepareStep()

	case stepAskUser:
		m.createUser = m.cursor == 0
		if !m.createUser {
			m.username = ""
			m.userPassword = ""
			m.step = stepSeed
			m.prepareStep()
			return m, nil
		}
		if m.username == "" {
			m.username = service.DefaultUsername
		}
		m.step = stepUsername
		m.prepareStep()

	case stepUsername:
		val := strings.TrimSpace(m.input.Value())
		if val == "" {
			val = service.DefaultUsername
		}
		if err := service.IdentifierValidator(val); err != nil {
			m.validationErr = err.Error()
			return m, nil
		}
		m.username = val
		m.step = stepUserPassword
		m.prepareStep()

	case stepUserPassword:
		val := m.input.Value()
		if val == "" {
			gen, err := utils.GeneratePassword(utils.DefaultPasswordLength)
			if err != nil {
				m.validationErr = err.Error()
				return m, nil
			}
			val = gen
		}
		m.userPassword = val
		m.step = stepSeed
		m.prepareStep()

	case stepSeed:
		val := strings.TrimSpace(m.input.Value())
		if val != "" {
			if _, err := service.CollectSeedFiles(val); err != nil {
				m.validationErr = err.Error()
				return m, nil
			}
		}
		m.seedPath = val
		m.step = stepReview
		m.prepareStep()

	case stepReview:
		m.done = true
		return m, tea.Quit
	}

	return m, textinput.Blink
}

func (m createModel) View() string {
	if m.done || m.quit {
		return ""
	}
	boxWidth := 68
	if m.width > 4 && m.width-4 < boxWidth {
		boxWidth = m.width - 4
	}
	return styleModal.Width(boxWidth).Render(m.ViewContent(boxWidth-6)) + "\n"
}

func (m createModel) stepProgress() (int, int) {
	total := 9
	if m.createUser {
		total = 11
	}
	cur := m.step + 1
	if !m.createUser && m.step >= stepSeed {
		cur -= 2
	}
	if cur > total {
		cur = total
	}
	return cur, total
}

func (m createModel) renderSummaryGrid(innerWidth int) string {
	var pairs [][2]string
	if m.dbName != "" && m.step > stepName {
		pairs = append(pairs, [2]string{"Name", m.dbName})
	}
	if m.step > stepProvider {
		pairs = append(pairs, [2]string{"Provider", m.provider})
	}
	if m.version != "" && m.step > stepVersion {
		pairs = append(pairs, [2]string{"Version", m.version})
	}
	if m.port != 0 && m.step > stepPort {
		pairs = append(pairs, [2]string{"Port", fmt.Sprintf("127.0.0.1:%d", m.port)})
	}
	if m.step > stepGroup {
		pairs = append(pairs, [2]string{"Group", service.GroupLabel(m.group)})
	}
	if m.step > stepRootPassword {
		pairs = append(pairs, [2]string{"Root Pass", "••••••••"})
	}
	if m.step > stepAskUser {
		if m.createUser {
			u := m.username
			if u == "" {
				u = service.DefaultUsername
			}
			pairs = append(pairs, [2]string{"Dev User", u})
		} else {
			pairs = append(pairs, [2]string{"Dev User", "(none)"})
		}
	}
	if m.step > stepSeed {
		seedLabel := "(none)"
		if m.seedPath != "" {
			seedLabel = m.seedPath
		}
		pairs = append(pairs, [2]string{"Seed", seedLabel})
	}
	if len(pairs) == 0 {
		return ""
	}

	colW := max(22, innerWidth/2-1)
	keyStyle := lipgloss.NewStyle().Foreground(colorMuted).Width(10)
	valStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	var rows []string
	for i := 0; i < len(pairs); i += 2 {
		left := lipgloss.NewStyle().Width(colW).Render(keyStyle.Render(pairs[i][0]+":") + valStyle.Render(pairs[i][1]))
		right := ""
		if i+1 < len(pairs) {
			right = lipgloss.NewStyle().Width(colW).Render(keyStyle.Render(pairs[i+1][0]+":") + valStyle.Render(pairs[i+1][1]))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, left, right))
	}

	card := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorSubtle).
		Padding(0, 1).
		Width(max(30, innerWidth-2)).
		Render(strings.Join(rows, "\n"))
	return card
}

// ViewContent renders the create wizard body without an outer border so it can
// be embedded cleanly inside a modal container.
func (m createModel) ViewContent(innerWidth int) string {
	if m.done || m.quit {
		return ""
	}
	if innerWidth < 36 {
		innerWidth = 60
	}

	cur, total := m.stepProgress()
	dots := strings.Repeat("●", cur) + strings.Repeat("○", max(0, total-cur))
	stepBadge := styleMutedText.Render(fmt.Sprintf("Step %d/%d  %s", cur, total, dots))
	title := styleModalTitle.Render("Create New Database")
	gap := max(2, innerWidth-lipgloss.Width(title)-lipgloss.Width(stepBadge))

	var b strings.Builder
	b.WriteString(title + strings.Repeat(" ", gap) + stepBadge + "\n\n")

	if grid := m.renderSummaryGrid(innerWidth); grid != "" {
		b.WriteString(grid + "\n\n")
	}

	promptTitle := m.stepPromptTitle()
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(promptTitle) + "\n")

	if m.step == stepReview {
		b.WriteString("\n" + styleBadgeRun.Render("Ready to provision container and initialize database.") + "\n")
	} else {
		if len(m.portHints) > 0 {
			b.WriteString("\n")
			for _, line := range m.portHints {
				b.WriteString(styleMutedText.Render("  "+line) + "\n")
			}
		}

		b.WriteString("\n")
		if m.isSelectStep() {
			for i, opt := range m.options {
				if i == m.cursor {
					b.WriteString(styleSelectedRow.Width(max(20, innerWidth-2)).Render(" ▸ "+opt) + "\n")
				} else {
					b.WriteString("   " + opt + "\n")
				}
			}
		} else {
			b.WriteString(m.input.View() + "\n")
		}
	}

	if m.validationErr != "" {
		b.WriteString("\n" + styleErrorText.Render("✗ "+m.validationErr) + "\n")
	}

	help := "Enter: confirm  •  Esc: back/cancel"
	if m.step == stepReview {
		help = "Enter: create database  •  Esc: back"
	} else if m.isSelectStep() {
		help = "↑/↓: select  •  " + help
	}
	b.WriteString("\n" + styleMutedText.Render(help))

	return b.String()
}

func (m createModel) stepPromptTitle() string {
	switch m.step {
	case stepName:
		return "Database name:"
	case stepProvider:
		return "Select database provider:"
	case stepVersion:
		if m.customMode {
			return fmt.Sprintf("Enter custom %s Docker tag:", m.provider)
		}
		return fmt.Sprintf("Select %s version:", m.provider)
	case stepPort:
		return fmt.Sprintf("Host port (default %d):", m.suggestPort)
	case stepGroup:
		if m.customMode {
			return "Enter new group name:"
		}
		return "Select group (for organizing by project):"
	case stepRootPassword:
		return "Root/admin password:"
	case stepAskUser:
		return "Create a dedicated dev user?"
	case stepUsername:
		return fmt.Sprintf("Dev username (default %s):", service.DefaultUsername)
	case stepUserPassword:
		return fmt.Sprintf("Password for '%s':", m.username)
	case stepSeed:
		return "Seed SQL file or directory (optional):"
	case stepReview:
		return "Review configuration:"
	}
	return ""
}

// RunCreateTUI launches the Bubble Tea interactive create flow.
func RunCreateTUI(stateMgr *state.Manager) (dbName, provider, version string, port int, group, rootPassword string, createUser bool, username, userPassword, seedPath string, ok bool) {
	p := tea.NewProgram(newCreateModel(stateMgr))
	res, err := p.Run()
	if err != nil {
		return "", "", "", 0, "", "", false, "", "", "", false
	}
	m, valid := res.(createModel)
	if !valid || !m.done || m.quit {
		return "", "", "", 0, "", "", false, "", "", "", false
	}
	return m.dbName, m.provider, m.version, m.port, m.group, m.rootPassword, m.createUser, m.username, m.userPassword, m.seedPath, true
}
