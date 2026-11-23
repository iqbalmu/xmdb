package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/state"
)

const managedPrefix = "xmdb_"

type StaleStateIssue struct {
	Name          string `json:"name"`
	ContainerName string `json:"container_name"`
	ContainerID   string `json:"container_id"`
}

type OrphanContainerIssue struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type PortConflictIssue struct {
	DatabaseName string `json:"database_name"`
	Port         int    `json:"port"`
	Reason       string `json:"reason"`
}

type DoctorReport struct {
	RuntimeOK        bool                   `json:"runtime_ok"`
	RuntimeError     string                 `json:"runtime_error,omitempty"`
	DatabasesCount   int                    `json:"databases_count"`
	StaleStates      []StaleStateIssue      `json:"stale_states"`
	OrphanContainers []OrphanContainerIssue `json:"orphan_containers"`
	OrphanVolumes    []string               `json:"orphan_volumes"`
	PortConflicts    []PortConflictIssue    `json:"port_conflicts"`
	Healthy          bool                   `json:"healthy"`
}

type DoctorFixResult struct {
	RemovedContainers int      `json:"removed_containers"`
	RemovedVolumes    int      `json:"removed_volumes"`
	RemovedStates     int      `json:"removed_states"`
	Errors            []string `json:"errors,omitempty"`
}

// HasFixableIssues reports whether any orphan container/volume or stale state exists.
func (r *DoctorReport) HasFixableIssues() bool {
	return len(r.StaleStates) > 0 || len(r.OrphanContainers) > 0 || len(r.OrphanVolumes) > 0
}

// EvaluateDoctorState compares saved state against discovered containers, volumes, and host ports.
func EvaluateDoctorState(
	dbs []*state.Database,
	containers []container.ManagedContainer,
	volumes []string,
	portInUseFn func(int) bool,
) *DoctorReport {
	report := &DoctorReport{
		RuntimeOK:        true,
		DatabasesCount:   len(dbs),
		StaleStates:      []StaleStateIssue{},
		OrphanContainers: []OrphanContainerIssue{},
		OrphanVolumes:    []string{},
		PortConflicts:    []PortConflictIssue{},
	}

	byID := make(map[string]container.ManagedContainer, len(containers))
	byName := make(map[string]container.ManagedContainer, len(containers))
	for _, c := range containers {
		if c.ID != "" {
			byID[c.ID] = c
		}
		if c.Name != "" {
			byName[c.Name] = c
		}
	}

	referencedContainerIDs := make(map[string]bool)
	referencedContainerNames := make(map[string]bool)
	referencedVolumes := make(map[string]bool)
	portOwners := make(map[int][]string)

	for _, db := range dbs {
		if db.ContainerID != "" {
			referencedContainerIDs[db.ContainerID] = true
		}
		if db.ContainerName != "" {
			referencedContainerNames[db.ContainerName] = true
		}
		if db.VolumeName != "" {
			referencedVolumes[db.VolumeName] = true
		}
		if db.Port > 0 {
			portOwners[db.Port] = append(portOwners[db.Port], db.Name)
		}

		c, ok := byID[db.ContainerID]
		if !ok && db.ContainerName != "" {
			c, ok = byName[db.ContainerName]
		}
		if !ok {
			report.StaleStates = append(report.StaleStates, StaleStateIssue{
				Name:          db.Name,
				ContainerName: db.ContainerName,
				ContainerID:   db.ContainerID,
			})
			continue
		}

		// If the managed container is stopped, verify another process isn't holding its port.
		if c.State != "running" && db.Port > 0 && portInUseFn != nil && portInUseFn(db.Port) {
			report.PortConflicts = append(report.PortConflicts, PortConflictIssue{
				DatabaseName: db.Name,
				Port:         db.Port,
				Reason:       "port is in use by another process while database is stopped",
			})
		}
	}

	for port, owners := range portOwners {
		if len(owners) > 1 {
			sort.Strings(owners)
			report.PortConflicts = append(report.PortConflicts, PortConflictIssue{
				DatabaseName: strings.Join(owners, ", "),
				Port:         port,
				Reason:       "duplicate port assigned across multiple managed databases",
			})
		}
	}

	for _, c := range containers {
		if !referencedContainerIDs[c.ID] && !referencedContainerNames[c.Name] {
			report.OrphanContainers = append(report.OrphanContainers, OrphanContainerIssue{
				ID:    c.ID,
				Name:  c.Name,
				State: c.State,
			})
		}
	}

	for _, v := range volumes {
		if strings.HasPrefix(v, managedPrefix) && strings.HasSuffix(v, "_data") && !referencedVolumes[v] {
			report.OrphanVolumes = append(report.OrphanVolumes, v)
		}
	}

	sort.Slice(report.StaleStates, func(i, j int) bool { return report.StaleStates[i].Name < report.StaleStates[j].Name })
	sort.Slice(report.OrphanContainers, func(i, j int) bool { return report.OrphanContainers[i].Name < report.OrphanContainers[j].Name })
	sort.Strings(report.OrphanVolumes)
	sort.Slice(report.PortConflicts, func(i, j int) bool { return report.PortConflicts[i].Port < report.PortConflicts[j].Port })

	report.Healthy = report.RuntimeOK &&
		len(report.StaleStates) == 0 &&
		len(report.OrphanContainers) == 0 &&
		len(report.OrphanVolumes) == 0 &&
		len(report.PortConflicts) == 0

	return report
}

