package commands

import (
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
)

// connectionConfig builds the DBConfig used to connect to a database from host.
func connectionConfig(db *state.Database) *providers.DBConfig {
	return service.ConnectionConfig(db)
}
