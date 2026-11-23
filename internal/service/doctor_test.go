package service

import (
	"testing"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/state"
)

func TestEvaluateDoctorState(t *testing.T) {
	dbs := []*state.Database{
		{
			Name:          "healthy_db",
			ContainerID:   "c1",
			ContainerName: "xmdb_healthy_db",
			VolumeName:    "xmdb_healthy_db_data",
			Port:          5432,
		},
		{
			Name:          "stale_db",
			ContainerID:   "c_missing",
			ContainerName: "xmdb_stale_db",
			VolumeName:    "xmdb_stale_db_data",
			Port:          5433,
		},
		{
			Name:          "stopped_conflict",
			ContainerID:   "c3",
			ContainerName: "xmdb_stopped_conflict",
			Port:          3306,
		},
	}

	containers := []container.ManagedContainer{
		{ID: "c1", Name: "xmdb_healthy_db", State: "running"},
		{ID: "c3", Name: "xmdb_stopped_conflict", State: "exited"},
		{ID: "c_orphan", Name: "xmdb_orphan_db", State: "exited"},
	}

	volumes := []string{
		"xmdb_healthy_db_data",
		"xmdb_orphan_data",
		"unrelated_vol",
	}

	report := EvaluateDoctorState(dbs, containers, volumes, func(port int) bool {
		return port == 3306
	})

	if report.Healthy {
		t.Fatalf("expected unhealthy report")
	}
	if len(report.StaleStates) != 1 || report.StaleStates[0].Name != "stale_db" {
		t.Fatalf("StaleStates = %+v, want [stale_db]", report.StaleStates)
	}
	if len(report.OrphanContainers) != 1 || report.OrphanContainers[0].Name != "xmdb_orphan_db" {
		t.Fatalf("OrphanContainers = %+v, want [xmdb_orphan_db]", report.OrphanContainers)
	}
	if len(report.OrphanVolumes) != 1 || report.OrphanVolumes[0] != "xmdb_orphan_data" {
		t.Fatalf("OrphanVolumes = %+v, want [xmdb_orphan_data]", report.OrphanVolumes)
	}
	if len(report.PortConflicts) != 1 || report.PortConflicts[0].DatabaseName != "stopped_conflict" {
		t.Fatalf("PortConflicts = %+v, want [stopped_conflict]", report.PortConflicts)
	}
}
