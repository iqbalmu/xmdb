package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

const (
	DefaultUsername       = "dev_user"
	containerStartTimeout = 60 * time.Second
	portProbeTimeout      = 500 * time.Millisecond
	connectAttemptTimeout = 5 * time.Second
)

type CreateOptions struct {
	Name         string
	Provider     string
	Version      string
	Preset       string
	CustomImage  string
	Port         int
	Group        string
	Project      string
	RootPassword string
	CreateUser   bool
	Username     string
	UserPassword string
	WithVolume   bool
	SeedPath     string
	KeepOnError  bool
}

// ProvisionDatabase creates, starts, and initializes a database container without
// writing to stdout; progress updates are delivered via onProgress if non-nil.
func ProvisionDatabase(ctx context.Context, runtimeMode string, stateMgr *state.Manager, opts CreateOptions, onProgress func(string)) (*state.Database, *providers.DBConfig, error) {
	report := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	if stateMgr.Exists(opts.Name) {
		return nil, nil, fmt.Errorf("database '%s' already exists", opts.Name)
	}

	prov, err := providers.For(opts.Provider)
	if err != nil {
		return nil, nil, err
	}

	port := opts.Port
	if port == 0 {
		port = prov.GetDefaultPort()
	}
	if owner := ManagedPortOwner(stateMgr, port); owner != "" {
		return nil, nil, fmt.Errorf("port %d is already used by database '%s'", port, owner)
	}
	if PortInUse(port) {
		return nil, nil, fmt.Errorf("port %d is already in use by another process", port)
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, nil, err
	}

	imageName := prov.ResolveImage(opts.Version, opts.Preset, opts.CustomImage)
	report(fmt.Sprintf("Pulling image %s...", imageName))
	if err := dockerMgr.PullImage(ctx, imageName); err != nil {
		return nil, nil, fmt.Errorf("pulling image: %w", err)
	}

	containerName := fmt.Sprintf("xmdb_%s", opts.Name)
	volumeName := ""
	var volumePath string
	if opts.WithVolume {
		volumeName = fmt.Sprintf("xmdb_%s_data", opts.Name)
		volumePath = prov.GetVolumePath(opts.Version)

		if exists, err := dockerMgr.VolumeExists(ctx, volumeName); err == nil && exists {
			return nil, nil, fmt.Errorf("volume '%s' already holds data from a previous database; remove it with 'docker volume rm %s' or pass --volume=false", volumeName, volumeName)
		}
	}

	dbConfig := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         port,
		RootPassword: opts.RootPassword,
		DatabaseName: opts.Name,
		User:         opts.Username,
		UserPassword: opts.UserPassword,
	}

	containerConfig := &container.ContainerConfig{
		Name:          containerName,
		Image:         imageName,
		Port:          port,
		ContainerPort: prov.GetContainerPort(),
		Env:           prov.GetEnvVars(dbConfig),
		Cmd:           prov.GetCommand(dbConfig),
		VolumeName:    volumeName,
		VolumePath:    volumePath,
	}

	adminConfig := &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         port,
		RootPassword: opts.RootPassword,
		DatabaseName: opts.Name,
	}

	report("Creating container...")
	containerID, err := dockerMgr.CreateContainer(ctx, containerConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("creating container: %w", err)
	}

	report("Starting container...")
	if err := dockerMgr.StartContainer(ctx, containerID, containerName); err != nil {
		CleanupContainer(dockerMgr, ctx, containerID, containerName, volumeName)
		return nil, nil, fmt.Errorf("starting container: %w", err)
	}

	report("Waiting for database to accept connections...")
	if err := WaitForDatabase(ctx, dockerMgr, containerID, containerName, prov, adminConfig, containerStartTimeout); err != nil {
		CleanupContainer(dockerMgr, ctx, containerID, containerName, volumeName)
		return nil, nil, err
	}

	if opts.CreateUser && opts.Username != "" {
		if sqlProv, ok := providers.AsSQL(prov); ok {
			report(fmt.Sprintf("Creating user '%s'...", opts.Username))

			db, err := sqlProv.Connect(adminConfig)
			if err != nil {
				CleanupContainer(dockerMgr, ctx, containerID, containerName, volumeName)
				return nil, nil, fmt.Errorf("connecting to database: %w", err)
			}

			if err := sqlProv.CreateUser(ctx, db, opts.Username, opts.UserPassword, opts.Name); err != nil {
				db.Close()
				CleanupContainer(dockerMgr, ctx, containerID, containerName, volumeName)
				return nil, nil, fmt.Errorf("creating user: %w", err)
			}
			db.Close()
		}
	}

	dbState := &state.Database{
		Name:          opts.Name,
		Provider:      opts.Provider,
		Version:       opts.Version,
		Preset:        strings.TrimSpace(opts.Preset),
		CustomImage:   strings.TrimSpace(opts.CustomImage),
		Group:         strings.TrimSpace(opts.Group),
		Project:       strings.TrimSpace(opts.Project),
		ContainerName: containerName,
		ContainerID:   containerID,
		Port:          port,
		RootPassword:  opts.RootPassword,
		DatabaseName:  opts.Name,
		User:          opts.Username,
		UserPassword:  opts.UserPassword,
		VolumeName:    volumeName,
		CreatedAt:     time.Now(),
	}

	if err := stateMgr.Save(dbState); err != nil {
		CleanupContainer(dockerMgr, ctx, containerID, containerName, volumeName)
		return nil, nil, fmt.Errorf("saving state: %w", err)
	}

	return dbState, dbConfig, nil
}

