package providers

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// MariaDBProvider implements Provider, SQLProvider, and SnapshotProvider for MariaDB.
type MariaDBProvider struct{}

func NewMariaDBProvider() *MariaDBProvider {
	return &MariaDBProvider{}
}

func (m *MariaDBProvider) Name() string {
	return "mariadb"
}

func (m *MariaDBProvider) Kind() EngineKind {
	return EngineSQL
}

func (m *MariaDBProvider) GetDefaultVersion() string {
	return DefaultMariaDBVersion
}

func (m *MariaDBProvider) GetImageName(version string) string {
	if version == "" {
		version = DefaultMariaDBVersion
	}
	return fmt.Sprintf("mariadb:%s", version)
}

func (m *MariaDBProvider) ResolveImage(version, _ string, customImage string) string {
	if img := strings.TrimSpace(customImage); img != "" {
		return img
	}
	return m.GetImageName(version)
}

func (m *MariaDBProvider) GetDefaultPort() int {
	return DefaultMariaDBPort
}

func (m *MariaDBProvider) GetContainerPort() string {
	return "3306"
}

func (m *MariaDBProvider) GetVolumePath(_ string) string {
	return "/var/lib/mysql"
}

func (m *MariaDBProvider) GetEnvVars(config *DBConfig) []string {
	return []string{
		fmt.Sprintf("MARIADB_ROOT_PASSWORD=%s", config.RootPassword),
		fmt.Sprintf("MARIADB_DATABASE=%s", config.DatabaseName),
	}
}

func (m *MariaDBProvider) GetCommand(_ *DBConfig) []string {
	return nil
}

func (m *MariaDBProvider) GetConnectionString(config *DBConfig) string {
	user, password := config.Auth("root")
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?multiStatements=true",
		user, password,
		config.Host,
		config.Port,
		config.DatabaseName,
	)
}

func (m *MariaDBProvider) GetEnvMap(config *DBConfig) map[string]string {
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

func (m *MariaDBProvider) Connect(config *DBConfig) (*sql.DB, error) {
	user, password := config.Auth("root")

	dsnCfg := mysql.NewConfig()
	dsnCfg.User = user
	dsnCfg.Passwd = password
	dsnCfg.Net = "tcp"
	dsnCfg.Addr = net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	dsnCfg.DBName = config.DatabaseName
	dsnCfg.AllowNativePasswords = true
	dsnCfg.MultiStatements = true
	dsnCfg.Timeout = 5 * time.Second

	connector, err := mysql.NewConnector(dsnCfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func (m *MariaDBProvider) Ping(ctx context.Context, cfg *DBConfig) error {
	db, err := m.Connect(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.PingContext(ctx)
}

func (m *MariaDBProvider) ShellCommand(cfg *DBConfig) []string {
	return []string{"mariadb", "-uroot", "-p" + cfg.RootPassword, cfg.DatabaseName}
}

func (m *MariaDBProvider) BackupCommand(cfg *DBConfig) []string {
	return []string{
		"mariadb-dump",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		cfg.DatabaseName,
	}
}

func (m *MariaDBProvider) RestoreCommand(cfg *DBConfig) []string {
	return []string{
		"mariadb",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		cfg.DatabaseName,
	}
}

func (m *MariaDBProvider) SnapshotDumpCommand(cfg *DBConfig) ([]string, string) {
	return []string{
		"mariadb-dump",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		"--single-transaction",
		"--routines",
		"--triggers",
		"--add-drop-table",
		cfg.DatabaseName,
	}, SnapshotFormatSQLGzip
}

func (m *MariaDBProvider) SnapshotRestoreCommand(cfg *DBConfig, _ string) []string {
	return m.RestoreCommand(cfg)
}

func (m *MariaDBProvider) CloneDumpCommand(cfg *DBConfig) []string {
	return []string{
		"mariadb-dump",
		"-u", "root",
		fmt.Sprintf("-p%s", cfg.RootPassword),
		"--single-transaction",
		cfg.DatabaseName,
	}
}

func (m *MariaDBProvider) CloneRestoreCommand(cfg *DBConfig) []string {
	return m.RestoreCommand(cfg)
}

func (m *MariaDBProvider) CreateUser(ctx context.Context, db *sql.DB, username, password, dbName string) error {
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

func (m *MariaDBProvider) ListTables(ctx context.Context, db *sql.DB, dbName string) ([]string, error) {
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
