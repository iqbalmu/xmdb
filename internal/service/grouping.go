package service

import (
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

const GlobalGroupLabel = "global"

// GroupLabel renders an empty group as the global label.
func GroupLabel(group string) string {
	if group == "" {
		return GlobalGroupLabel
	}
	return group
}

// FilterByGroup keeps databases belonging to a group; the special value
// "global" matches ungrouped databases. An empty filter keeps everything.
func FilterByGroup(dbs []*state.Database, group string) []*state.Database {
	if group == "" {
		return dbs
	}

	var out []*state.Database
	for _, d := range dbs {
		if group == GlobalGroupLabel {
			if d.Group == "" {
				out = append(out, d)
			}
			continue
		}
		if d.Group == group {
			out = append(out, d)
		}
	}
	return out
}

// ConnectionConfig builds the DBConfig used to connect to a database from host.
func ConnectionConfig(db *state.Database) *providers.DBConfig {
	return &providers.DBConfig{
		Host:         "localhost",
		Port:         db.Port,
		RootPassword: db.RootPassword,
		DatabaseName: db.DatabaseName,
		User:         db.User,
		UserPassword: db.UserPassword,
	}
}