// ProvisionAndSeedDatabase provisions a database and, when opts.SeedPath is set,
// executes the seed SQL file(s). On seed failure, the newly created container,
// volume, and state are rolled back unless opts.KeepOnError is true.
func ProvisionAndSeedDatabase(ctx context.Context, runtimeMode string, stateMgr *state.Manager, opts CreateOptions, onProgress func(string)) (*state.Database, *providers.DBConfig, int, error) {
	if strings.TrimSpace(opts.SeedPath) != "" {
		if _, err := CollectSeedFiles(opts.SeedPath); err != nil {
			return nil, nil, 0, err
		}
	}

	dbState, dbConfig, err := ProvisionDatabase(ctx, runtimeMode, stateMgr, opts, onProgress)
	if err != nil {
		return nil, nil, 0, err
	}

	if strings.TrimSpace(opts.SeedPath) == "" {
		return dbState, dbConfig, 0, nil
	}

	seededCount, err := SeedDatabase(ctx, runtimeMode, dbState.Name, opts.SeedPath, onProgress)
	if err != nil {
		if !opts.KeepOnError {
			if onProgress != nil {
				onProgress("Seed failed; rolling back created database...")
			}
			RollbackProvisionedDatabase(ctx, runtimeMode, stateMgr, dbState)
		}
		return nil, nil, seededCount, err
	}

	return dbState, dbConfig, seededCount, nil
}

// RollbackProvisionedDatabase removes a newly provisioned database's container,
// volume, and saved state when a post-provision step (like seeding or cloning) fails.
func RollbackProvisionedDatabase(ctx context.Context, runtimeMode string, stateMgr *state.Manager, dbState *state.Database) {
	if dbState == nil {
		return
	}
	if dockerMgr, err := container.NewDockerManager(runtimeMode); err == nil {
		CleanupContainer(dockerMgr, ctx, dbState.ContainerID, dbState.ContainerName, dbState.VolumeName)
	}
	if stateMgr != nil {
		_ = stateMgr.Delete(dbState.Name)
	}
}

