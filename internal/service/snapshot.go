package service

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/iqbalmu/xmdb/internal/container"
	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
	"github.com/iqbalmu/xmdb/pkg/utils"
)

const (
	SnapshotsDir            = "snapshots"
	SnapshotMetaFile        = "meta.json"
	DefaultMaxAutoSnapshots = 5
)

var snapshotNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// SnapshotMeta describes a single managed logical snapshot on disk.
type SnapshotMeta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Database  string    `json:"database"`
	Provider  string    `json:"provider"`
	Version   string    `json:"version"`
	Format    string    `json:"format"`
	FileName  string    `json:"file_name"`
	SizeBytes int64     `json:"size_bytes"`
	Auto      bool      `json:"auto,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// SnapshotRestoreOptions configures snapshot rollback behavior.
type SnapshotRestoreOptions struct {
	NoAutoSnapshot bool
}

// SnapshotManager manages snapshot metadata and payload files under a root directory.
type SnapshotManager struct {
	baseDir string
}

func NewSnapshotManager() (*SnapshotManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return NewSnapshotManagerAt(filepath.Join(homeDir, state.StateDir, SnapshotsDir))
}

func NewSnapshotManagerAt(baseDir string) (*SnapshotManager, error) {
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create snapshots directory: %w", err)
	}
	return &SnapshotManager{baseDir: baseDir}, nil
}

func (m *SnapshotManager) dbDir(dbName string) string {
	return filepath.Join(m.baseDir, dbName)
}

func (m *SnapshotManager) snapDir(dbName, id string) string {
	return filepath.Join(m.baseDir, dbName, id)
}

func (m *SnapshotManager) DataPath(meta *SnapshotMeta) string {
	return filepath.Join(m.snapDir(meta.Database, meta.ID), meta.FileName)
}

func (m *SnapshotManager) SaveMeta(meta *SnapshotMeta) error {
	dir := m.snapDir(meta.Database, meta.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshot metadata: %w", err)
	}
	metaPath := filepath.Join(dir, SnapshotMetaFile)
	tmpPath := metaPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("writing snapshot metadata: %w", err)
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("saving snapshot metadata: %w", err)
	}
	return nil
}

// List returns all snapshots for dbName ordered newest-first.
func (m *SnapshotManager) List(dbName string) ([]*SnapshotMeta, error) {
	entries, err := os.ReadDir(m.dbDir(dbName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading snapshots directory: %w", err)
	}

	var out []*SnapshotMeta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(m.dbDir(dbName), entry.Name(), SnapshotMetaFile)
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta SnapshotMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		out = append(out, &meta)
	}

	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// Find resolves a snapshot by exact ID first, then by exact Name (newest match).
func (m *SnapshotManager) Find(dbName, ref string) (*SnapshotMeta, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("snapshot name or ID is required")
	}
	snaps, err := m.List(dbName)
	if err != nil {
		return nil, err
	}
	for _, s := range snaps {
		if s.ID == ref {
			return s, nil
		}
	}
	for _, s := range snaps {
		if s.Name == ref {
			return s, nil
		}
	}
	return nil, fmt.Errorf("snapshot '%s' not found for database '%s'", ref, dbName)
}

// Delete removes a snapshot directory by ID or Name.
func (m *SnapshotManager) Delete(dbName, ref string) (*SnapshotMeta, error) {
	meta, err := m.Find(dbName, ref)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(m.snapDir(dbName, meta.ID)); err != nil {
		return nil, fmt.Errorf("deleting snapshot '%s': %w", meta.Name, err)
	}
	return meta, nil
}

// PruneAuto removes the oldest auto-snapshots for dbName beyond keep.
// Manual snapshots are never pruned.
func (m *SnapshotManager) PruneAuto(dbName string, keep int) (int, error) {
	if keep < 0 {
		keep = 0
	}
	snaps, err := m.List(dbName)
	if err != nil {
		return 0, err
	}
	autoCount := 0
	removed := 0
	for _, s := range snaps {
		if !s.Auto {
			continue
		}
		autoCount++
		if autoCount <= keep {
			continue
		}
		if err := os.RemoveAll(m.snapDir(dbName, s.ID)); err == nil {
			removed++
		}
	}
	return removed, nil
}

// RenameDatabaseDir moves snapshots when a database is renamed and updates metadata.
func (m *SnapshotManager) RenameDatabaseDir(oldName, newName string) error {
	oldDir := m.dbDir(oldName)
	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return nil
	}
	newDir := m.dbDir(newName)
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("snapshot directory for '%s' already exists", newName)
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("renaming snapshot directory: %w", err)
	}
	snaps, err := m.List(newName)
	if err != nil {
		return nil
	}
	for _, s := range snaps {
		s.Database = newName
		_ = m.SaveMeta(s)
	}
	return nil
}

// ValidateSnapshotName checks whether name is a valid snapshot label.
func ValidateSnapshotName(name string) error {
	if !snapshotNameRe.MatchString(name) {
		return fmt.Errorf("invalid snapshot name %q: use letters, digits, '.', '_' or '-' (max 64, start alphanumeric)", name)
	}
	return nil
}

func generateSnapshotID(now time.Time) string {
	var buf [3]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%s-%s", now.UTC().Format("20060102-150405"), hex.EncodeToString(buf[:]))
}

func snapshotFileName(format string) string {
	switch format {
	case providers.SnapshotFormatPGDump:
		return "data.pgdump"
	case providers.SnapshotFormatSQLGzip:
		return "data.sql.gz"
	case providers.SnapshotFormatKVJSONGzip:
		return "data.kv.json.gz"
	case providers.SnapshotFormatMongoArchive:
		return "data.archive.gz"
	default:
		return "data.dump"
	}
}

func adminDBConfig(db *state.Database) *providers.DBConfig {
	return &providers.DBConfig{
		Host:         "127.0.0.1",
		Port:         db.Port,
		RootPassword: db.RootPassword,
		DatabaseName: db.DatabaseName,
	}
}

// CreateSnapshot takes an online logical snapshot of dbName without stopping the container.
func CreateSnapshot(ctx context.Context, runtimeMode, dbName, snapName, note string, auto bool) (*SnapshotMeta, error) {
	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}
	db, err := stateMgr.Load(dbName)
	if err != nil {
		return nil, fmt.Errorf("database '%s' not found", dbName)
	}

	prov, err := providers.For(db.Provider)
	if err != nil {
		return nil, err
	}
	kvProv, isKV := providers.AsKV(prov)
	snapProv, isSnap := providers.AsSnapshot(prov)
	if !isKV && !isSnap {
		return nil, fmt.Errorf("provider '%s' does not support snapshots", db.Provider)
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, err
	}

	running, err := dockerMgr.IsContainerRunning(ctx, db.ContainerID, db.ContainerName)
	if err != nil || !running {
		return nil, fmt.Errorf("database '%s' is not running", dbName)
	}

	snapMgr, err := NewSnapshotManager()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	snapName = strings.TrimSpace(snapName)
	if snapName == "" {
		snapName = fmt.Sprintf("snap-%s", now.Format("20060102-150405"))
	} else if err := ValidateSnapshotName(snapName); err != nil {
		return nil, err
	}

	var cmd []string
	var format string
	if isKV {
		format = providers.SnapshotFormatKVJSONGzip
	} else {
		cmd, format = snapProv.SnapshotDumpCommand(adminDBConfig(db))
	}
	id := generateSnapshotID(now)
	fileName := snapshotFileName(format)

	meta := &SnapshotMeta{
		ID:        id,
		Name:      snapName,
		Database:  db.Name,
		Provider:  db.Provider,
		Version:   db.Version,
		Format:    format,
		FileName:  fileName,
		Auto:      auto,
		Note:      strings.TrimSpace(note),
		CreatedAt: now,
	}

	dir := snapMgr.snapDir(db.Name, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating snapshot directory: %w", err)
	}

	dataPath := snapMgr.DataPath(meta)
	tmpPath := dataPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("creating snapshot payload: %w", err)
	}

	if isKV {
		dumpErr := kvProv.DumpKV(ctx, adminDBConfig(db), f)
		closeErr := f.Close()
		if dumpErr != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("creating KV snapshot: %w", dumpErr)
		}
		if closeErr != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("finalizing KV snapshot: %w", closeErr)
		}
	} else {
		var outWriter io.Writer = f
		var gz *gzip.Writer
		if format == providers.SnapshotFormatSQLGzip {
			gz = gzip.NewWriter(f)
			outWriter = gz
		}

		res, execErr := dockerMgr.Exec(ctx, db.ContainerID, db.ContainerName, cmd, nil, outWriter)
		if gz != nil {
			if closeErr := gz.Close(); closeErr != nil && execErr == nil {
				execErr = closeErr
			}
		}
		if closeErr := f.Close(); closeErr != nil && execErr == nil {
			execErr = closeErr
		}

		if execErr != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("creating snapshot: %w", execErr)
		}
		if res.ExitCode != 0 {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("creating snapshot (exit %d):\n%s", res.ExitCode, res.Stderr)
		}
	}

	if err := os.Rename(tmpPath, dataPath); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("finalizing snapshot payload: %w", err)
	}

	if info, err := os.Stat(dataPath); err == nil {
		meta.SizeBytes = info.Size()
	}

	if err := snapMgr.SaveMeta(meta); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	if auto {
		_, _ = snapMgr.PruneAuto(db.Name, DefaultMaxAutoSnapshots)
	}
	return meta, nil
}

// CreateAutoSnapshot takes a safety snapshot before a destructive operation if
// the container is running. Returns (nil, nil) if the container is stopped.
func CreateAutoSnapshot(ctx context.Context, runtimeMode, dbName, reason string) (*SnapshotMeta, error) {
	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}
	db, err := stateMgr.Load(dbName)
	if err != nil {
		return nil, err
	}
	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, err
	}
	running, err := dockerMgr.IsContainerRunning(ctx, db.ContainerID, db.ContainerName)
	if err != nil || !running {
		return nil, nil
	}

	label := fmt.Sprintf("auto-pre-%s-%s", reason, time.Now().UTC().Format("20060102-150405"))
	note := fmt.Sprintf("auto safety snapshot before %s", reason)
	return CreateSnapshot(ctx, runtimeMode, dbName, label, note, true)
}

// RestoreSnapshot rolls dbName back to the referenced snapshot.
func RestoreSnapshot(ctx context.Context, runtimeMode, dbName, ref string, opts SnapshotRestoreOptions, onProgress func(string)) (*SnapshotMeta, error) {
	report := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	stateMgr, err := state.NewManager()
	if err != nil {
		return nil, err
	}
	db, err := stateMgr.Load(dbName)
	if err != nil {
		return nil, fmt.Errorf("database '%s' not found", dbName)
	}

	prov, err := providers.For(db.Provider)
	if err != nil {
		return nil, err
	}
	snapMgr, err := NewSnapshotManager()
	if err != nil {
		return nil, err
	}
	meta, err := snapMgr.Find(dbName, ref)
	if err != nil {
		return nil, err
	}
	if meta.Provider != db.Provider {
		return nil, fmt.Errorf("snapshot provider '%s' does not match database provider '%s'", meta.Provider, db.Provider)
	}

	dockerMgr, err := container.NewDockerManager(runtimeMode)
	if err != nil {
		return nil, err
	}
	running, err := dockerMgr.IsContainerRunning(ctx, db.ContainerID, db.ContainerName)
	if err != nil || !running {
		return nil, fmt.Errorf("database '%s' is not running", dbName)
	}

	if !opts.NoAutoSnapshot {
		report("Taking safety auto-snapshot before rollback...")
		if autoMeta, err := CreateAutoSnapshot(ctx, runtimeMode, dbName, "snapshot-restore"); err != nil {
			return nil, fmt.Errorf("creating safety snapshot (pass --no-snapshot to skip): %w", err)
		} else if autoMeta != nil {
			report(fmt.Sprintf("Saved safety snapshot '%s'", autoMeta.Name))
		}
	}

	report("Cleaning existing database objects...")
	if err := resetDatabaseContentsForRestore(ctx, prov, db); err != nil {
		return nil, fmt.Errorf("preparing database for rollback: %w", err)
	}

	report(fmt.Sprintf("Restoring snapshot '%s'...", meta.Name))
	if err := StreamSnapshotToDatabase(ctx, dockerMgr, prov, db, snapMgr, meta); err != nil {
		return nil, err
	}

	if err := restoreUserGrantsAfterSnapshot(ctx, prov, db); err != nil {
		report(fmt.Sprintf("Warning: could not refresh user grants: %v", err))
	}

	return meta, nil
}

// StreamSnapshotToDatabase streams a saved snapshot payload into target.
func StreamSnapshotToDatabase(ctx context.Context, dockerMgr *container.DockerManager, prov providers.Provider, target *state.Database, snapMgr *SnapshotManager, meta *SnapshotMeta) error {
	dataPath := snapMgr.DataPath(meta)
	f, err := os.Open(dataPath)
	if err != nil {
		return fmt.Errorf("opening snapshot payload: %w", err)
	}
	defer f.Close()

	if kvProv, ok := providers.AsKV(prov); ok {
		return kvProv.RestoreKV(ctx, adminDBConfig(target), f)
	}

	snapProv, ok := providers.AsSnapshot(prov)
	if !ok {
		return fmt.Errorf("provider '%s' does not support snapshot restore", target.Provider)
	}

	var reader io.Reader = f
	if meta.Format == providers.SnapshotFormatSQLGzip {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("reading compressed snapshot: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	cmd := snapProv.SnapshotRestoreCommand(adminDBConfig(target), meta.Format)
	res, err := dockerMgr.Exec(ctx, target.ContainerID, target.ContainerName, cmd, reader, io.Discard)
	if err != nil {
		return fmt.Errorf("restoring snapshot: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("restoring snapshot (exit %d):\n%s", res.ExitCode, res.Stderr)
	}
	return nil
}

// resetDatabaseContentsForRestore wipes the logical database inside a running
// SQL container so objects created after a snapshot do not linger after rollback.
func resetDatabaseContentsForRestore(ctx context.Context, prov providers.Provider, dbState *state.Database) error {
	sqlProv, ok := providers.AsSQL(prov)
	if !ok {
		return nil
	}

	if dbState.Provider == utils.ProviderPostgres {
		maintCfg := &providers.DBConfig{
			Host:         "127.0.0.1",
			Port:         dbState.Port,
			RootPassword: dbState.RootPassword,
			DatabaseName: "postgres",
		}
		conn, err := sqlProv.Connect(maintCfg)
		if err != nil {
			return err
		}
		defer conn.Close()

		_, _ = conn.ExecContext(ctx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbState.DatabaseName,
		)
		dbIdent := providers.QuoteIdentPG(dbState.DatabaseName)
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbIdent)); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", dbIdent)); err != nil {
			return err
		}
		if dbState.User != "" {
			userIdent := providers.QuoteIdentPG(dbState.User)
			_, _ = conn.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", dbIdent, userIdent))
			_, _ = conn.ExecContext(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", dbIdent, userIdent))
		}
		return nil
	}

	if providers.IsMySQLFamily(dbState.Provider) {
		conn, err := sqlProv.Connect(adminDBConfig(dbState))
		if err != nil {
			return err
		}
		defer conn.Close()

		dbIdent := providers.QuoteIdentMySQL(dbState.DatabaseName)
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbIdent)); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", dbIdent)); err != nil {
			return err
		}
		if dbState.User != "" {
			userLit := providers.QuoteLiteralMySQL(dbState.User)
			_, _ = conn.ExecContext(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'", dbIdent, userLit))
			_, _ = conn.ExecContext(ctx, "FLUSH PRIVILEGES")
		}
		return nil
	}

	return nil
}

// restoreUserGrantsAfterSnapshot ensures the provisioned dev user has access to
// objects restored by the superuser (especially on PostgreSQL).
func restoreUserGrantsAfterSnapshot(ctx context.Context, prov providers.Provider, dbState *state.Database) error {
	if dbState.User == "" || dbState.Provider != utils.ProviderPostgres {
		return nil
	}
	sqlProv, ok := providers.AsSQL(prov)
	if !ok {
		return nil
	}
	conn, err := sqlProv.Connect(adminDBConfig(dbState))
	if err != nil {
		return err
	}
	defer conn.Close()

	userIdent := providers.QuoteIdentPG(dbState.User)
	_, _ = conn.ExecContext(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO %s", userIdent))
	_, _ = conn.ExecContext(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO %s", userIdent))
	return nil
}
