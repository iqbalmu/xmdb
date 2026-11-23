package commands

import (
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
)

const globalGroupLabel = service.GlobalGroupLabel

func groupLabel(group string) string {
	return service.GroupLabel(group)
}

func filterByGroup(dbs []*state.Database, group string) []*state.Database {
	return service.FilterByGroup(dbs, group)
}
