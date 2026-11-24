package providers

import (
	"reflect"
	"strings"
	"testing"
)

func TestKnownVersionsNewestFirst(t *testing.T) {
	wantPG := []string{"18", "17", "16", "15", "14"}
	if got := KnownVersions("postgres"); !reflect.DeepEqual(got, wantPG) {
		t.Errorf("postgres versions = %v, want %v (newest first)", got, wantPG)
	}

	wantMySQL := []string{"9.0", "8.4", "8.0"}
	if got := KnownVersions("mysql"); !reflect.DeepEqual(got, wantMySQL) {
		t.Errorf("mysql versions = %v, want %v (newest first)", got, wantMySQL)
	}

	// The configured defaults must stay selectable in the picker.
	for _, v := range KnownVersions("postgres") {
		if v == DefaultPostgresVersion {
			return
		}
	}
	t.Errorf("postgres default %q missing from KnownVersions", DefaultPostgresVersion)
}

func TestGetVolumePath(t *testing.T) {
	if got := NewMySQLProvider().GetVolumePath("8.0"); got != "/var/lib/mysql" {
		t.Errorf("mysql volume path = %q, want /var/lib/mysql", got)
	}

	pg := NewPostgresProvider()
	for _, v := range []string{"13", "14", "15", "16", "17", ""} {
		if got := pg.GetVolumePath(v); got != "/var/lib/postgresql/data" {
			t.Errorf("postgres %q volume path = %q, want the legacy data path", v, got)
		}
	}
	for _, v := range []string{"18", "18-alpine", "19"} {
		if got := pg.GetVolumePath(v); got != "/var/lib/postgresql" {
			t.Errorf("postgres %q volume path = %q, want /var/lib/postgresql", v, got)
		}
	}
}

func TestGetImageName(t *testing.T) {
	if got := NewMySQLProvider().GetImageName(""); got != "mysql:8.0" {
		t.Errorf("mysql default image = %q, want mysql:8.0", got)
	}
	if got := NewPostgresProvider().GetImageName(""); got != "postgres:16" {
		t.Errorf("postgres default image = %q, want postgres:16", got)
	}
	if got := NewMySQLProvider().GetImageName("9.1"); got != "mysql:9.1" {
		t.Errorf("mysql custom image = %q, want mysql:9.1", got)
	}
}

func TestGetDefaultPort(t *testing.T) {
	if got := NewMySQLProvider().GetDefaultPort(); got != 3306 {
		t.Errorf("mysql default port = %d, want 3306", got)
	}
	if got := NewPostgresProvider().GetDefaultPort(); got != 5432 {
		t.Errorf("postgres default port = %d, want 5432", got)
	}
}

func TestGetEnvVars(t *testing.T) {
	cfg := &DBConfig{RootPassword: "s3cret", DatabaseName: "app"}

	mysqlVars := strings.Join(NewMySQLProvider().GetEnvVars(cfg), " ")
	if !strings.Contains(mysqlVars, "MYSQL_ROOT_PASSWORD=s3cret") ||
		!strings.Contains(mysqlVars, "MYSQL_DATABASE=app") {
		t.Errorf("mysql env vars missing expected entries: %s", mysqlVars)
	}

	pgVars := strings.Join(NewPostgresProvider().GetEnvVars(cfg), " ")
	if !strings.Contains(pgVars, "POSTGRES_PASSWORD=s3cret") ||
		!strings.Contains(pgVars, "POSTGRES_DB=app") {
		t.Errorf("postgres env vars missing expected entries: %s", pgVars)
	}
}

func TestGetConnectionStringAuth(t *testing.T) {
	super := &DBConfig{Host: "localhost", Port: 5432, RootPassword: "rootpw", DatabaseName: "app"}

	user, pass := super.Auth("postgres")
	if user != "postgres" || pass != "rootpw" {
		t.Errorf("Auth on superuser config = %s/%s, want postgres/rootpw", user, pass)
	}

	withUser := &DBConfig{Host: "localhost", Port: 5432, RootPassword: "rootpw",
		DatabaseName: "app", User: "dev_user", UserPassword: "devpw"}
	user, pass = withUser.Auth("postgres")
	if user != "dev_user" || pass != "devpw" {
		t.Errorf("Auth with user = %s/%s, want dev_user/devpw", user, pass)
	}

	// A user without a dedicated password falls back to the superuser password.
	noPass := &DBConfig{RootPassword: "rootpw", User: "dev_user"}
	user, pass = noPass.Auth("postgres")
	if user != "dev_user" || pass != "rootpw" {
		t.Errorf("Auth user without password = %s/%s, want dev_user/rootpw", user, pass)
	}
}

