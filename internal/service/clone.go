package service

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

type CloneOptions struct {
	SourceName string
	TargetName string
	Port       int
	Group      string
	GroupSet   bool
	Version    string
	WithVolume bool
}

// CloneDatabase provisions a new database matching source and streams its data into it.
func CloneDatabase(ctx context.Context, runtimeMode string, stateMgr *state.Manager, opts CloneOptions, onProgress func(string)) (*state.Database, *providers.DBConfig, error) {
	report := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	if err := DBNameValidator(stateMgr)(opts.TargetName); err != nil {
		return nil, nil, err
	}

	src, err := stateMgr.Load(opts.SourceName)
	if err != nil {
		return nil, nil, fmt.Errorf("source database '%s' not found", opts.SourceName)
	}

	prov, err := providers.For(src.Provider)
	if err != nil {
		return nil, nil, err
	}

	version := src.Version
	if opts.Version != "" {
		if err := VersionValidator(opts.Version); err != nil {
			return nil, nil, err
		}
		version = opts.Version
	}

	group := src.Group
	if opts.GroupSet {
		if opts.Group != "" {
			if err := GroupValidator(opts.Group); err != nil {
				return nil, nil, err
			}
		}
		group = strings.TrimSpace(opts.Group)
	}

	port := opts.Port
	if port == 0 {
		port = NextFreePort(stateMgr, prov.GetDefaultPort())
	}

	rootPassword, err := utils.GeneratePassword(utils.DefaultPasswordLength)
	if err != nil {
		return nil, nil, fmt.Errorf("generating password: %w", err)
	}

	createUser := src.User != ""
	var userPassword string
	if createUser {
		userPassword, err = utils.GeneratePassword(utils.DefaultPasswordLength)
		if err != nil {
			return nil, nil, fmt.Errorf("generating password: %w", err)
		}
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, nil, err
	}

	wasRunning, err := dockerMgr.IsContainerRunning(ctx, src.ContainerID, src.ContainerName)
	if err != nil {
		return nil, nil, fmt.Errorf("checking source container status: %w", err)
	}

	if !wasRunning {
		report(fmt.Sprintf("Starting source database '%s' temporarily...", src.Name))
		if err := dockerMgr.StartContainer(ctx, src.ContainerID, src.ContainerName); err != nil {
			return nil, nil, fmt.Errorf("starting source database '%s': %w", src.Name, err)
		}
		defer func() {
			report(fmt.Sprintf("Stopping source database '%s'...", src.Name))
			_ = dockerMgr.StopContainer(ctx, src.ContainerID, src.ContainerName)
		}()

		srcAdminCfg := &providers.DBConfig{
			Host:         "127.0.0.1",
			Port:         src.Port,
			RootPassword: src.RootPassword,
			DatabaseName: src.DatabaseName,
		}
		if err := WaitForDatabase(ctx, dockerMgr, src.ContainerID, src.ContainerName, prov, srcAdminCfg, containerStartTimeout); err != nil {
			return nil, nil, fmt.Errorf("waiting for source database '%s': %w", src.Name, err)
		}
	}

	targetOpts := CreateOptions{
		Name:         opts.TargetName,
		Provider:     src.Provider,
		Version:      version,
		Port:         port,
		Group:        group,
		RootPassword: rootPassword,
		CreateUser:   createUser,
		Username:     src.User,
		UserPassword: userPassword,
		WithVolume:   opts.WithVolume,
	}

	targetState, targetCfg, err := ProvisionDatabase(ctx, runtimeMode, stateMgr, targetOpts, onProgress)
	if err != nil {
		return nil, nil, err
	}

	report(fmt.Sprintf("Streaming data from '%s' to '%s'...", src.Name, targetState.Name))
	if err := streamCloneData(ctx, dockerMgr, src, targetState); err != nil {
		report("Clone failed; rolling back target database...")
		RollbackProvisionedDatabase(ctx, runtimeMode, stateMgr, targetState)
		return nil, nil, err
	}

	return targetState, targetCfg, nil
}

func streamCloneData(ctx context.Context, dockerMgr *container.DockerManager, src, target *state.Database) error {
	var dumpCmd, restoreCmd []string
	if src.Provider == utils.ProviderMySQL {
		dumpCmd = []string{
			"mysqldump",
			"-u", "root",
			fmt.Sprintf("-p%s", src.RootPassword),
			"--single-transaction",
			src.DatabaseName,
		}
		restoreCmd = []string{
			"mysql",
			"-u", "root",
			fmt.Sprintf("-p%s", target.RootPassword),
			target.DatabaseName,
		}
	} else {
		dumpCmd = []string{
			"pg_dump",
			"-U", "postgres",
			"--no-owner",
			"--no-acl",
			src.DatabaseName,
		}
		restoreCmd = []string{
			"psql",
			"-U", "postgres",
			"-d", target.DatabaseName,
			"-v", "ON_ERROR_STOP=1",
		}
	}

	pr, pw := io.Pipe()
	dumpErrCh := make(chan error, 1)

	go func() {
		res, err := dockerMgr.Exec(ctx, src.ContainerID, src.ContainerName, dumpCmd, nil, pw)
		if err != nil {
			_ = pw.CloseWithError(fmt.Errorf("dumping source '%s': %w", src.Name, err))
			dumpErrCh <- err
			return
		}
		if res.ExitCode != 0 {
			dumpErr := fmt.Errorf("dumping source '%s' (exit %d):\n%s", src.Name, res.ExitCode, res.Stderr)
			_ = pw.CloseWithError(dumpErr)
			dumpErrCh <- dumpErr
			return
		}
		_ = pw.Close()
		dumpErrCh <- nil
	}()

	res, restoreErr := dockerMgr.Exec(ctx, target.ContainerID, target.ContainerName, restoreCmd, pr, io.Discard)
	if restoreErr != nil {
		_ = pr.CloseWithError(restoreErr)
		<-dumpErrCh
		return fmt.Errorf("restoring into target '%s': %w", target.Name, restoreErr)
	}
	if res.ExitCode != 0 {
		err := fmt.Errorf("restoring into target '%s' (exit %d):\n%s", target.Name, res.ExitCode, res.Stderr)
		_ = pr.CloseWithError(err)
		<-dumpErrCh
		return err
	}
	_ = pr.Close()

	if dumpErr := <-dumpErrCh; dumpErr != nil {
		return dumpErr
	}
	return nil
}
