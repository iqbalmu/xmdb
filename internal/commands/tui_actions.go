package commands

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/service"
	"github.com/iqbalmu/xmdb/internal/state"
)

func loadDatabasesCmd() tea.Cmd {
	return func() tea.Msg {
		stateMgr, err := state.NewManager()
		if err != nil {
			return msgDatabasesLoaded{dockerErr: err}
		}
		dbs, err := stateMgr.List()
		if err != nil {
			return msgDatabasesLoaded{dockerErr: err}
		}

		dockerMgr, dockerErr := container.NewDockerManager(runtimeMode)
		ctx := context.Background()

		entries := make([]dbEntry, 0, len(dbs))
		for _, db := range dbs {
			running := false
			if dockerErr == nil && db.ContainerID != "" {
				if r, err := dockerMgr.IsContainerRunning(ctx, db.ContainerID, db.ContainerName); err == nil {
					running = r
				}
			}
			connStr := ""
			if prov, err := providers.For(db.Provider); err == nil {
				connStr = prov.GetConnectionString(connectionConfig(db))
			}
			entries = append(entries, dbEntry{
				db:      db,
				running: running,
				connStr: connStr,
			})
		}
		return msgDatabasesLoaded{entries: entries, dockerErr: dockerErr}
	}
}

func loadDetailCmd(entry *dbEntry, tab int) tea.Cmd {
	if entry == nil || (tab != 1 && tab != 2) {
		return nil
	}
	db := entry.db
	running := entry.running
	return func() tea.Msg {
		if tab == 1 {
			dockerMgr, err := container.NewDockerManager(runtimeMode)
			if err != nil {
				return msgDetailLoaded{name: db.Name, tab: tab, err: err}
			}
			logs, err := dockerMgr.TailLogs(context.Background(), db.ContainerID, db.ContainerName, 200)
			return msgDetailLoaded{name: db.Name, tab: tab, logs: logs, err: err}
		}
		if tab == 2 {
			if !running {
				return msgDetailLoaded{name: db.Name, tab: tab, err: fmt.Errorf("database is stopped")}
			}
			prov, err := providers.For(db.Provider)
			if err != nil {
				return msgDetailLoaded{name: db.Name, tab: tab, err: err}
			}
			conn, err := prov.Connect(&providers.DBConfig{
				Host:         "127.0.0.1",
				Port:         db.Port,
				RootPassword: db.RootPassword,
				DatabaseName: db.DatabaseName,
			})
			if err != nil {
				return msgDetailLoaded{name: db.Name, tab: tab, err: err}
			}
			defer conn.Close()
			tables, err := prov.ListTables(context.Background(), conn, db.DatabaseName)
			if err != nil {
				return msgDetailLoaded{name: db.Name, tab: tab, tables: tables, err: err}
			}
			schemaGraph := fetchTableSchemaGraph(context.Background(), conn, db.Provider, db.DatabaseName, tables)
			return msgDetailLoaded{name: db.Name, tab: tab, tables: tables, tableSchema: schemaGraph, err: nil}
		}
		return nil
	}
}

func loadTableDetailCmd(db *state.Database, tableName string) tea.Cmd {
	return func() tea.Msg {
		detail, err := fetchTableDetail(context.Background(), db, tableName)
		return msgTableDetailLoaded{dbName: db.Name, detail: detail, err: err}
	}
}

func runQuickSQLCmd(db *state.Database, query string) tea.Cmd {
	return func() tea.Msg {
		res, err := executeQuickSQL(context.Background(), db, query)
		return msgQueryResult{dbName: db.Name, result: res, err: err}
	}
}

func toggleDBCmd(entry dbEntry) tea.Cmd {
	return func() tea.Msg {
		dockerMgr, err := container.NewDockerManager(runtimeMode)
		if err != nil {
			return msgActionDone{err: err}
		}
		ctx := context.Background()
		if entry.running {
			if err := dockerMgr.StopContainer(ctx, entry.db.ContainerID, entry.db.ContainerName); err != nil {
				return msgActionDone{err: fmt.Errorf("stopping %s: %w", entry.db.Name, err)}
			}
			return msgActionDone{toast: fmt.Sprintf("Stopped '%s'", entry.db.Name)}
		}
		if err := dockerMgr.StartContainer(ctx, entry.db.ContainerID, entry.db.ContainerName); err != nil {
			return msgActionDone{err: fmt.Errorf("starting %s: %w", entry.db.Name, err)}
		}
		return msgActionDone{toast: fmt.Sprintf("Started '%s'", entry.db.Name)}
	}
}

