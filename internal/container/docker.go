package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"golang.org/x/term"
)

const (
	DefaultStopTimeout  = 10
	connectProbeTimeout = 1500 * time.Millisecond
)

type DockerManager struct {
	client *client.Client
}

// endpoint is one candidate daemon address to probe.
type endpoint struct {
	name string
	host string
}

// candidateEndpoints lists daemon addresses in probe order for the selected
// runtime mode: "auto", "docker" or "podman". A set DOCKER_HOST always wins
// (the standard override).
func candidateEndpoints(mode string) []endpoint {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return []endpoint{{"DOCKER_HOST", host}}
	}

	if runtime.GOOS == "windows" {
		// Windows has no unix sockets: Docker Desktop serves a named pipe,
		// and podman machine forwards its API through its own pipe.
		var out []endpoint
		if mode == "auto" || mode == "docker" {
			out = append(out, endpoint{"docker desktop", "npipe:////./pipe/docker_engine"})
		}
		if mode == "auto" || mode == "podman" {
			out = append(out, endpoint{"podman", "npipe:////./pipe/podman"})
		}
		return out
	}

	var out []endpoint
	if mode == "auto" || mode == "docker" {
		out = append(out, endpoint{"docker", "unix:///var/run/docker.sock"})
	}
	if mode == "auto" || mode == "podman" {
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			out = append(out, endpoint{"podman (rootless)", "unix://" + dir + "/podman/podman.sock"})
		}
		if uid := os.Getuid(); uid > 0 {
			out = append(out, endpoint{"podman (rootless)", fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", uid)})
		}
		out = append(out, endpoint{"podman (rootful)", "unix:///run/podman/podman.sock"})
	}
	return out
}

// NewDockerManager connects to a Docker-compatible daemon (Docker, Podman, or
// anything speaking the same API), probing candidates until one answers.
func NewDockerManager(runtime string) (*DockerManager, error) {
	var tried []string
	for _, ep := range candidateEndpoints(runtime) {
		opts := []client.Opt{client.WithHost(ep.host), client.WithAPIVersionNegotiation()}
		if ep.name == "DOCKER_HOST" {
			// The standard override also carries TLS settings from env.
			opts = []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
		}

		cli, err := client.NewClientWithOpts(opts...)
		if err != nil {
			tried = append(tried, ep.name+": "+err.Error())
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), connectProbeTimeout)
		_, err = cli.Ping(ctx)
		cancel()
		if err == nil {
			return &DockerManager{client: cli}, nil
		}
		tried = append(tried, fmt.Sprintf("%s [%s]: %v", ep.name, ep.host, err))
		cli.Close()
	}

	return nil, fmt.Errorf(
		"no Docker-compatible daemon reachable (tried: %s). Start Docker, or for rootless podman: systemctl --user start podman.socket",
		strings.Join(tried, "; "),
	)
}

func (d *DockerManager) PullImage(ctx context.Context, imageName string) error {
	reader, err := d.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()

	// Read the output so the pull completes before returning.
	_, err = io.Copy(io.Discard, reader)
	return err
}

func (d *DockerManager) CreateContainer(ctx context.Context, config *ContainerConfig) (string, error) {
	// Loopback only: dev databases must not be reachable from other machines.
	portBinding := nat.PortBinding{
		HostIP:   "127.0.0.1",
		HostPort: fmt.Sprintf("%d", config.Port),
	}

	containerPort, err := nat.NewPort("tcp", config.ContainerPort)
	if err != nil {
		return "", err
	}

	containerConfig := &container.Config{
		Image: config.Image,
		Env:   config.Env,
		ExposedPorts: nat.PortSet{
			containerPort: struct{}{},
		},
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			containerPort: []nat.PortBinding{portBinding},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	if config.VolumeName != "" && config.VolumePath != "" {
		hostConfig.Mounts = []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: config.VolumeName,
				Target: config.VolumePath,
			},
		}
	}

	resp, err := d.client.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		nil,
		nil,
		config.Name,
	)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (d *DockerManager) StartContainer(ctx context.Context, containerID, containerName string) error {
	return d.client.ContainerStart(ctx, resolveContainerRef(containerID, containerName), container.StartOptions{})
}

func (d *DockerManager) StopContainer(ctx context.Context, containerID, containerName string) error {
	timeout := DefaultStopTimeout
	return d.client.ContainerStop(ctx, resolveContainerRef(containerID, containerName), container.StopOptions{Timeout: &timeout})
}

func (d *DockerManager) RemoveContainer(ctx context.Context, containerID, containerName string) error {
	return d.client.ContainerRemove(ctx, resolveContainerRef(containerID, containerName), container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
}

// resolveContainerRef picks the daemon reference for a stored container:
// the name is stable across recreations, the ID can go stale. The daemon
// API accepts either form.
func resolveContainerRef(containerID, containerName string) string {
	if containerName != "" {
		return containerName
	}
	return containerID
}

func (d *DockerManager) IsContainerRunning(ctx context.Context, containerID, containerName string) (bool, error) {
	inspect, err := d.client.ContainerInspect(ctx, resolveContainerRef(containerID, containerName))
	if err != nil {
		return false, err
	}
	return inspect.State.Running, nil
}

// ContainerStatus returns the daemon's lifecycle status for the container
// ("running", "exited", "restarting", ...).
func (d *DockerManager) ContainerStatus(ctx context.Context, containerID, containerName string) (string, error) {
	inspect, err := d.client.ContainerInspect(ctx, resolveContainerRef(containerID, containerName))
	if err != nil {
		return "", err
	}
	return inspect.State.Status, nil
}

// TailLogs returns the last decoded log lines of a container, stdout and
// stderr merged.
func (d *DockerManager) TailLogs(ctx context.Context, containerID, containerName string, tail int) (string, error) {
	reader, err := d.client.ContainerLogs(ctx, resolveContainerRef(containerID, containerName), container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       strconv.Itoa(tail),
	})
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, reader); err != nil {
		return "", err
	}
	combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	return combined, nil
}

