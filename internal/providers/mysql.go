package providers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

func init() {
	// The driver logs every protocol hiccup (e.g. each failed readiness poll
	// against a still-initializing server) straight to stderr. The CLI reports
	// its own errors, so drop the chatter.
	mysql.SetLogger(log.New(io.Discard, "", 0))
}

type MySQLProvider struct{}

func NewMySQLProvider() *MySQLProvider {
	return &MySQLProvider{}
}

func (m *MySQLProvider) Name() string {
	return "mysql"
}

func (m *MySQLProvider) Kind() EngineKind {
	return EngineSQL
}

func (m *MySQLProvider) GetDefaultVersion() string {
	return DefaultMySQLVersion
}

func (m *MySQLProvider) GetImageName(version string) string {
	if version == "" {
		version = DefaultMySQLVersion
	}
	return fmt.Sprintf("mysql:%s", version)
}

func (m *MySQLProvider) ResolveImage(version, _ string, customImage string) string {
	if img := strings.TrimSpace(customImage); img != "" {
		return img
	}
	return m.GetImageName(version)
}

func (m *MySQLProvider) GetDefaultPort() int {
	return DefaultMySQLPort
}

func (m *MySQLProvider) GetContainerPort() string {
	return "3306"
}

// GetVolumePath is version-independent for MySQL.
func (m *MySQLProvider) GetVolumePath(_ string) string {
	return "/var/lib/mysql"
}

func (m *MySQLProvider) GetEnvVars(config *DBConfig) []string {
	return []string{
		fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s", config.RootPassword),
		fmt.Sprintf("MYSQL_DATABASE=%s", config.DatabaseName),
	}
}

func (m *MySQLProvider) GetCommand(_ *DBConfig) []string {
	return nil
}

// GetConnectionString returns a display form of the connection details. The
// driver cannot represent passwords containing ':' unambiguously in this
// format; code that actually connects must use Connect instead.
func (m *MySQLProvider) GetConnectionString(config *DBConfig) string {
	user, password := config.Auth("root")
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?multiStatements=true",
		user, password,
		config.Host,
		config.Port,
		config.DatabaseName,
	)
}

func (m *MySQLProvider) GetEnvMap(config *DBConfig) map[string]string {
	user, password := config.Auth("root")
	host := config.Host
	if host == "" {
		host = "localhost"
	}
	return map[string]string{
		"DATABASE_URL": m.GetConnectionString(config),
		"DB_PROVIDER":  m.Name(),
		"DB_HOST":      host,
		"DB_PORT":      strconv.Itoa(config.Port),
		"DB_NAME":      config.DatabaseName,
		"DB_USER":      user,
		"DB_PASSWORD":  password,
	}
}

// Connect opens a sql.DB without going through the textual DSN, so passwords
// with special characters cannot break the parse.
func (m *MySQLProvider) Connect(config *DBConfig) (*sql.DB, error) {
	user, password := config.Auth("root")

	dsnCfg := mysql.NewConfig()
	dsnCfg.User = user
	dsnCfg.Passwd = password
	dsnCfg.Net = "tcp"
	dsnCfg.Addr = net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	dsnCfg.DBName = config.DatabaseName
	dsnCfg.AllowNativePasswords = true
	dsnCfg.MultiStatements = true
	dsnCfg.Timeout = 5 * time.Second // dial timeout only; queries stay unbounded

	connector, err := mysql.NewConnector(dsnCfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func (m *MySQLProvider) Ping(ctx context.Context, cfg *DBConfig) error {
	db, err := m.Connect(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.PingContext(ctx)
}

func (m *MySQLProvider) ShellCommand(cfg *DBConfig) []string {
	return []string{"mysql", "-uroot", "-p" + cfg.RootPassword, cfg.DatabaseName}
}

func (m *MySQLProvider) BackupCommand(cfg *DBConfig) []string {
	return []string{
		"mysqldump",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		cfg.DatabaseName,
	}
}

func (m *MySQLProvider) RestoreCommand(cfg *DBConfig) []string {
	return []string{
		"mysql",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		cfg.DatabaseName,
	}
}

func (m *MySQLProvider) SnapshotDumpCommand(cfg *DBConfig) ([]string, string) {
	return []string{
		"mysqldump",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		"--single-transaction",
		"--routines",
		"--triggers",
		"--add-drop-table",
		cfg.DatabaseName,
	}, SnapshotFormatSQLGzip
}

func (m *MySQLProvider) SnapshotRestoreCommand(cfg *DBConfig, _ string) []string {
	return m.RestoreCommand(cfg)
}

func (m *MySQLProvider) CloneDumpCommand(cfg *DBConfig) []string {
	return []string{
		"mysqldump",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		"--single-transaction",
		cfg.DatabaseName,
	}
}

func (m *MySQLProvider) CloneRestoreCommand(cfg *DBConfig) []string {
	return m.RestoreCommand(cfg)
}

func (m *MySQLProvider) CreateUser(ctx context.Context, db *sql.DB, username, password, dbName string) error {
	user := QuoteLiteralMySQL(username)

	_, err := db.ExecContext(ctx, fmt.Sprintf(
		"CREATE USER IF NOT EXISTS %s@'%%' IDENTIFIED BY %s",
		user, QuoteLiteralMySQL(password),
	))
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf(
		"GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'",
		QuoteIdentMySQL(dbName), user,
	))
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, "FLUSH PRIVILEGES")
	return err
}

func (m *MySQLProvider) ListTables(ctx context.Context, db *sql.DB, dbName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW TABLES FROM %s", QuoteIdentMySQL(dbName)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			continue
		}
		tables = append(tables, tableName)
	}
	return tables, nil
}
