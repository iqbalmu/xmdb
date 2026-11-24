package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/project"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

// UpOptions configures `xmdb up`.
type UpOptions struct {
	Reseed  bool
	SyncEnv bool
	EnvFile string
}

// UpResult summarizes what `xmdb up` did for a single manifest entry.
type UpResult struct {
	Key          string
	InstanceName string
	Provider     string
	Port         int
	Action       string // "created", "started", "running"
	SeededFiles  int
	PortReassign string
}

// DownOptions configures `xmdb down`.
type DownOptions struct {
	Remove bool
}

// DownResult summarizes what `xmdb down` did for a single manifest entry.
type DownResult struct {
	Key          string
	InstanceName string
	Action       string // "stopped", "removed", "already-stopped", "missing"
}

// UpProject idempotently provisions and starts every database declared in manifestPath.
func UpProject(ctx context.Context, runtimeMode, manifestPath string, opts UpOptions, onProgress func(string)) ([]UpResult, string, error) {
	report := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	manifest, absManifestPath, err := project.Load(manifestPath)
	if err != nil {
		return nil, "", err
	}
	manifestDir := filepath.Dir(absManifestPath)

	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, "", err
	}
	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, "", err
	}

	keys := manifest.SortedKeys()
	results := make([]UpResult, 0, len(keys))

	for _, key := range keys {
		spec := manifest.Databases[key]
		instName := manifest.ResolveInstanceName(key)
		prov, err := providers.For(spec.Provider)
		if err != nil {
			return results, "", err
		}

		seedPath := strings.TrimSpace(spec.Seed)
		if seedPath != "" && !filepath.IsAbs(seedPath) {
			seedPath = filepath.Join(manifestDir, seedPath)
		}

		if stateMgr.Exists(instName) {
			dbState, err := stateMgr.Load(instName)
			if err != nil {
				return results, "", err
			}
			running, _ := dockerMgr.IsContainerRunning(ctx, dbState.ContainerID, dbState.ContainerName)
			action := "running"
			if !running {
				report(fmt.Sprintf("[%s] Starting existing database '%s'...", key, instName))
				if err := dockerMgr.StartContainer(ctx, dbState.ContainerID, dbState.ContainerName); err != nil {
					return results, "", fmt.Errorf("starting '%s': %w", instName, err)
				}
				if err := WaitForDatabase(ctx, dockerMgr, dbState.ContainerID, dbState.ContainerName, prov, adminDBConfig(dbState), containerStartTimeout); err != nil {
					return results, "", fmt.Errorf("waiting for '%s': %w", instName, err)
				}
				action = "started"
			} else {
				report(fmt.Sprintf("[%s] Database '%s' is already running on port %d", key, instName, dbState.Port))
			}

			seeded := 0
			if opts.Reseed && seedPath != "" {
				report(fmt.Sprintf("[%s] Re-seeding '%s' from %s...", key, instName, spec.Seed))
				n, err := SeedDatabase(ctx, runtimeMode, instName, seedPath, onProgress)
				if err != nil {
					return results, "", fmt.Errorf("seeding '%s': %w", instName, err)
				}
				seeded = n
			}

			results = append(results, UpResult{
				Key:          key,
				InstanceName: instName,
				Provider:     dbState.Provider,
				Port:         dbState.Port,
				Action:       action,
				SeededFiles:  seeded,
			})
			continue
		}

		// Provision new database
		version := strings.TrimSpace(spec.Version)
		if version == "" {
			version = prov.GetDefaultVersion()
		}

		requestedPort := spec.Port
		if requestedPort == 0 {
			requestedPort = prov.GetDefaultPort()
		}
		port := requestedPort
		portNote := ""
		if ManagedPortOwner(stateMgr, port) != "" || PortInUse(port) {
			port = NextFreePort(stateMgr, requestedPort)
			if port != requestedPort {
				portNote = fmt.Sprintf("port %d busy, assigned %d", requestedPort, port)
				report(fmt.Sprintf("[%s] Warning: %s", key, portNote))
			}
		}

		rootPassword, err := utils.GeneratePassword(utils.DefaultPasswordLength)
		if err != nil {
			return results, "", fmt.Errorf("generating root password: %w", err)
		}
		userPassword, err := utils.GeneratePassword(utils.DefaultPasswordLength)
		if err != nil {
			return results, "", fmt.Errorf("generating user password: %w", err)
		}

		createUser := prov.Kind() == providers.EngineSQL
		username := ""
		if createUser {
			username = DefaultUsername
		}

		createOpts := CreateOptions{
			Name:         instName,
			Provider:     prov.Name(),
			Version:      version,
			Preset:       spec.Preset,
			CustomImage:  spec.Image,
			Port:         port,
			Group:        manifest.Project,
			Project:      manifest.Project,
			RootPassword: rootPassword,
			CreateUser:   createUser,
			Username:     username,
			UserPassword: userPassword,
			WithVolume:   spec.VolumeEnabled(),
			SeedPath:     seedPath,
		}

		report(fmt.Sprintf("[%s] Creating %s database '%s' on port %d...", key, prov.Name(), instName, port))
		dbState, _, seeded, err := ProvisionAndSeedDatabase(ctx, runtimeMode, stateMgr, createOpts, func(msg string) {
			report(fmt.Sprintf("[%s] %s", key, msg))
		})
		if err != nil {
			return results, "", fmt.Errorf("provisioning '%s': %w", instName, err)
		}

		results = append(results, UpResult{
			Key:          key,
			InstanceName: instName,
			Provider:     dbState.Provider,
			Port:         dbState.Port,
			Action:       "created",
			SeededFiles:  seeded,
			PortReassign: portNote,
		})
	}

	syncedPath := ""
	if opts.SyncEnv {
		envTarget := strings.TrimSpace(opts.EnvFile)
		if envTarget == "" {
			envTarget = strings.TrimSpace(manifest.EnvFile)
		}
		if envTarget == "" {
			envTarget = ".env"
		}
		if !filepath.IsAbs(envTarget) {
			envTarget = filepath.Join(manifestDir, envTarget)
		}
		vars, err := ResolveProjectEnv(absManifestPath)
		if err != nil {
			return results, "", err
		}
		syncedPath, err = project.SyncEnvFile(envTarget, vars)
		if err != nil {
			return results, "", err
		}
		report(fmt.Sprintf("Synced environment variables to %s", syncedPath))
	}

	return results, syncedPath, nil
}

