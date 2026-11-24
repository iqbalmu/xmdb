package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/internal/tui"
	"github.com/iqbalmu/xmdb/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	createProvider    string
	createVersion     string
	createPreset      string
	createImage       string
	createPort        int
	createGroup       string
	createInteractive bool
	createWithVolume  bool
	createSeedPath    string
	createKeepOnError bool
)

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new database",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := runCreate(args)
		return err
	},
}

func init() {
	createCmd.Flags().StringVarP(&createProvider, "provider", "p", "", "Database provider (postgres, mysql, mariadb, redis, valkey, mongodb)")
	createCmd.Flags().StringVarP(&createVersion, "version", "v", "", "Database version")
	createCmd.Flags().StringVar(&createPreset, "preset", "", "Provider preset (postgres: pgvector, postgis, timescaledb)")
	createCmd.Flags().StringVar(&createImage, "image", "", "Custom Docker image override (e.g. pgvector/pgvector:pg16)")
	createCmd.Flags().IntVar(&createPort, "port", 0, "Host port")
	createCmd.Flags().StringVarP(&createGroup, "group", "g", "", "Group the database belongs to (empty = global)")
	createCmd.Flags().BoolVarP(&createInteractive, "interactive", "i", false, "Interactive mode")
	createCmd.Flags().BoolVar(&createWithVolume, "volume", true, "Create with persistent volume (default: true)")
	createCmd.Flags().StringVar(&createSeedPath, "seed", "", "SQL file or directory of *.sql files to execute after creation")
	createCmd.Flags().BoolVar(&createKeepOnError, "keep-on-error", false, "Keep created container/state if seeding fails")
}

func runCreate(args []string) (bool, error) {
	var dbName string
	var provider string
	var version string
	var port int
	var rootPassword string
	var createUser bool
	var username string
	var userPassword string
	seedPath := strings.TrimSpace(createSeedPath)

	stateMgr, err := state.NewManager()
	if err != nil {
		return true, err
	}

	// Interactive mode: Esc goes back one prompt; Esc at the very first
	// prompt backs out of create entirely (reported to the caller).
	if createInteractive || len(args) == 0 {
		if !promptCreate(stateMgr, &dbName, &provider, &version, &port,
			&rootPassword, &createUser, &username, &userPassword, &seedPath) {
			return false, nil // backed out
		}
	} else {
		dbName = args[0]
		provider = createProvider
		version = createVersion
		port = createPort

		if provider == "" {
			return true, fmt.Errorf("--provider is required")
		}
		prov, err := providers.For(provider)
		if err != nil {
			return true, err
		}
		provider = prov.Name()

		rootPassword, err = utils.GeneratePassword(utils.DefaultPasswordLength)
		if err != nil {
			return true, fmt.Errorf("generating password: %w", err)
		}

		if prov.Kind() == providers.EngineSQL {
			userPassword, err = utils.GeneratePassword(utils.DefaultPasswordLength)
			if err != nil {
				return true, fmt.Errorf("generating password: %w", err)
			}
			username = defaultUsername
			createUser = true
		}

		if version == "" {
			version = prov.GetDefaultVersion()
		}
	}

	opts := createOptions{
		Name:         dbName,
		Provider:     provider,
		Version:      version,
		Preset:       strings.TrimSpace(createPreset),
		CustomImage:  strings.TrimSpace(createImage),
		Port:         port,
		Group:        strings.TrimSpace(createGroup),
		RootPassword: rootPassword,
		CreateUser:   createUser,
		Username:     username,
		UserPassword: userPassword,
		WithVolume:   createWithVolume,
		SeedPath:     seedPath,
		KeepOnError:  createKeepOnError,
	}

	color.Cyan("\nCreating %s database '%s'...\n", provider, dbName)
	dbState, dbConfig, seededCount, err := provisionAndSeedDatabase(context.Background(), stateMgr, opts, func(msg string) {
		color.White(msg)
	})
	if err != nil {
		return true, err
	}

	prov, _ := providers.For(dbState.Provider)
	color.Green("\nDatabase '%s' created successfully!\n", dbName)
	if seededCount > 0 {
		color.Green("Executed %d seed file(s).\n", seededCount)
	}

	fmt.Println("Connection Information:")
	color.Cyan("  Provider: %s", dbState.Provider)
	if dbState.Preset != "" {
		color.Cyan("  Preset:   %s", dbState.Preset)
	}
	color.Cyan("  Port:     %d", dbState.Port)
	if prov.Kind() != providers.EngineKV {
		color.Cyan("  Database: %s", dbState.DatabaseName)
	}
	color.Cyan("  User:     %s", dbState.GetUsername())
	color.Cyan("  Password: %s", dbState.GetPassword())
	fmt.Println()

	color.White("Connection String:")
	fmt.Printf("  %s\n", prov.GetConnectionString(dbConfig))

	fmt.Println()
	return true, nil
}

// Steps of the guided create flow, in prompt order.
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

func promptCreate(
	stateMgr *state.Manager,
	dbName, provider, version *string,
	port *int,
	rootPassword *string,
	createUser *bool,
	username, userPassword, seedPath *string,
) bool {
	n, p, pr, v, pt, grp, rp, cu, u, up, sp, ok := tui.RunCreateTUI(stateMgr)
	if !ok {
		return false
	}
	*dbName = n
	if createProvider != "" {
		*provider = createProvider
	} else {
		*provider = p
	}
	if createPreset == "" {
		createPreset = pr
	}
	if createVersion != "" {
		*version = createVersion
	} else {
		*version = v
	}
	if createPort != 0 {
		*port = createPort
	} else {
		*port = pt
	}
	if createGroup == "" {
		createGroup = grp
	}
	*rootPassword = rp
	*createUser = cu
	*username = u
	*userPassword = up
	if strings.TrimSpace(*seedPath) == "" {
		*seedPath = sp
	}
	return true
}
