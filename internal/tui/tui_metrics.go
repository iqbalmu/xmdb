package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
)

type dbMetrics struct {
	CPUPercent    float64
	MemUsage      uint64
	MemLimit      uint64
	MemPercent    float64
	Uptime        time.Duration
	ActiveConns   int // -1 if unavailable
	SnapshotCount int
	UpdatedAt     time.Time
}

type msgMetricsLoaded struct {
	name    string
	metrics *dbMetrics
}

func loadMetricsCmd(runtimeMode string, entry *dbEntry) tea.Cmd {
	if entry == nil || !entry.running {
		return nil
	}
	db := entry.db
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()

		out := &dbMetrics{
			ActiveConns: -1,
			UpdatedAt:   time.Now(),
		}

		if dockerMgr, err := container.NewDockerManager(runtimeMode); err == nil {
			if stats, err := dockerMgr.GetContainerStats(ctx, db.ContainerID, db.ContainerName); err == nil && stats != nil {
				out.CPUPercent = stats.CPUPercent
				out.MemUsage = stats.MemUsage
				out.MemLimit = stats.MemLimit
				out.MemPercent = stats.MemPercent
				out.Uptime = stats.Uptime
			}
		}

		if snapMgr, err := service.NewSnapshotManager(); err == nil {
			if snaps, err := snapMgr.List(db.Name); err == nil {
				out.SnapshotCount = len(snaps)
			}
		}

		out.ActiveConns = fetchActiveConnections(ctx, db)
		return msgMetricsLoaded{name: db.Name, metrics: out}
	}
}

func fetchActiveConnections(ctx context.Context, dbState *state.Database) int {
	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return -1
	}
	cfg := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
	}

	if sqlProv, ok := providers.AsSQL(prov); ok {
		conn, err := sqlProv.Connect(cfg)
		if err != nil {
			return -1
		}
		defer conn.Close()

		if providers.IsMySQLFamily(dbState.Provider) {
			var name, val string
			if err := conn.QueryRowContext(ctx, "SHOW STATUS LIKE 'Threads_connected'").Scan(&name, &val); err == nil {
				if n, err := strconv.Atoi(val); err == nil {
					return n
				}
			}
			return -1
		}
		var count int
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity").Scan(&count); err == nil {
			return count
		}
		return -1
	}

	if kvProv, ok := providers.AsKV(prov); ok {
		if out, _, err := kvProv.ExecKV(ctx, cfg, "INFO clients"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "connected_clients:") {
					raw := strings.TrimSpace(strings.TrimPrefix(line, "connected_clients:"))
					if n, err := strconv.Atoi(raw); err == nil {
						return n
					}
				}
			}
		}
	}

	return -1
}

func renderMetricBar(percent float64, width int) string {
	if width <= 0 {
		width = 14
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent / 100.0 * float64(width))
	if filled > width {
		filled = width
	}
	barColor := colorSuccess
	if percent >= 80 {
		barColor = colorDanger
	} else if percent >= 50 {
		barColor = colorWarning
	}
	fillStr := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filled))
	emptyStr := styleMutedText.Render(strings.Repeat("░", width-filled))
	return fillStr + emptyStr
}

func formatMetricBytes(b uint64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatUptime(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	d = d.Round(time.Second)
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	mins := d / time.Minute
	d -= mins * time.Minute
	secs := d / time.Second

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh %dm %ds", hours, mins, secs)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func renderLiveMetricsCard(m *dbMetrics) string {
	if m == nil {
		return styleMutedText.Render("Collecting live metrics...")
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Live Metrics") + "\n")
	b.WriteString(fmt.Sprintf("%s %s %.1f%%\n",
		styleLabel.Render("CPU:"),
		renderMetricBar(m.CPUPercent, 14),
		m.CPUPercent,
	))
	memInfo := formatMetricBytes(m.MemUsage)
	if m.MemLimit > 0 {
		memInfo = fmt.Sprintf("%s / %s (%.1f%%)", formatMetricBytes(m.MemUsage), formatMetricBytes(m.MemLimit), m.MemPercent)
	}
	b.WriteString(fmt.Sprintf("%s %s %s\n",
		styleLabel.Render("Memory:"),
		renderMetricBar(m.MemPercent, 14),
		memInfo,
	))
	b.WriteString(fmt.Sprintf("%s %s\n", styleLabel.Render("Uptime:"), formatUptime(m.Uptime)))
	if m.ActiveConns >= 0 {
		b.WriteString(fmt.Sprintf("%s %d\n", styleLabel.Render("Connections:"), m.ActiveConns))
	}
	b.WriteString(fmt.Sprintf("%s %d (press 'S' to manage)\n", styleLabel.Render("Snapshots:"), m.SnapshotCount))
	return b.String()
}
