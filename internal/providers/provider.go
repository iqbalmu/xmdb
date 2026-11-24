package providers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	DefaultMySQLPort    = 3306
	DefaultPostgresPort = 5432
	DefaultMariaDBPort  = 3306
	DefaultRedisPort    = 6379
	DefaultValkeyPort   = 6379
	DefaultMongoDBPort  = 27017

	DefaultMySQLVersion    = "8.0"
	DefaultPostgresVersion = "16"
	DefaultMariaDBVersion  = "11.4"
	DefaultRedisVersion    = "7.4"
	DefaultValkeyVersion   = "8.0"
	DefaultMongoDBVersion  = "8.0"

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
	SnapshotFormatSQLGzip      = "sql.gz"
	SnapshotFormatPGDump       = "pgdump"
	SnapshotFormatKVJSONGzip   = "kv.json.gz"
	SnapshotFormatMongoArchive = "mongo.archive.gz"
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

// PostInitProvider is implemented by SQL providers that run post-init DDL for presets
// (e.g., CREATE EXTENSION IF NOT EXISTS vector).
type PostInitProvider interface {
	PostInit(ctx context.Context, db *sql.DB, preset string) error
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

// KVKeyInfo summarizes a key returned by SCAN on a key-value store.
type KVKeyInfo struct {
	Key  string
	Type string
	TTL  int64 // -1 = no expiry, -2 = missing, >=0 = seconds remaining
}

// KVKeyDetail holds the inspected value and metadata of a single KV key.
type KVKeyDetail struct {
	Key   string
	Type  string
	TTL   int64
	Size  int
	Value string
}

// KVProvider is implemented by Redis/Valkey key-value engines.
type KVProvider interface {
	Provider
	ScanKeys(ctx context.Context, cfg *DBConfig, pattern string, limit int) ([]KVKeyInfo, error)
	GetKeyDetail(ctx context.Context, cfg *DBConfig, key string) (*KVKeyDetail, error)
	ExecKV(ctx context.Context, cfg *DBConfig, cmdLine string) (string, time.Duration, error)
	DumpKV(ctx context.Context, cfg *DBConfig, w io.Writer) error
	RestoreKV(ctx context.Context, cfg *DBConfig, r io.Reader) error
}

// DocumentProvider is implemented by document engines (MongoDB).
type DocumentProvider interface {
	Provider
	ListCollectionsCommand(cfg *DBConfig) []string
	SampleCollectionCommand(cfg *DBConfig, collection string, limit, skip int, filterJSON string) []string
	EvalQueryCommand(cfg *DBConfig, expr string) []string
}

// AsSQL returns the SQLProvider capability if p supports relational SQL access.
func AsSQL(p Provider) (SQLProvider, bool) {
	sp, ok := p.(SQLProvider)
	return sp, ok
}

// AsSnapshot returns the SnapshotProvider capability if p supports in-container
// command-based backup, restore, snapshot, and clone operations.
func AsSnapshot(p Provider) (SnapshotProvider, bool) {
	sp, ok := p.(SnapshotProvider)
	return sp, ok
}

// AsKV returns the KVProvider capability if p is a key-value store.
func AsKV(p Provider) (KVProvider, bool) {
	kp, ok := p.(KVProvider)
	return kp, ok
}

// AsDocument returns the DocumentProvider capability if p is a document store.
func AsDocument(p Provider) (DocumentProvider, bool) {
	dp, ok := p.(DocumentProvider)
	return dp, ok
}

// IsMySQLFamily reports whether provider speaks the MySQL SQL dialect.
func IsMySQLFamily(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	return p == "mysql" || p == "mariadb"
}

// AllProviders returns all supported database providers in display order.
func AllProviders() []Provider {
	return []Provider{
		NewPostgresProvider(),
		NewMySQLProvider(),
		NewMariaDBProvider(),
		NewRedisProvider(),
		NewValkeyProvider(),
		NewMongoDBProvider(),
	}
}

// SupportedProviderNames returns the canonical names of all supported providers.
func SupportedProviderNames() []string {
	all := AllProviders()
	names := make([]string, 0, len(all))
	for _, p := range all {
		names = append(names, p.Name())
	}
	return names
}

// For returns the provider implementation for a provider name.
func For(name string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "mysql":
		return NewMySQLProvider(), nil
	case "postgres":
		return NewPostgresProvider(), nil
	case "mariadb":
		return NewMariaDBProvider(), nil
	case "redis":
		return NewRedisProvider(), nil
	case "valkey":
		return NewValkeyProvider(), nil
	case "mongodb":
		return NewMongoDBProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported provider '%s' (supported: %s)", name, strings.Join(SupportedProviderNames(), ", "))
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
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "postgres":
		return []string{"18", "17", "16", "15", "14"}
	case "mariadb":
		return []string{"11.4", "11.2", "10.11"}
	case "redis":
		return []string{"7.4", "7.2", "6.2"}
	case "valkey":
		return []string{"8.0", "7.2"}
	case "mongodb":
		return []string{"8.0", "7.0", "6.0"}
	default:
		return []string{"9.0", "8.4", "8.0"}
	}
}

// KnownPresets lists supported image/extension presets for provider.
func KnownPresets(provider string) []string {
	if strings.ToLower(strings.TrimSpace(provider)) == "postgres" {
		return []string{"pgvector", "postgis", "timescaledb"}
	}
	return nil
}

// ValidatePreset validates an optional preset name for provider.
func ValidatePreset(provider, preset string) error {
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == "" {
		return nil
	}
	known := KnownPresets(provider)
	for _, k := range known {
		if k == preset {
			return nil
		}
	}
	if len(known) == 0 {
		return fmt.Errorf("provider '%s' does not support presets (got %q)", provider, preset)
	}
	return fmt.Errorf("unsupported preset %q for %s (supported: %s)", preset, provider, strings.Join(known, ", "))
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
