package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/project"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/spf13/cobra"
)

var (
	initFromGroup string
	initOutput    string
	initProject   string
	initForce     bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a project xmdb.yaml manifest (from template or an existing group)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit()
	},
}

func init() {
	initCmd.Flags().StringVar(&initFromGroup, "from-group", "", "Export existing managed databases in this group into xmdb.yaml")
	initCmd.Flags().StringVarP(&initOutput, "output", "o", project.DefaultManifestFile, "Output manifest file path")
	initCmd.Flags().StringVarP(&initProject, "project", "p", "", "Project name (default: current directory name)")
	initCmd.Flags().BoolVarP(&initForce, "force", "f", false, "Overwrite existing manifest file")
}

func projectManifestPath(dir string) (string, error) {
	return project.FindManifest(dir)
}

func runInit() error {
	outPath := strings.TrimSpace(initOutput)
	if outPath == "" {
		outPath = project.DefaultManifestFile
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(absOut); err == nil && !initForce {
		return fmt.Errorf("%s already exists (use --force to overwrite)", absOut)
	}

	var m *project.Manifest
	if g := strings.TrimSpace(initFromGroup); g != "" {
		m, err = service.BuildManifestFromGroup(g)
		if err != nil {
			return err
		}
		if p := strings.TrimSpace(initProject); p != "" {
			m.Project = p
		}
	} else {
		projName := strings.TrimSpace(initProject)
		if projName == "" {
			if cwd, err := os.Getwd(); err == nil {
				projName = filepath.Base(cwd)
			}
		}
		m = project.StarterManifest(projName)
	}

	if err := m.Save(absOut); err != nil {
		return err
	}

	color.Green("\nCreated %s for project '%s' (%d database(s))\n", absOut, m.Project, len(m.Databases))
	fmt.Println("Next steps:")
	fmt.Println("  xmdb up --sync-env    # provision/start databases and sync .env")
	fmt.Println("  xmdb run -- <cmd>     # run your app with database env vars injected")
	fmt.Println()
	return nil
}
