package providers

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// respProvider shares the RESP2 protocol implementation between Redis and Valkey.
type respProvider struct {
	name           string
	imageRepo      string
	defaultVersion string
	defaultPort    int
	serverBin      string
	cliBin         string
	urlEnvKey      string
}

// RedisProvider implements Provider and KVProvider for Redis.
type RedisProvider struct {
	respProvider
}

// ValkeyProvider implements Provider and KVProvider for Valkey.
type ValkeyProvider struct {
	respProvider
}

func NewRedisProvider() *RedisProvider {
	return &RedisProvider{
		respProvider: respProvider{
			name:           "redis",
			imageRepo:      "redis",
			defaultVersion: DefaultRedisVersion,
			defaultPort:    DefaultRedisPort,
			serverBin:      "redis-server",
			cliBin:         "redis-cli",
			urlEnvKey:      "REDIS_URL",
		},
	}
}

func NewValkeyProvider() *ValkeyProvider {
	return &ValkeyProvider{
		respProvider: respProvider{
			name:           "valkey",
			imageRepo:      "valkey/valkey",
			defaultVersion: DefaultValkeyVersion,
			defaultPort:    DefaultValkeyPort,
			serverBin:      "valkey-server",
			cliBin:         "valkey-cli",
			urlEnvKey:      "VALKEY_URL",
		},
	}
}

func (r *respProvider) Name() string              { return r.name }
func (r *respProvider) Kind() EngineKind          { return EngineKV }
func (r *respProvider) GetDefaultVersion() string { return r.defaultVersion }
func (r *respProvider) GetDefaultPort() int       { return r.defaultPort }
func (r *respProvider) GetContainerPort() string  { return "6379" }
func (r *respProvider) GetVolumePath(_ string) string {
	return "/data"
}

func (r *respProvider) GetImageName(version string) string {
	if version == "" {
		version = r.defaultVersion
	}
	return fmt.Sprintf("%s:%s", r.imageRepo, version)
}

func (r *respProvider) ResolveImage(version, _ string, customImage string) string {
	if img := strings.TrimSpace(customImage); img != "" {
		return img
	}
	return r.GetImageName(version)
}

func (r *respProvider) GetEnvVars(_ *DBConfig) []string {
	return nil
}

func (r *respProvider) GetCommand(cfg *DBConfig) []string {
	cmd := []string{r.serverBin, "--appendonly", "yes"}
	if cfg != nil && cfg.RootPassword != "" {
		cmd = append(cmd, "--requirepass", cfg.RootPassword)
	}
	return cmd
}

func (r *respProvider) GetConnectionString(cfg *DBConfig) string {
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	u := url.URL{
		Scheme: "redis",
		Host:   net.JoinHostPort(host, strconv.Itoa(cfg.Port)),
		Path:   "/0",
	}
	if cfg.RootPassword != "" {
		u.User = url.UserPassword("", cfg.RootPassword)
	}
	return u.String()
}

func (r *respProvider) GetEnvMap(cfg *DBConfig) map[string]string {
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	connURL := r.GetConnectionString(cfg)
	env := map[string]string{
		"REDIS_URL":      connURL,
		"REDIS_HOST":     host,
		"REDIS_PORT":     strconv.Itoa(cfg.Port),
		"REDIS_PASSWORD": cfg.RootPassword,
		"DB_PROVIDER":    r.name,
	}
	if r.urlEnvKey != "REDIS_URL" {
		env[r.urlEnvKey] = connURL
	}
	return env
}

func (r *respProvider) ShellCommand(cfg *DBConfig) []string {
	if cfg.RootPassword != "" {
		return []string{r.cliBin, "-a", cfg.RootPassword, "--no-auth-warning"}
	}
	return []string{r.cliBin}
}