// AnalyzeDoctor inspects the container runtime, saved state, orphan resources, and port conflicts.
func AnalyzeDoctor(ctx context.Context, runtimeMode string) (*DoctorReport, error) {
	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}
	dbs, err := stateMgr.List()
	if err != nil {
		return nil, err
	}

	dockerMgr, dockerErr := container.NewDockerManager(runtimeMode)
	if dockerErr != nil {
		return &DoctorReport{
			RuntimeOK:        false,
			RuntimeError:     dockerErr.Error(),
			DatabasesCount:   len(dbs),
			StaleStates:      []StaleStateIssue{},
			OrphanContainers: []OrphanContainerIssue{},
			OrphanVolumes:    []string{},
			PortConflicts:    []PortConflictIssue{},
			Healthy:          false,
		}, nil
	}

	containers, err := dockerMgr.ListContainersByPrefix(ctx, managedPrefix)
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}
	volumes, err := dockerMgr.ListVolumesByPrefix(ctx, managedPrefix)
	if err != nil {
		return nil, fmt.Errorf("listing volumes: %w", err)
	}

	return EvaluateDoctorState(dbs, containers, volumes, PortInUse), nil
}

// FixDoctor removes orphan containers, orphan volumes, and stale state files found in report.
func FixDoctor(ctx context.Context, runtimeMode string, report *DoctorReport) (*DoctorFixResult, error) {
	res := &DoctorFixResult{}
	if report == nil {
		return res, nil
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}

	if report.RuntimeOK && (len(report.OrphanContainers) > 0 || len(report.OrphanVolumes) > 0) {
		dockerMgr, err := container.NewDockerManager(runtimeMode)
		if err != nil {
			return nil, err
		}
		for _, oc := range report.OrphanContainers {
			if oc.State == "running" {
				_ = dockerMgr.StopContainer(ctx, oc.ID, oc.Name)
			}
			if err := dockerMgr.RemoveContainer(ctx, oc.ID, oc.Name); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("removing container %s: %v", oc.Name, err))
			} else {
				res.RemovedContainers++
			}
		}
		for _, ov := range report.OrphanVolumes {
			if err := dockerMgr.RemoveVolume(ctx, ov); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("removing volume %s: %v", ov, err))
			} else {
				res.RemovedVolumes++
			}
		}
	}

	for _, ss := range report.StaleStates {
		if err := stateMgr.Delete(ss.Name); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("removing stale state %s: %v", ss.Name, err))
		} else {
			res.RemovedStates++
		}
	}

	return res, nil
}
