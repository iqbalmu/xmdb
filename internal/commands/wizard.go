package commands

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/internal/tui"
)

// navResult reports how a menu or action was left.
type navResult int

const (
	navExit navResult = iota // the action ran to completion, or the user chose Exit
	navBack                  // the user pressed Esc / Back: show the parent menu again
)

type wizardAction struct {
	label string
	run   func() navResult
}

// runWizard launches the interactive split-pane TUI dashboard when `xmdb` is
// invoked with no arguments.
func runWizard() {
	tui.RunDashboard(runtimeMode, runShell)
}

func runWizardLegacy() {
	groups := []wizardGroup{
		{
			label: "Databases",
			actions: []wizardAction{
				{label: "Create a new database", run: func() navResult {
					ok, err := runCreate(nil)
					if err != nil {
						color.Red("Error: %v", err)
						return navExit
					}
					if ok {
						return navExit
					}
					return navBack
				}},
				{label: "List all databases", run: wrapRun(runList)},
				{label: "Show status of all databases", run: wrapRun(runStatus)},
				{label: "Start a database", run: pickAndRun("Start which database?", runStart)},
				{label: "Stop a database", run: pickAndRun("Stop which database?", runStop)},
				{label: "Remove a database", run: pickAndRun("Remove which database?", runRemove)},
			},
		},
		{
			label: "Connect & maintenance",
			actions: []wizardAction{
				{label: "Show connection info", run: pickAndRun("Show info for which database?", runConnect)},
				{label: "View database logs", run: pickAndRun("Show logs of which database?", runLogs)},
				{label: "Backup a database", run: pickAndRun("Backup which database?", runBackup)},
			},
		},
	}

	topOptions := make([]string, 0, len(groups)+2)
	for _, g := range groups {
		topOptions = append(topOptions, g.label)
	}
	topOptions = append(topOptions, "Command overview", "Exit")

	for {
		choice, ok := promptSelect("What do you want to do?", topOptions)
		if !ok || choice == "Exit" {
			fmt.Println()
			return
		}

		if choice == "Command overview" {
			showOverview()
			return
		}

		for _, g := range groups {
			if g.label == choice {
				if runGroupMenu(g) == navExit {
					return
				}
				break // Esc/Back: show the top menu again
			}
		}
	}
}

type wizardGroup struct {
	label   string
	actions []wizardAction
}

// runGroupMenu shows one group's actions. Running an action completes the
// wizard (navExit); Esc or Back returns to the parent menu (navBack).
func runGroupMenu(group wizardGroup) navResult {
	options := make([]string, 0, len(group.actions)+1)
	for _, a := range group.actions {
		options = append(options, a.label)
	}
	options = append(options, "Back")

	choice, ok := promptSelect(group.label, options)
	if !ok || choice == "Back" {
		return navBack
	}

	for _, a := range group.actions {
		if a.label == choice {
			return a.run()
		}
	}
	return navBack
}

func wrapRun(fn func() error) func() navResult {
	return func() navResult {
		if err := fn(); err != nil {
			color.Red("Error: %v", err)
		}
		return navExit
	}
}

// pickAndRun asks which database to act on; Esc there goes back to the menu.
func pickAndRun(question string, fn func(string) error) func() navResult {
	return func() navResult {
		name, ok := pickDatabase(question)
		if !ok {
			return navBack
		}
		if err := fn(name); err != nil {
			color.Red("Error: %v", err)
		}
		return navExit
	}
}

// pickDatabase shows a selector over the managed databases and returns the
// chosen name; ok=false means the user cancelled (Esc) or there is nothing
// to pick.
func pickDatabase(question string) (string, bool) {
	stateMgr, err := state.NewManager()
	if err != nil {
		color.Red("Error: %v", err)
		return "", false
	}

	dbs, err := stateMgr.List()
	if err != nil {
		color.Red("Error loading databases: %v", err)
		return "", false
	}
	if len(dbs) == 0 {
		color.Yellow("\nNo databases yet. Pick 'Create a new database' first.")
		return "", false
	}

	options := make([]string, len(dbs))
	for i, d := range dbs {
		options[i] = fmt.Sprintf("%s (%s, %s)", d.Name, d.Provider, groupLabel(d.Group))
	}

	choice, ok := promptSelect(question, options)
	if !ok {
		return "", false
	}

	for i, o := range options {
		if o == choice {
			return dbs[i].Name, true
		}
	}
	return "", false
}
