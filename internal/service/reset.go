package service

import (
	"context"
	"fmt"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

// ResetOptions configures reset safety behavior.
type ResetOptions struct {
	NoSnapshot bool
}

// ResetDatabase wipes and recreates the named database container and volume,
// keeping the same port and credentials. Takes a safety auto-snapshot first
// when the container is running.
func ResetDatabase(ctx context.Context, runtimeMode, name string, onProgress func(string)) (*state.Database, error) {
	return ResetDatabaseWithOptions(ctx, runtimeMode, name, ResetOptions{}, onProgress)
}

// ResetDatabaseWithOptions wipes and recreates the named database container and volume.
func ResetDatabaseWithOptions(ctx context.Context, runtimeMode, name string, opts ResetOptions, onProgress func(string)) (*state.Database, error) {
	report := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}

	dbState, err := stateMgr.Load(name)
	if err != nil {
		return nil, fmt.Errorf("database '%s' not found", name)
	}

	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return nil, err
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, err
	}

	running, _ := dockerMgr.IsContainerRunning(ctx, dbState.ContainerID, dbState.ContainerName)
	if running {
		if !opts.NoSnapshot {
			report("Taking safety auto-snapshot before reset...")
			if autoMeta, err := CreateAutoSnapshot(ctx, runtimeMode, name, "reset"); err != nil {
				return nil, fmt.Errorf("creating safety snapshot (pass --no-snapshot to skip): %w", err)
			} else if autoMeta != nil {
				report(fmt.Sprintf("Saved safety snapshot '%s'", autoMeta.Name))
			}
		}
		report("Stopping container...")
		_ = dockerMgr.StopContainer(ctx, dbState.ContainerID, dbState.ContainerName)
	}

	report("Removing old container...")
	_ = dockerMgr.RemoveContainer(ctx, dbState.ContainerID, dbState.ContainerName)

	volumePath := ""
	if dbState.VolumeName != "" {
		volumePath = prov.GetVolumePath(dbState.Version)
		report(fmt.Sprintf("Removing data volume '%s'...", dbState.VolumeName))
		if err := dockerMgr.RemoveVolume(ctx, dbState.VolumeName); err != nil {
			report(fmt.Sprintf("Warning: could not remove volume: %v", err))
		}
	}

	dbConfig := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         dbState.Port,
		RootPassword: dbState.RootPassword,
		DatabaseName: dbState.DatabaseName,
		User:         dbState.User,
		UserPassword: dbState.UserPassword,
	}

	containerConfig := &container.ContainerConfig{
		Name:          dbState.ContainerName,
		Image:         prov.ResolveImage(dbState.Version, dbState.Preset, dbState.CustomImage),
		Port:          dbState.Port,
		ContainerPort: prov.GetContainerPort(),
		Env:           prov.GetEnvVars(dbConfig),
		Cmd:           prov.GetCommand(dbConfig),
		VolumeName:    dbState.VolumeName,
		VolumePath:    volumePath,
	}

	report("Creating fresh container...")
	containerID, err := dockerMgr.CreateContainer(ctx, containerConfig)
	if err != nil {
		return nil, fmt.Errorf("creating container: %w", err)
	}

	report("Starting container...")
	if err := dockerMgr.StartContainer(ctx, containerID, dbState.ContainerName); err != nil {
		CleanupContainer(dockerMgr, ctx, containerID, dbState.ContainerName, dbState.VolumeName)
		return nil, fmt.Errorf("starting container: %w", err)
	}

	adminConfig := adminDBConfig(dbState)

	report("Waiting for database to accept connections...")
	if err := WaitForDatabase(ctx, dockerMgr, containerID, dbState.ContainerName, prov, adminConfig, containerStartTimeout); err != nil {
		CleanupContainer(dockerMgr, ctx, containerID, dbState.ContainerName, dbState.VolumeName)
		return nil, err
	}

	if sqlProv, ok := providers.AsSQL(prov); ok {
		postInitProv, hasPostInit := prov.(providers.PostInitProvider)
		needPostInit := hasPostInit && dbState.Preset != ""
		if dbState.User != "" || needPostInit {
			db, err := sqlProv.Connect(adminConfig)
			if err != nil {
				CleanupContainer(dockerMgr, ctx, containerID, dbState.ContainerName, dbState.VolumeName)
				return nil, fmt.Errorf("connecting to database: %w", err)
			}
			if dbState.User != "" {
				report(fmt.Sprintf("Provisioning user '%s'...", dbState.User))
				if err := sqlProv.CreateUser(ctx, db, dbState.User, dbState.UserPassword, dbState.DatabaseName); err != nil {
					db.Close()
					CleanupContainer(dockerMgr, ctx, containerID, dbState.ContainerName, dbState.VolumeName)
					return nil, fmt.Errorf("creating user: %w", err)
				}
			}
			if needPostInit {
				report(fmt.Sprintf("Enabling preset '%s' extension...", dbState.Preset))
				if err := postInitProv.PostInit(ctx, db, dbState.Preset); err != nil {
					db.Close()
					CleanupContainer(dockerMgr, ctx, containerID, dbState.ContainerName, dbState.VolumeName)
					return nil, fmt.Errorf("enabling preset '%s': %w", dbState.Preset, err)
				}
			}
			db.Close()
		}
	}

	dbState.ContainerID = containerID
	if err := stateMgr.Save(dbState); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return dbState, nil
}