// GetContainerLogs retrieves logs from a container
func (d *DockerManager) GetContainerLogs(ctx context.Context, containerID, containerName, tail string, follow bool) (io.ReadCloser, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Timestamps: true,
	}
	return d.client.ContainerLogs(ctx, resolveContainerRef(containerID, containerName), options)
}

// ExecResult reports the captured stderr and exit status of a finished exec.
// Stdout is streamed straight to the caller's writer and never buffered, so
// dumps of any size pass through without piling up in memory.
type ExecResult struct {
	Stderr   string
	ExitCode int
}

// Exec runs cmd in the container, streaming stdin into it and its stdout into
// stdout (nil discards), while stderr is captured. err reports transport
// failures only; check ExitCode for the command's own status.
func (d *DockerManager) Exec(ctx context.Context, containerID, containerName string, cmd []string, stdin io.Reader, stdout io.Writer) (*ExecResult, error) {
	ref := resolveContainerRef(containerID, containerName)

	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}
	if stdin != nil {
		execConfig.AttachStdin = true
	}

	execID, err := d.client.ContainerExecCreate(ctx, ref, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := d.client.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to attach exec: %w", err)
	}
	defer resp.Close()

	// Pump stdin concurrently: a large dump can fill the pipe while the
	// command's output fills ours, which would deadlock on sequential I/O.
	// Copying the reader concurrently drains output at the same time.
	stdinErr := make(chan error, 1)
	if stdin != nil {
		go func() {
			_, err := io.Copy(resp.Conn, stdin)
			if err == nil {
				resp.CloseWrite()
			}
			stdinErr <- err
		}()
	}

	var stderr bytes.Buffer
	out := stdout
	if out == nil {
		out = io.Discard
	}
	if _, err := stdcopy.StdCopy(out, &stderr, resp.Reader); err != nil {
		return nil, fmt.Errorf("failed to read exec output: %w", err)
	}
	if stdin != nil {
		// The exec stream closes when the command finishes, so a stuck write
		// cannot outlive StdCopy.
		if err := <-stdinErr; err != nil {
			return nil, fmt.Errorf("failed to write stdin: %w", err)
		}
	}

	inspect, err := d.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return &ExecResult{
		Stderr:   stderr.String(),
		ExitCode: inspect.ExitCode,
	}, nil
}

// ExecInteractive attaches an interactive TTY session to a command inside the
// container (e.g., psql or mysql).
func (d *DockerManager) ExecInteractive(ctx context.Context, containerID, containerName string, cmd []string) error {
	ref := resolveContainerRef(containerID, containerName)

	execConfig := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          cmd,
	}

	execID, err := d.client.ContainerExecCreate(ctx, ref, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := d.client.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{Tty: true})
	if err != nil {
		return fmt.Errorf("failed to attach exec: %w", err)
	}
	defer resp.Close()

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err == nil {
			defer term.Restore(fd, oldState)
		}
		if w, h, err := term.GetSize(fd); err == nil {
			_ = d.client.ContainerExecResize(ctx, execID.ID, container.ResizeOptions{
				Height: uint(h),
				Width:  uint(w),
			})
		}
	}

	outDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(os.Stdout, resp.Reader)
		outDone <- err
	}()

	go func() {
		_, _ = io.Copy(resp.Conn, os.Stdin)
		resp.CloseWrite()
	}()

	if err := <-outDone; err != nil {
		return fmt.Errorf("interactive session error: %w", err)
	}

	inspect, err := d.client.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("shell exited with code %d", inspect.ExitCode)
	}
	return nil
}

// VolumeExists reports whether a named volume exists.
func (d *DockerManager) VolumeExists(ctx context.Context, name string) (bool, error) {
	_, err := d.client.VolumeInspect(ctx, name)
	if client.IsErrNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// RemoveVolume deletes a named volume.
func (d *DockerManager) RemoveVolume(ctx context.Context, name string) error {
	return d.client.VolumeRemove(ctx, name, false)
}

// ManagedContainer summarizes a container discovered on the daemon.
type ManagedContainer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Status string `json:"status"`
}

// ListContainersByPrefix returns all containers (running or stopped) whose name
// starts with prefix.
func (d *DockerManager) ListContainersByPrefix(ctx context.Context, prefix string) ([]ManagedContainer, error) {
	items, err := d.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	var out []ManagedContainer
	for _, c := range items {
		for _, rawName := range c.Names {
			name := strings.TrimPrefix(rawName, "/")
			if strings.HasPrefix(name, prefix) {
				out = append(out, ManagedContainer{
					ID:     c.ID,
					Name:   name,
					State:  c.State,
					Status: c.Status,
				})
				break
			}
		}
	}
	return out, nil
}

// ListVolumesByPrefix returns all volume names starting with prefix.
func (d *DockerManager) ListVolumesByPrefix(ctx context.Context, prefix string) ([]string, error) {
	resp, err := d.client.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, v := range resp.Volumes {
		if v != nil && strings.HasPrefix(v.Name, prefix) {
			out = append(out, v.Name)
		}
	}
	return out, nil
}

type ContainerConfig struct {
	Name          string
	Image         string
	Port          int
	ContainerPort string
	Env           []string
	VolumeName    string
	VolumePath    string
}