func toggleGroupCmd(group string, entries []dbEntry) tea.Cmd {
	return func() tea.Msg {
		dockerMgr, err := container.NewDockerManager(runtimeMode)
		if err != nil {
			return msgActionDone{err: err}
		}
		ctx := context.Background()
		anyRunning := false
		for _, e := range entries {
			if e.running {
				anyRunning = true
				break
			}
		}
		changed := 0
		for _, e := range entries {
			if anyRunning && e.running {
				if err := dockerMgr.StopContainer(ctx, e.db.ContainerID, e.db.ContainerName); err != nil {
					return msgActionDone{err: fmt.Errorf("stopping %s: %w", e.db.Name, err)}
				}
				changed++
			} else if !anyRunning && !e.running {
				if err := dockerMgr.StartContainer(ctx, e.db.ContainerID, e.db.ContainerName); err != nil {
					return msgActionDone{err: fmt.Errorf("starting %s: %w", e.db.Name, err)}
				}
				changed++
			}
		}
		if anyRunning {
			return msgActionDone{toast: fmt.Sprintf("Stopped %d database(s) in '%s'", changed, group)}
		}
		return msgActionDone{toast: fmt.Sprintf("Started %d database(s) in '%s'", changed, group)}
	}
}

func removeDBCmd(entry dbEntry) tea.Cmd {
	return func() tea.Msg {
		stateMgr, err := state.NewManager()
		if err != nil {
			return msgActionDone{err: err}
		}
		dockerMgr, err := container.NewDockerManager(runtimeMode)
		if err != nil {
			return msgActionDone{err: err}
		}
		ctx := context.Background()
		if entry.running {
			_ = dockerMgr.StopContainer(ctx, entry.db.ContainerID, entry.db.ContainerName)
		}
		_ = dockerMgr.RemoveContainer(ctx, entry.db.ContainerID, entry.db.ContainerName)
		if entry.db.VolumeName != "" {
			_ = dockerMgr.RemoveVolume(ctx, entry.db.VolumeName)
		}
		if err := stateMgr.Delete(entry.db.Name); err != nil {
			return msgActionDone{err: err}
		}
		return msgActionDone{toast: fmt.Sprintf("Removed '%s'", entry.db.Name)}
	}
}

func resetDBCmd(entry dbEntry) tea.Cmd {
	return func() tea.Msg {
		if _, err := service.ResetDatabase(context.Background(), runtimeMode, entry.db.Name, nil); err != nil {
			return msgActionDone{err: err}
		}
		return msgActionDone{toast: fmt.Sprintf("Reset '%s' complete", entry.db.Name)}
	}
}

func backupDBCmd(name, outPath string) tea.Cmd {
	return func() tea.Msg {
		absPath, sizeBytes, _, err := backupDatabase(context.Background(), name, outPath)
		if err != nil {
			return msgActionDone{err: err}
		}
		mb := float64(sizeBytes) / 1024 / 1024
		return msgActionDone{toast: fmt.Sprintf("Backup saved: %s (%.2f MB)", absPath, mb)}
	}
}

func restoreDBCmd(name, filePath string) tea.Cmd {
	return func() tea.Msg {
		absPath, _, _, err := restoreDatabase(context.Background(), name, filePath, io.Discard)
		if err != nil {
			return msgActionDone{err: err}
		}
		return msgActionDone{toast: fmt.Sprintf("Restored '%s' from %s", name, absPath)}
	}
}

func createDBCmd(opts createOptions) tea.Cmd {
	return func() tea.Msg {
		stateMgr, err := state.NewManager()
		if err != nil {
			return msgActionDone{err: err}
		}
		_, _, seededCount, err := provisionAndSeedDatabase(context.Background(), stateMgr, opts, nil)
		if err != nil {
			return msgActionDone{err: err}
		}
		if seededCount > 0 {
			return msgActionDone{toast: fmt.Sprintf("Created database '%s' (%d seed file(s))", opts.Name, seededCount)}
		}
		return msgActionDone{toast: fmt.Sprintf("Created database '%s'", opts.Name)}
	}
}

func cloneDBCmd(opts cloneOptions) tea.Cmd {
	return func() tea.Msg {
		stateMgr, err := state.NewManager()
		if err != nil {
			return msgActionDone{err: err}
		}
		target, _, err := cloneDatabase(context.Background(), stateMgr, opts, nil)
		if err != nil {
			return msgActionDone{err: err}
		}
		return msgActionDone{toast: fmt.Sprintf("Cloned '%s' -> '%s' (port %d)", opts.SourceName, target.Name, target.Port)}
	}
}
