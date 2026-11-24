package providers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	DefaultMySQLPort    = 3306
	DefaultPostgresPort = 5432

	DefaultMySQLVersion    = "8.0"
	DefaultPostgresVersion = "16"

	// connectTimeoutSeconds bounds one TCP + handshake attempt, so a hung
	// dial cannot stall past the caller's own deadline.
	connectTimeoutSeconds = 5
)

// EngineKind classifies the data model of a database provider so CLI and TUI
// views can adapt cleanly across SQL, key-value, and document engines.
type EngineKind string

const (
	EngineSQL      EngineKind = "sql"
	EngineKV       EngineKind = "kv"
	EngineDocument EngineKind = "document"
)

// SnapshotFormat identifies how a logical snapshot payload is encoded.
const (
	SnapshotFormatSQLGzip = "sql.gz"
	SnapshotFormatPGDump  = "pgdump"
)

// Provider is the core lifecycle contract shared by every database engine.
type Provider interface {
	Name() string
	Kind() EngineKind
	GetImageName(version string) string
	ResolveImage(version, preset, customImage string) string
	GetDefaultVersion() string
	GetDefaultPort() int
	GetContainerPort() string
	GetEnvVars(config *DBConfig) []string
	GetCommand(config *DBConfig) []string
	GetConnectionString(config *DBConfig) string
	GetEnvMap(config *DBConfig) map[string]string
	// GetVolumePath returns the container path a named volume must be
	// mounted at for the given version.
	GetVolumePath(version string) string
	// Ping checks protocol-level readiness against a running instance.
	Ping(ctx context.Context, cfg *DBConfig) error
	// ShellCommand returns the argv for an interactive shell inside the container.
	ShellCommand(cfg *DBConfig) []string
}

// SQLProvider is implemented by relational engines that expose *sql.DB access.
type SQLProvider interface {
	Provider
	// Connect opens a working sql.DB for the given config. Passwords that are
	// awkward in a connection string (MySQL ':', '/', '@') are safe here.
	Connect(cfg *DBConfig) (*sql.DB, error)
	CreateUser(ctx context.Context, db *sql.DB, username, password, dbName string) error
	ListTables(ctx context.Context, db *sql.DB, dbName string) ([]string, error)
}

// SnapshotProvider is implemented by engines supporting in-container logical
// backup, restore, snapshot, and streaming clone operations.
type SnapshotProvider interface {
	Provider
	BackupCommand(cfg *DBConfig) []string
	RestoreCommand(cfg *DBConfig) []string
	SnapshotDumpCommand(cfg *DBConfig) (cmd []string, format string)
	SnapshotRestoreCommand(cfg *DBConfig, format string) []string
	CloneDumpCommand(cfg *DBConfig) []string
	CloneRestoreCommand(cfg *DBConfig) []string
}

// AsSQL returns the SQLProvider capability if p supports relational SQL access.
func AsSQL(p Provider) (SQLProvider, bool) {
	sp, ok := p.(SQLProvider)
	return sp, ok
}

// AsSnapshot returns the SnapshotProvider capability if p supports logical
// backup, restore, snapshot, and clone operations.
func AsSnapshot(p Provider) (SnapshotProvider, bool) {
	sp, ok := p.(SnapshotProvider)
	return sp, ok
}

// For returns the provider implementation for a provider name.
func For(name string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mysql":
		return NewMySQLProvider(), nil
	case "postgres":
		return NewPostgresProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported provider '%s'", name)
	}
}

type DBConfig struct {
	Host         string
	Port         int
	RootPassword string
	DatabaseName string
	User         string
	UserPassword string
}

// KnownVersions lists Docker tags commonly used for local development,
// newest first. The default version still gets the highlight in the picker;
// users can always enter any other tag manually.
func KnownVersions(provider string) []string {
	if provider == "postgres" {
		return []string{"18", "17", "16", "15", "14"}
	}
	return []string{"9.0", "8.4", "8.0"}
}

// Auth returns the username and password to connect with, preferring the
// dedicated user over the superuser account.
func (c *DBConfig) Auth(superuser string) (string, string) {
	if c.User != "" {
		pass := c.UserPassword
		if pass == "" {
			pass = c.RootPassword
		}
		return c.User, pass
	}
	return superuser, c.RootPassword
}