func TestMySQLGetConnectionString(t *testing.T) {
	cfg := &DBConfig{Host: "localhost", Port: 3306, RootPassword: "rootpw",
		DatabaseName: "app", User: "dev_user", UserPassword: "devpw"}

	want := "dev_user:devpw@tcp(localhost:3306)/app?multiStatements=true"
	if got := NewMySQLProvider().GetConnectionString(cfg); got != want {
		t.Errorf("mysql conn string = %q, want %q", got, want)
	}
}

func TestPostgresGetConnectionString(t *testing.T) {
	cfg := &DBConfig{Host: "localhost", Port: 5432, RootPassword: "rootpw",
		DatabaseName: "app", User: "dev_user", UserPassword: "devpw"}

	got := NewPostgresProvider().GetConnectionString(cfg)
	for _, want := range []string{"postgres://dev_user:devpw@", "localhost:5432", "/app", "sslmode=disable"} {
		if !strings.Contains(got, want) {
			t.Errorf("postgres conn string %q missing %q", got, want)
		}
	}

	// Special characters must be URL-escaped.
	weird := &DBConfig{Host: "localhost", Port: 5432, RootPassword: "rootpw",
		DatabaseName: "app", User: "dev@user", UserPassword: "p@ss:word"}
	got = NewPostgresProvider().GetConnectionString(weird)
	if strings.Contains(got, "dev@user") || strings.Contains(got, "p@ss:word") {
		t.Errorf("postgres conn string not URL-escaped: %q", got)
	}
}

func TestProviderCapabilitiesAndCommands(t *testing.T) {
	cfg := &DBConfig{
		Host:         "localhost",
		Port:         5432,
		RootPassword: "secret",
		DatabaseName: "appdb",
		User:         "dev_user",
		UserPassword: "devpw",
	}

	for _, name := range []string{"mysql", "postgres"} {
		prov, err := For(name)
		if err != nil {
			t.Fatalf("For(%q) failed: %v", name, err)
		}
		if prov.Name() != name {
			t.Errorf("Name() = %q, want %q", prov.Name(), name)
		}
		if prov.Kind() != EngineSQL {
			t.Errorf("%s Kind() = %q, want %q", name, prov.Kind(), EngineSQL)
		}
		if _, ok := AsSQL(prov); !ok {
			t.Errorf("%s should implement SQLProvider", name)
		}
		snapProv, ok := AsSnapshot(prov)
		if !ok {
			t.Fatalf("%s should implement SnapshotProvider", name)
		}
		if img := prov.ResolveImage("16", "", "custom/repo:1.0"); img != "custom/repo:1.0" {
			t.Errorf("%s ResolveImage custom = %q, want custom/repo:1.0", name, img)
		}
		if len(prov.ShellCommand(cfg)) == 0 {
			t.Errorf("%s ShellCommand returned empty", name)
		}
		envMap := prov.GetEnvMap(cfg)
		if envMap["DATABASE_URL"] == "" || envMap["DB_HOST"] != "localhost" || envMap["DB_NAME"] != "appdb" {
			t.Errorf("%s unexpected GetEnvMap: %+v", name, envMap)
		}
		dumpCmd, format := snapProv.SnapshotDumpCommand(cfg)
		if len(dumpCmd) == 0 || format == "" {
			t.Errorf("%s SnapshotDumpCommand invalid: %v, %q", name, dumpCmd, format)
		}
		if len(snapProv.SnapshotRestoreCommand(cfg, format)) == 0 {
			t.Errorf("%s SnapshotRestoreCommand returned empty", name)
		}
	}
}
