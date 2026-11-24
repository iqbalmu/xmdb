package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iqbalmu/xmdb/internal/providers"
	"gopkg.in/yaml.v3"
)

const (
	DefaultManifestFile = "xmdb.yaml"
	CurrentVersion      = 1
)

// Manifest defines a declarative set of local databases for a project repo.
type Manifest struct {
	Version   int                     `yaml:"version"`
	Project   string                  `yaml:"project"`
	EnvFile   string                  `yaml:"env_file,omitempty"`
	Databases map[string]DatabaseSpec `yaml:"databases"`
}

// DatabaseSpec describes a single managed database within a Manifest.
type DatabaseSpec struct {
	Name     string            `yaml:"name,omitempty"`
	Provider string            `yaml:"provider"`
	Version  string            `yaml:"version,omitempty"`
	Preset   string            `yaml:"preset,omitempty"`
	Image    string            `yaml:"image,omitempty"`
	Port     int               `yaml:"port,omitempty"`
	Volume   *bool             `yaml:"volume,omitempty"`
	Seed     string            `yaml:"seed,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
}

// VolumeEnabled reports whether a named volume should be attached (default true).
func (d DatabaseSpec) VolumeEnabled() bool {
	if d.Volume == nil {
		return true
	}
	return *d.Volume
}

// Load reads, parses, and validates a manifest file from path.
func Load(path string) (*Manifest, string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", fmt.Errorf("resolving manifest path: %w", err)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("manifest file not found: %s (create one with: xmdb init)", absPath)
		}
		return nil, "", fmt.Errorf("reading manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, "", fmt.Errorf("parsing %s: %w", absPath, err)
	}
	if err := m.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid manifest %s: %w", absPath, err)
	}
	return &m, absPath, nil
}

// FindManifest locates xmdb.yaml starting from startDir and walking up parent
// directories until the filesystem root.
func FindManifest(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, DefaultManifestFile)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in %s or parent directories (create one with: xmdb init)", DefaultManifestFile, startDir)
		}
		dir = parent
	}
}

// Save writes m as YAML to path.
func (m *Manifest) Save(path string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	header := "# xmdb project manifest — run `xmdb up` to start, `xmdb down` to stop\n"
	content := append([]byte(header), data...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// Validate checks manifest structure, provider names, and uniqueness of instance names.
func (m *Manifest) Validate() error {
	if m.Version == 0 {
		m.Version = CurrentVersion
	}
	if m.Version != CurrentVersion {
		return fmt.Errorf("unsupported manifest version %d (expected %d)", m.Version, CurrentVersion)
	}
	m.Project = strings.TrimSpace(m.Project)
	if m.Project == "" {
		return fmt.Errorf("project name is required")
	}
	if len(m.Databases) == 0 {
		return fmt.Errorf("at least one database entry is required under 'databases'")
	}

	seenNames := make(map[string]string, len(m.Databases))
	for _, key := range m.SortedKeys() {
		spec := m.Databases[key]
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("database key cannot be empty")
		}
		prov, err := providers.For(spec.Provider)
		if err != nil {
			return fmt.Errorf("database '%s': %w", key, err)
		}
		if err := providers.ValidatePreset(prov.Name(), spec.Preset); err != nil {
			return fmt.Errorf("database '%s': %w", key, err)
		}
		if spec.Port < 0 || spec.Port > 65535 {
			return fmt.Errorf("database '%s': invalid port %d", key, spec.Port)
		}
		instName := m.ResolveInstanceName(key)
		if prevKey, dup := seenNames[instName]; dup {
			return fmt.Errorf("databases '%s' and '%s' resolve to the same instance name '%s'", prevKey, key, instName)
		}
		seenNames[instName] = key
	}
	return nil
}

// SortedKeys returns the database keys in deterministic alphabetical order.
func (m *Manifest) SortedKeys() []string {
	keys := make([]string, 0, len(m.Databases))
	for k := range m.Databases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ResolveInstanceName returns the global xmdb database name for key.
// Defaults to "<project>_<key>" unless DatabaseSpec.Name is explicitly set.
func (m *Manifest) ResolveInstanceName(key string) string {
	spec := m.Databases[key]
	if explicit := strings.TrimSpace(spec.Name); explicit != "" {
		return explicit
	}
	return fmt.Sprintf("%s_%s", sanitizeNameSegment(m.Project), sanitizeNameSegment(key))
}

func sanitizeNameSegment(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_.-")
	if out == "" {
		return "db"
	}
	return out
}

// StarterManifest builds a ready-to-edit default manifest for projectName.
func StarterManifest(projectName string) *Manifest {
	if strings.TrimSpace(projectName) == "" {
		projectName = "myapp"
	}
	return &Manifest{
		Version: CurrentVersion,
		Project: sanitizeNameSegment(projectName),
		EnvFile: ".env",
		Databases: map[string]DatabaseSpec{
			"db": {
				Provider: "postgres",
				Version:  providers.DefaultPostgresVersion,
				Port:     providers.DefaultPostgresPort,
				Env: map[string]string{
					"DATABASE_URL": "{{url}}",
					"DB_HOST":      "{{host}}",
					"DB_PORT":      "{{port}}",
					"DB_USER":      "{{user}}",
					"DB_PASSWORD":  "{{password}}",
					"DB_NAME":      "{{database}}",
				},
			},
		},
	}
}