func (r *respProvider) Ping(ctx context.Context, cfg *DBConfig) error {
	client, err := dialRESP(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.close()

	val, err := client.do("PING")
	if err != nil {
		return err
	}
	if strings.ToUpper(val.asString()) != "PONG" {
		return fmt.Errorf("unexpected PING response: %q", val.asString())
	}
	return nil
}

// ScanKeys returns up to limit keys matching pattern along with their type and TTL.
func (r *respProvider) ScanKeys(ctx context.Context, cfg *DBConfig, pattern string, limit int) ([]KVKeyInfo, error) {
	if limit <= 0 {
		limit = 200
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = "*"
	}

	client, err := dialRESP(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer client.close()

	cursor := "0"
	var keys []string
	seen := make(map[string]bool)

	for len(keys) < limit {
		res, err := client.do("SCAN", cursor, "MATCH", pattern, "COUNT", "100")
		if err != nil {
			return nil, err
		}
		if len(res.array) < 2 {
			break
		}
		cursor = res.array[0].asString()
		for _, item := range res.array[1].array {
			k := item.asString()
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
				if len(keys) >= limit {
					break
				}
			}
		}
		if cursor == "0" {
			break
		}
	}

	out := make([]KVKeyInfo, 0, len(keys))
	for _, k := range keys {
		tVal, _ := client.do("TYPE", k)
		ttlVal, _ := client.do("TTL", k)
		out = append(out, KVKeyInfo{
			Key:  k,
			Type: tVal.asString(),
			TTL:  ttlVal.intVal,
		})
	}
	return out, nil
}

// GetKeyDetail fetches the type, TTL, and formatted value preview for a single key.
func (r *respProvider) GetKeyDetail(ctx context.Context, cfg *DBConfig, key string) (*KVKeyDetail, error) {
	client, err := dialRESP(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer client.close()

	typeRes, err := client.do("TYPE", key)
	if err != nil {
		return nil, err
	}
	typ := typeRes.asString()
	ttlRes, _ := client.do("TTL", key)

	detail := &KVKeyDetail{
		Key:  key,
		Type: typ,
		TTL:  ttlRes.intVal,
	}

	switch typ {
	case "string":
		v, err := client.do("GET", key)
		if err != nil {
			return nil, err
		}
		s := v.asString()
		detail.Size = len(s)
		detail.Value = s
	case "hash":
		v, err := client.do("HGETALL", key)
		if err != nil {
			return nil, err
		}
		m := make(map[string]string, len(v.array)/2)
		for i := 0; i+1 < len(v.array); i += 2 {
			m[v.array[i].asString()] = v.array[i+1].asString()
		}
		detail.Size = len(m)
		if raw, err := json.MarshalIndent(m, "", "  "); err == nil {
			detail.Value = string(raw)
		}
	case "list":
		v, err := client.do("LRANGE", key, "0", "99")
		if err != nil {
			return nil, err
		}
		items := make([]string, 0, len(v.array))
		for _, el := range v.array {
			items = append(items, el.asString())
		}
		detail.Size = len(items)
		if raw, err := json.MarshalIndent(items, "", "  "); err == nil {
			detail.Value = string(raw)
		}
	case "set":
		v, err := client.do("SMEMBERS", key)
		if err != nil {
			return nil, err
		}
		items := make([]string, 0, len(v.array))
		for _, el := range v.array {
			items = append(items, el.asString())
		}
		detail.Size = len(items)
		if raw, err := json.MarshalIndent(items, "", "  "); err == nil {
			detail.Value = string(raw)
		}
	case "zset":
		v, err := client.do("ZRANGE", key, "0", "99", "WITHSCORES")
		if err != nil {
			return nil, err
		}
		type zEntry struct {
			Member string `json:"member"`
			Score  string `json:"score"`
		}
		entries := make([]zEntry, 0, len(v.array)/2)
		for i := 0; i+1 < len(v.array); i += 2 {
			entries = append(entries, zEntry{Member: v.array[i].asString(), Score: v.array[i+1].asString()})
		}
		detail.Size = len(entries)
		if raw, err := json.MarshalIndent(entries, "", "  "); err == nil {
			detail.Value = string(raw)
		}
	case "none":
		detail.Value = "(key does not exist)"
	default:
		detail.Value = fmt.Sprintf("(preview not supported for type %q)", typ)
	}
	return detail, nil
}

// ExecKV parses a Redis command string and executes it over RESP2.
func (r *respProvider) ExecKV(ctx context.Context, cfg *DBConfig, cmdLine string) (string, time.Duration, error) {
	args, err := splitCommandArgs(cmdLine)
	if err != nil {
		return "", 0, err
	}
	if len(args) == 0 {
		return "", 0, fmt.Errorf("empty command")
	}

	client, err := dialRESP(ctx, cfg)
	if err != nil {
		return "", 0, err
	}
	defer client.close()

	start := time.Now()
	val, err := client.do(args...)
	dur := time.Since(start)
	if err != nil {
		return "", dur, err
	}
	return val.format(0), dur, nil
}

type kvDumpEntry struct {
	Key   string `json:"key"`
	TTLms int64  `json:"ttl_ms"`
	Dump  string `json:"dump_b64"`
}

// DumpKV takes an online logical dump using SCAN + PTTL + DUMP, written as gzipped JSON.
func (r *respProvider) DumpKV(ctx context.Context, cfg *DBConfig, w io.Writer) error {
	client, err := dialRESP(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.close()

	cursor := "0"
	seen := make(map[string]bool)
	var entries []kvDumpEntry

	for {
		res, err := client.do("SCAN", cursor, "COUNT", "200")
		if err != nil {
			return fmt.Errorf("scanning keys: %w", err)
		}
		if len(res.array) < 2 {
			break
		}
		cursor = res.array[0].asString()
		for _, item := range res.array[1].array {
			k := item.asString()
			if seen[k] {
				continue
			}
			seen[k] = true

			dumpRes, err := client.do("DUMP", k)
			if err != nil || dumpRes.isNull {
				continue
			}
			pttlRes, _ := client.do("PTTL", k)
			ttl := pttlRes.intVal
			if ttl < 0 {
				ttl = 0
			}
			entries = append(entries, kvDumpEntry{
				Key:   k,
				TTLms: ttl,
				Dump:  base64.StdEncoding.EncodeToString(dumpRes.bulk),
			})
		}
		if cursor == "0" {
			break
		}
	}

	gz := gzip.NewWriter(w)
	if err := json.NewEncoder(gz).Encode(entries); err != nil {
		gz.Close()
		return fmt.Errorf("encoding KV dump: %w", err)
	}
	return gz.Close()
}

// RestoreKV flushes the active DB and restores all keys from a gzipped JSON KV dump.
func (r *respProvider) RestoreKV(ctx context.Context, cfg *DBConfig, rdr io.Reader) error {
	gz, err := gzip.NewReader(rdr)
	if err != nil {
		return fmt.Errorf("reading KV dump: %w", err)
	}
	defer gz.Close()

	var entries []kvDumpEntry
	if err := json.NewDecoder(gz).Decode(&entries); err != nil {
		return fmt.Errorf("decoding KV dump: %w", err)
	}

	client, err := dialRESP(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.close()

	if _, err := client.do("FLUSHDB"); err != nil {
		return fmt.Errorf("flushing DB before restore: %w", err)
	}
	for _, e := range entries {
		payload, err := base64.StdEncoding.DecodeString(e.Dump)
		if err != nil {
			return fmt.Errorf("decoding key %q: %w", e.Key, err)
		}
		if _, err := client.doRaw([][]byte{
			[]byte("RESTORE"),
			[]byte(e.Key),
			[]byte(strconv.FormatInt(e.TTLms, 10)),
			payload,
			[]byte("REPLACE"),
		}); err != nil {
			return fmt.Errorf("restoring key %q: %w", e.Key, err)
		}
	}
	return nil
}

// --- Minimal stdlib RESP2 client ---

type respClient struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

type respValue struct {
	kind   byte // '+', '-', ':', '$', '*'
	str    string
	bulk   []byte
	intVal int64
	array  []respValue
	isNull bool
}

func (v respValue) asString() string {
	switch v.kind {
	case '$':
		if v.isNull {
			return "(nil)"
		}
		return string(v.bulk)
	case ':':
		return strconv.FormatInt(v.intVal, 10)
	default:
		return v.str
	}
}

func (v respValue) format(indent int) string {
	pad := strings.Repeat("  ", indent)
	switch v.kind {
	case '+':
		return pad + v.str
	case '-':
		return pad + "(error) " + v.str
	case ':':
		return pad + "(integer) " + strconv.FormatInt(v.intVal, 10)
	case '$':
		if v.isNull {
			return pad + "(nil)"
		}
		return pad + string(v.bulk)
	case '*':
		if v.isNull {
			return pad + "(nil)"
		}
		if len(v.array) == 0 {
			return pad + "(empty array)"
		}
		var b strings.Builder
		for i, el := range v.array {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(fmt.Sprintf("%s%d) %s", pad, i+1, strings.TrimSpace(el.format(indent+1))))
		}
		return b.String()
	default:
		return pad + v.str
	}
}

func dialRESP(ctx context.Context, cfg *DBConfig) (*respClient, error) {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	dialer := &net.Dialer{Timeout: time.Duration(connectTimeoutSeconds) * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}

	c := &respClient{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
	if cfg.RootPassword != "" {
		if _, err := c.do("AUTH", cfg.RootPassword); err != nil {
			conn.Close()
			return nil, fmt.Errorf("RESP auth failed: %w", err)
		}
	}
	return c, nil
}

func (c *respClient) close() {
	_ = c.conn.Close()
}

func (c *respClient) do(args ...string) (respValue, error) {
	raw := make([][]byte, len(args))
	for i, a := range args {
		raw[i] = []byte(a)
	}
	return c.doRaw(raw)
}

func (c *respClient) doRaw(args [][]byte) (respValue, error) {
	fmt.Fprintf(c.w, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(c.w, "$%d\r\n", len(a))
		_, _ = c.w.Write(a)
		_, _ = c.w.WriteString("\r\n")
	}
	if err := c.w.Flush(); err != nil {
		return respValue{}, err
	}
	val, err := readRESPValue(c.r)
	if err != nil {
		return respValue{}, err
	}
	if val.kind == '-' {
		return val, fmt.Errorf("%s", val.str)
	}
	return val, nil
}

func readRESPValue(r *bufio.Reader) (respValue, error) {
	prefix, err := r.ReadByte()
	if err != nil {
		return respValue{}, err
	}
	line, err := readRESPLine(r)
	if err != nil {
		return respValue{}, err
	}

	switch prefix {
	case '+', '-':
		return respValue{kind: prefix, str: line}, nil
	case ':':
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return respValue{}, err
		}
		return respValue{kind: ':', intVal: n}, nil
	case '$':
		n, err := strconv.Atoi(line)
		if err != nil {
			return respValue{}, err
		}
		if n < 0 {
			return respValue{kind: '$', isNull: true}, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return respValue{}, err
		}
		return respValue{kind: '$', bulk: buf[:n]}, nil
	case '*':
		n, err := strconv.Atoi(line)
		if err != nil {
			return respValue{}, err
		}
		if n < 0 {
			return respValue{kind: '*', isNull: true}, nil
		}
		arr := make([]respValue, 0, n)
		for i := 0; i < n; i++ {
			el, err := readRESPValue(r)
			if err != nil {
				return respValue{}, err
			}
			arr = append(arr, el)
		}
		return respValue{kind: '*', array: arr}, nil
	default:
		return respValue{}, fmt.Errorf("unexpected RESP prefix %q", prefix)
	}
}

func readRESPLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

// splitCommandArgs splits a Redis console command line respecting single/double quotes.
func splitCommandArgs(input string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	hasToken := false

	for i := 0; i < len(input); i++ {
		ch := input[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(ch)
			}
		case inDouble:
			if ch == '\\' && i+1 < len(input) {
				i++
				cur.WriteByte(input[i])
			} else if ch == '"' {
				inDouble = false
			} else {
				cur.WriteByte(ch)
			}
		case ch == '\'':
			inSingle = true
			hasToken = true
		case ch == '"':
			inDouble = true
			hasToken = true
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if hasToken {
				args = append(args, cur.String())
				cur.Reset()
				hasToken = false
			}
		default:
			cur.WriteByte(ch)
			hasToken = true
		}
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote in command")
	}
	if hasToken {
		args = append(args, cur.String())
	}
	return args, nil
}
