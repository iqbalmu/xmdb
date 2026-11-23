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
	createCmd.Flags().StringVarP(&createProvider, "provider", "p", "", "Database provider (mysql, postgres)")
	createCmd.Flags().StringVarP(&createVersion, "version", "v", "", "Database version")
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

		rootPassword, err = utils.GeneratePassword(utils.DefaultPasswordLength)
		if err != nil {
			return true, fmt.Errorf("generating password: %w", err)
		}

		userPassword, err = utils.GeneratePassword(utils.DefaultPasswordLength)
		if err != nil {
			return true, fmt.Errorf("generating password: %w", err)
		}

		username = defaultUsername
		createUser = true

		if version == "" {
			version = providers.DefaultMySQLVersion
			if provider == utils.ProviderPostgres {
				version = providers.DefaultPostgresVersion
			}
		}
	}

	opts := createOptions{
		Name:         dbName,
		Provider:     provider,
		Version:      version,
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
	color.Cyan("  Port: %d", dbState.Port)
	color.Cyan("  Database: %s", dbState.DatabaseName)

	if opts.CreateUser && opts.Username != "" {
		color.Cyan("  User: %s", opts.Username)
		color.Cyan("  Password: %s", opts.UserPassword)
		fmt.Println()

		color.White("Connection String:")
		fmt.Printf("  %s\n", prov.GetConnectionString(dbConfig))
	}

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
	n, p, v, pt, grp, rp, cu, u, up, sp, ok := tui.RunCreateTUI(stateMgr)
	if !ok {
		return false
	}
	*dbName = n
	if createProvider != "" {
		*provider = createProvider
	} else {
		*provider = p
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
