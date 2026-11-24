package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/iqbalmu/xmdb/internal/providers"
	"github.com/iqbalmu/xmdb/internal/state"
)

const (
	EnvBlockStartPrefix = "# xmdb:start"
	EnvBlockStartLine   = "# xmdb:start (managed by xmdb — do not edit manually)"
	EnvBlockEndLine     = "# xmdb:end"
)

// ExpandEnvTemplate expands {{url}}, {{host}}, {{port}}, {{user}}, {{password}},
// {{database}}, and {{provider}} placeholders using db and prov.
func ExpandEnvTemplate(tmpl string, prov providers.Provider, db *state.Database) string {
	cfg := &providers.DBConfig{
		Host:         "localhost",
		Port:         db.Port,
		RootPassword: db.RootPassword,
		DatabaseName: db.DatabaseName,
		User:         db.User,
		UserPassword: db.UserPassword,
	}
	replacer := strings.NewReplacer(
		"{{url}}", prov.GetConnectionString(cfg),
		"{{host}}", "localhost",
		"{{port}}", strconv.Itoa(db.Port),
		"{{user}}", db.GetUsername(),
		"{{password}}", db.GetPassword(),
		"{{database}}", db.DatabaseName,
		"{{provider}}", db.Provider,
	)
	return replacer.Replace(tmpl)
}

// BuildDatabaseEnv computes the env vars for one manifest database entry.
// If spec.Env is empty, falls back to the provider's default env map,
// prefixing keys with KEY_ when multiDB is true to avoid collisions.
func BuildDatabaseEnv(key string, spec DatabaseSpec, prov providers.Provider, db *state.Database, multiDB bool) map[string]string {
	out := make(map[string]string)
	if len(spec.Env) > 0 {
		for k, tmpl := range spec.Env {
			out[k] = ExpandEnvTemplate(tmpl, prov, db)
		}
		return out
	}

	cfg := &providers.DBConfig{
		Host:         "localhost",
		Port:         db.Port,
		RootPassword: db.RootPassword,
		DatabaseName: db.DatabaseName,
		User:         db.User,
		UserPassword: db.UserPassword,
	}
	defaults := prov.GetEnvMap(cfg)
	prefix := ""
	if multiDB {
		prefix = envKeyPrefix(key) + "_"
	}
	for k, v := range defaults {
		out[prefix+k] = v
	}
	return out
}

func envKeyPrefix(key string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(key)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	res := strings.Trim(b.String(), "_")
	if res == "" {
		return "DB"
	}
	return res
}

// FormatEnvLines returns sorted KEY=VALUE lines suitable for stdout or .env files.
func FormatEnvLines(vars map[string]string) []string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", k, formatEnvValue(vars[k])))
	}
	return lines
}

func formatEnvValue(val string) string {
	if val == "" {
		return ""
	}
	needsQuote := strings.ContainsAny(val, " \t\n\r#\"'")
	if !needsQuote {
		return val
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`).Replace(val)
	return `"` + escaped + `"`
}

// RenderManagedBlock renders the complete # xmdb:start ... # xmdb:end block.
func RenderManagedBlock(vars map[string]string) string {
	lines := make([]string, 0, len(vars)+2)
	lines = append(lines, EnvBlockStartLine)
	lines = append(lines, FormatEnvLines(vars)...)
	lines = append(lines, EnvBlockEndLine)
	return strings.Join(lines, "\n")
}

// UpsertManagedBlock replaces an existing # xmdb:start ... # xmdb:end section
// in existing or appends a new block without altering any other lines.
func UpsertManagedBlock(existing string, vars map[string]string) (string, error) {
	block := RenderManagedBlock(vars)
	if existing == "" {
		return block + "\n", nil
	}

	normalized := strings.ReplaceAll(existing, "\r\n", "\n")
	rawLines := strings.Split(normalized, "\n")
	// Preserve trailing newline distinction by trimming final empty split element.
	hasTrailingNewline := strings.HasSuffix(normalized, "\n")
	if hasTrailingNewline && len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	startIdx := -1
	endIdx := -1
	for i, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if startIdx == -1 && strings.HasPrefix(trimmed, EnvBlockStartPrefix) {
			startIdx = i
			continue
		}
		if startIdx != -1 && trimmed == EnvBlockEndLine {
			endIdx = i
			break
		}
	}

	if startIdx != -1 && endIdx == -1 {
		return "", fmt.Errorf("found '%s' without matching '%s' marker", EnvBlockStartPrefix, EnvBlockEndLine)
	}

	blockLines := strings.Split(block, "\n")
	var outLines []string
	if startIdx != -1 {
		outLines = append(outLines, rawLines[:startIdx]...)
		outLines = append(outLines, blockLines...)
		outLines = append(outLines, rawLines[endIdx+1:]...)
	} else {
		outLines = append(outLines, rawLines...)
		if len(outLines) > 0 && strings.TrimSpace(outLines[len(outLines)-1]) != "" {
			outLines = append(outLines, "")
		}
		outLines = append(outLines, blockLines...)
	}

	return strings.Join(outLines, "\n") + "\n", nil
}

// SyncEnvFile safely upserts the managed xmdb block into path atomically.
func SyncEnvFile(path string, vars map[string]string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving env file path: %w", err)
	}

	var existing string
	perm := os.FileMode(0o600)
	if info, err := os.Stat(absPath); err == nil {
		perm = info.Mode().Perm()
		data, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", absPath, err)
		}
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("checking %s: %w", absPath, err)
	}

	updated, err := UpsertManagedBlock(existing, vars)
	if err != nil {
		return "", fmt.Errorf("updating %s: %w", absPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("creating directory for %s: %w", absPath, err)
	}
	tmpPath := absPath + ".xmdb.tmp"
	if err := os.WriteFile(tmpPath, []byte(updated), perm); err != nil {
		return "", fmt.Errorf("writing %s: %w", absPath, err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("replacing %s: %w", absPath, err)
	}
	return absPath, nil
}
