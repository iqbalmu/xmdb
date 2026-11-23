package commands

import (
	"context"

	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
)

const defaultUsername = service.DefaultUsername

type createOptions = service.CreateOptions

func provisionDatabase(ctx context.Context, stateMgr *state.Manager, opts createOptions, onProgress func(string)) (*state.Database, *providers.DBConfig, error) {
	return service.ProvisionDatabase(ctx, runtimeMode, stateMgr, opts, onProgress)
}

func provisionAndSeedDatabase(ctx context.Context, stateMgr *state.Manager, opts createOptions, onProgress func(string)) (*state.Database, *providers.DBConfig, int, error) {
	return service.ProvisionAndSeedDatabase(ctx, runtimeMode, stateMgr, opts, onProgress)
}

func rollbackProvisionedDatabase(ctx context.Context, stateMgr *state.Manager, dbState *state.Database) {
	service.RollbackProvisionedDatabase(ctx, runtimeMode, stateMgr, dbState)
}

func dbNameValidator(m *state.Manager) func(string) error  { return service.DBNameValidator(m) }
func portValidator(m *state.Manager) func(string) error    { return service.PortValidator(m) }
func versionValidator(s string) error                      { return service.VersionValidator(s) }
func identifierValidator(s string) error                   { return service.IdentifierValidator(s) }
func requiredText(s string) error                          { return service.RequiredText(s) }
func groupValidator(s string) error                        { return service.GroupValidator(s) }
func nextFreePort(m *state.Manager, start int) int         { return service.NextFreePort(m, start) }
func knownGroups(m *state.Manager) []string                { return service.KnownGroups(m) }
func portAvailabilityRows(m *state.Manager, d, s int) []string {
	return service.PortAvailabilityRows(m, d, s)
}