// WaitForDatabase polls until the database server answers a real protocol handshake.
func WaitForDatabase(ctx context.Context, dockerMgr *container.DockerManager, containerID, containerName string, prov providers.Provider, cfg *providers.DBConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for {
		if status, err := dockerMgr.ContainerStatus(ctx, containerID, containerName); err == nil && status == "exited" {
			logs, tailErr := dockerMgr.TailLogs(ctx, containerID, containerName, 20)
			if tailErr != nil || logs == "" {
				return fmt.Errorf("database container exited during startup (no logs available)")
			}
			return fmt.Errorf("database container exited during startup; last log lines:\n%s", indentBlock(logs))
		}

		attemptCtx, cancel := context.WithTimeout(ctx, connectAttemptTimeout)
		lastErr = prov.Ping(attemptCtx, cfg)
		cancel()
		if lastErr == nil {
			return nil
		}

		if time.Now().After(deadline) {
			logs, tailErr := dockerMgr.TailLogs(ctx, containerID, containerName, 10)
			if tailErr == nil && logs != "" {
				return fmt.Errorf("timeout after %v waiting for database (last error: %v); container log tail:\n%s", timeout, lastErr, indentBlock(logs))
			}
			return fmt.Errorf("timeout after %v waiting for database (last error: %w); inspect the container with: docker logs %s", timeout, lastErr, containerName)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// CleanupContainer removes a failed creation's container and data volume.
func CleanupContainer(dockerMgr *container.DockerManager, ctx context.Context, containerID, containerName, volumeName string) {
	if err := dockerMgr.RemoveContainer(ctx, containerID, containerName); err != nil {
		color.Yellow("  Warning: could not clean up container '%s': %v", containerName, err)
	}
	if volumeName != "" {
		if err := dockerMgr.RemoveVolume(ctx, volumeName); err != nil {
			color.Yellow("  Warning: could not clean up volume '%s': %v", volumeName, err)
		}
	}
}

// ManagedPortOwner returns the name of the managed database bound to port.
func ManagedPortOwner(m *state.Manager, port int) string {
	dbs, err := m.List()
	if err != nil {
		return ""
	}
	for _, d := range dbs {
		if d.Port == port {
			return d.Name
		}
	}
	return ""
}

// PortInUse reports whether a local process is already listening on the port.
func PortInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), portProbeTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

var (
	dbNameRe     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,49}$`)
	versionRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	identifierRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)
	groupRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,29}$`)
)

func DBNameValidator(m *state.Manager) func(string) error {
	return func(s string) error {
		if !dbNameRe.MatchString(s) {
			return errors.New("use letters, digits, '.', '_' or '-' (max 50, start alphanumeric)")
		}
		if m.Exists(s) {
			return fmt.Errorf("database '%s' already exists", s)
		}
		return nil
	}
}

func PortValidator(m *state.Manager) func(string) error {
	return func(s string) error {
		port, err := strconv.Atoi(s)
		if err != nil || port < 1 || port > 65535 {
			return errors.New("port must be a number between 1 and 65535")
		}
		if owner := ManagedPortOwner(m, port); owner != "" {
			return fmt.Errorf("port %d is already used by database '%s'", port, owner)
		}
		if PortInUse(port) {
			return fmt.Errorf("port %d is already in use by another process", port)
		}
		return nil
	}
}

func VersionValidator(s string) error {
	if !versionRe.MatchString(s) {
		return errors.New("use letters, digits, '.', '_' or '-'")
	}
	return nil
}

func IdentifierValidator(s string) error {
	if !identifierRe.MatchString(s) {
		return errors.New("use letters, digits or '_' (max 32)")
	}
	return nil
}

func RequiredText(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("value is required")
	}
	return nil
}

func GroupValidator(s string) error {
	if !groupRe.MatchString(strings.TrimSpace(s)) {
		return errors.New("use letters, digits, '.', '_', '-' or spaces (max 30)")
	}
	return nil
}

func PortAvailabilityRows(m *state.Manager, defaultPort, suggest int) []string {
	var rows []string
	seen := map[int]bool{}
	if dbs, err := m.List(); err == nil {
		sort.Slice(dbs, func(i, j int) bool { return dbs[i].Port < dbs[j].Port })
		for _, d := range dbs {
			rows = append(rows, fmt.Sprintf("%5d  %-6s  managed database '%s'", d.Port, "in use", d.Name))
			seen[d.Port] = true
		}
	}
	if defaultPort != suggest && !seen[defaultPort] && PortInUse(defaultPort) {
		rows = append(rows, fmt.Sprintf("%5d  %-6s  another process", defaultPort, "in use"))
	}
	rows = append(rows, fmt.Sprintf("%5d  %-6s  suggested", suggest, "free"))
	return rows
}

func NextFreePort(m *state.Manager, start int) int {
	for port := start; port < start+100; port++ {
		if ManagedPortOwner(m, port) == "" && !PortInUse(port) {
			return port
		}
	}
	return start
}

func KnownGroups(m *state.Manager) []string {
	dbs, err := m.List()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var groups []string
	for _, d := range dbs {
		if d.Group == "" || seen[d.Group] {
			continue
		}
		seen[d.Group] = true
		groups = append(groups, d.Group)
	}
	sort.Strings(groups)
	return groups
}