// DownProject stops (or removes when opts.Remove is true) all databases in manifestPath.
func DownProject(ctx context.Context, runtimeMode, manifestPath string, opts DownOptions, onProgress func(string)) ([]DownResult, error) {
	report := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	manifest, _, err := project.Load(manifestPath)
	if err != nil {
		return nil, err
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}
	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, err
	}

	keys := manifest.SortedKeys()
	results := make([]DownResult, 0, len(keys))

	for _, key := range keys {
		instName := manifest.ResolveInstanceName(key)
		if !stateMgr.Exists(instName) {
			results = append(results, DownResult{
				Key:          key,
				InstanceName: instName,
				Action:       "missing",
			})
			continue
		}

		dbState, err := stateMgr.Load(instName)
		if err != nil {
			return results, err
		}

		running, _ := dockerMgr.IsContainerRunning(ctx, dbState.ContainerID, dbState.ContainerName)
		if opts.Remove {
			if running {
				report(fmt.Sprintf("[%s] Stopping '%s'...", key, instName))
				_ = dockerMgr.StopContainer(ctx, dbState.ContainerID, dbState.ContainerName)
			}
			report(fmt.Sprintf("[%s] Removing '%s'...", key, instName))
			_ = dockerMgr.RemoveContainer(ctx, dbState.ContainerID, dbState.ContainerName)
			if dbState.VolumeName != "" {
				_ = dockerMgr.RemoveVolume(ctx, dbState.VolumeName)
			}
			_ = stateMgr.Delete(instName)
			results = append(results, DownResult{
				Key:          key,
				InstanceName: instName,
				Action:       "removed",
			})
			continue
		}

		if !running {
			results = append(results, DownResult{
				Key:          key,
				InstanceName: instName,
				Action:       "already-stopped",
			})
			continue
		}

		report(fmt.Sprintf("[%s] Stopping '%s'...", key, instName))
		if err := dockerMgr.StopContainer(ctx, dbState.ContainerID, dbState.ContainerName); err != nil {
			return results, fmt.Errorf("stopping '%s': %w", instName, err)
		}
		results = append(results, DownResult{
			Key:          key,
			InstanceName: instName,
			Action:       "stopped",
		})
	}

	return results, nil
}

// ResolveProjectEnv resolves all env vars defined by manifestPath against current DB state.
func ResolveProjectEnv(manifestPath string) (map[string]string, error) {
	manifest, _, err := project.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}

	keys := manifest.SortedKeys()
	multiDB := len(keys) > 1
	merged := make(map[string]string)

	for _, key := range keys {
		spec := manifest.Databases[key]
		instName := manifest.ResolveInstanceName(key)
		dbState, err := stateMgr.Load(instName)
		if err != nil {
			return nil, fmt.Errorf("database '%s' (from manifest key '%s') is not provisioned yet; run `xmdb up` first", instName, key)
		}
		prov, err := providers.For(dbState.Provider)
		if err != nil {
			return nil, err
		}
		for k, v := range project.BuildDatabaseEnv(key, spec, prov, dbState, multiDB) {
			merged[k] = v
		}
	}
	return merged, nil
}

// ResolveDatabaseEnv resolves the default env map for a single managed database.
func ResolveDatabaseEnv(dbName string) (map[string]string, error) {
	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}
	dbState, err := stateMgr.Load(dbName)
	if err != nil {
		return nil, fmt.Errorf("database '%s' not found", dbName)
	}
	prov, err := providers.For(dbState.Provider)
	if err != nil {
		return nil, err
	}
	return prov.GetEnvMap(ConnectionConfig(dbState)), nil
}

// BuildManifestFromGroup creates a Manifest populated from all managed databases in group.
func BuildManifestFromGroup(group string) (*project.Manifest, error) {
	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}
	dbs, err := stateMgr.List()
	if err != nil {
		return nil, err
	}
	filtered := FilterByGroup(dbs, group)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no databases found in group '%s'", group)
	}

	projName := strings.TrimSpace(group)
	if projName == "" || strings.EqualFold(projName, GlobalGroupLabel) {
		projName = "myapp"
	}

	m := &project.Manifest{
		Version:   project.CurrentVersion,
		Project:   projName,
		EnvFile:   ".env",
		Databases: make(map[string]project.DatabaseSpec, len(filtered)),
	}

	for _, db := range filtered {
		key := db.Name
		prefix := projName + "_"
		if strings.HasPrefix(key, prefix) && len(key) > len(prefix) {
			key = strings.TrimPrefix(key, prefix)
		}
		vol := db.VolumeName != ""
		spec := project.DatabaseSpec{
			Provider: db.Provider,
			Version:  db.Version,
			Preset:   db.Preset,
			Image:    db.CustomImage,
			Port:     db.Port,
			Volume:   &vol,
		}
		if db.Name != fmt.Sprintf("%s_%s", projName, key) {
			spec.Name = db.Name
		}
		m.Databases[key] = spec
	}
	return m, nil
}
