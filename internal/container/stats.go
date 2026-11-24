package container

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
)

// ContainerMetrics holds a one-shot snapshot of container resource usage.
type ContainerMetrics struct {
	CPUPercent float64
	MemUsage   uint64
	MemLimit   uint64
	MemPercent float64
	StartedAt  time.Time
	Uptime     time.Duration
}

// GetContainerStats collects one-shot CPU, memory, and uptime metrics for a running container.
func (d *DockerManager) GetContainerStats(ctx context.Context, containerID, containerName string) (*ContainerMetrics, error) {
	ref := resolveContainerRef(containerID, containerName)

	inspect, err := d.client.ContainerInspect(ctx, ref)
	if err != nil {
		return nil, err
	}

	metrics := &ContainerMetrics{}
	if inspect.State != nil && inspect.State.StartedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); parseErr == nil {
			metrics.StartedAt = t
			if inspect.State.Running {
				metrics.Uptime = time.Since(t)
			}
		}
	}

	resp, err := d.client.ContainerStatsOneShot(ctx, ref)
	if err != nil {
		return metrics, nil
	}
	defer resp.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return metrics, fmt.Errorf("decoding container stats: %w", err)
	}

	// Calculate CPU percentage using the standard Docker CLI formula.
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if onlineCPUs == 0 {
		onlineCPUs = 1
	}
	if systemDelta > 0 && cpuDelta > 0 {
		metrics.CPUPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}

	// Memory usage excluding reclaimable file cache (handles cgroup v1 & v2).
	memUsage := stats.MemoryStats.Usage
	if cache, ok := stats.MemoryStats.Stats["inactive_file"]; ok && cache < memUsage {
		memUsage -= cache
	} else if cache, ok := stats.MemoryStats.Stats["cache"]; ok && cache < memUsage {
		memUsage -= cache
	}
	metrics.MemUsage = memUsage
	metrics.MemLimit = stats.MemoryStats.Limit
	if metrics.MemLimit > 0 {
		metrics.MemPercent = (float64(metrics.MemUsage) / float64(metrics.MemLimit)) * 100.0
	}

	return metrics, nil
}
