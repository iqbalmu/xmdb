package providers

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MongoDBProvider implements Provider, SnapshotProvider, and DocumentProvider for MongoDB.
type MongoDBProvider struct{}

func NewMongoDBProvider() *MongoDBProvider {
	return &MongoDBProvider{}
}

func (m *MongoDBProvider) Name() string              { return "mongodb" }
func (m *MongoDBProvider) Kind() EngineKind          { return EngineDocument }
func (m *MongoDBProvider) GetDefaultVersion() string { return DefaultMongoDBVersion }
func (m *MongoDBProvider) GetDefaultPort() int       { return DefaultMongoDBPort }
func (m *MongoDBProvider) GetContainerPort() string  { return "27017" }
func (m *MongoDBProvider) GetVolumePath(_ string) string {
	return "/data/db"
}

func (m *MongoDBProvider) GetImageName(version string) string {
	if version == "" {
		version = DefaultMongoDBVersion
	}
	return fmt.Sprintf("mongo:%s", version)
}

func (m *MongoDBProvider) ResolveImage(version, _ string, customImage string) string {
	if img := strings.TrimSpace(customImage); img != "" {
		return img
	}
	return m.GetImageName(version)
}

func (m *MongoDBProvider) GetEnvVars(cfg *DBConfig) []string {
	return []string{
		"MONGO_INITDB_ROOT_USERNAME=root",
		fmt.Sprintf("MONGO_INITDB_ROOT_PASSWORD=%s", cfg.RootPassword),
		fmt.Sprintf("MONGO_INITDB_DATABASE=%s", cfg.DatabaseName),
	}
}

func (m *MongoDBProvider) GetCommand(_ *DBConfig) []string {
	return nil
}

func (m *MongoDBProvider) GetConnectionString(cfg *DBConfig) string {
	return mongoURI(cfg, cfg.Host, cfg.Port)
}

func mongoURI(cfg *DBConfig, host string, port int) string {
	if host == "" {
		host = "localhost"
	}
	user, pass := cfg.Auth("root")
	u := url.URL{
		Scheme: "mongodb",
		User:   url.UserPassword(user, pass),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + cfg.DatabaseName,
	}
	q := u.Query()
	q.Set("authSource", "admin")
	u.RawQuery = q.Encode()
	return u.String()
}

func mongoContainerURI(cfg *DBConfig) string {
	return mongoURI(cfg, "127.0.0.1", DefaultMongoDBPort)
}

func (m *MongoDBProvider) GetEnvMap(cfg *DBConfig) map[string]string {
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	user, pass := cfg.Auth("root")
	return map[string]string{
		"MONGODB_URI":    m.GetConnectionString(cfg),
		"MONGO_HOST":     host,
		"MONGO_PORT":     strconv.Itoa(cfg.Port),
		"MONGO_USER":     user,
		"MONGO_PASSWORD": pass,
		"MONGO_DB":       cfg.DatabaseName,
		"DB_PROVIDER":    m.Name(),
	}
}

// Ping performs a lightweight MongoDB OP_MSG { ping: 1, $db: "admin" } wire
// protocol handshake to confirm the server is accepting client requests.
func (m *MongoDBProvider) Ping(ctx context.Context, cfg *DBConfig) error {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	dialer := &net.Dialer{Timeout: time.Duration(connectTimeoutSeconds) * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}

	req := buildMongoPingOPMsg()
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("sending mongo ping: %w", err)
	}

	var hdr [16]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return fmt.Errorf("reading mongo ping response: %w", err)
	}
	msgLen := int(binary.LittleEndian.Uint32(hdr[0:4]))
	if msgLen < 16 || msgLen > 1024*1024 {
		return fmt.Errorf("unexpected mongo reply length %d", msgLen)
	}
	body := make([]byte, msgLen-16)
	if _, err := io.ReadFull(conn, body); err != nil {
		return fmt.Errorf("reading mongo ping body: %w", err)
	}
	return nil
}

// buildMongoPingOPMsg builds a valid MongoDB OP_MSG packet carrying
// BSON { "ping": int32(1), "$db": "admin" }.
func buildMongoPingOPMsg() []byte {
	// BSON body:
	//   int32("ping") = 1   -> 0x10 "ping\0" int32(1)
	//   string("$db")="admin" -> 0x02 "$db\0" int32(6) "admin\0"
	//   terminator 0x00
	var doc []byte
	doc = append(doc, 0, 0, 0, 0) // length placeholder
	doc = append(doc, 0x10)
	doc = append(doc, []byte("ping\x00")...)
	doc = binary.LittleEndian.AppendUint32(doc, 1)
	doc = append(doc, 0x02)
	doc = append(doc, []byte("$db\x00")...)
	doc = binary.LittleEndian.AppendUint32(doc, 6)
	doc = append(doc, []byte("admin\x00")...)
	doc = append(doc, 0x00)
	binary.LittleEndian.PutUint32(doc[0:4], uint32(len(doc)))

	// OP_MSG header (16 bytes) + flagBits (4 bytes) + section kind 0 (1 byte) + BSON
	totalLen := 16 + 4 + 1 + len(doc)
	pkt := make([]byte, 0, totalLen)
	pkt = binary.LittleEndian.AppendUint32(pkt, uint32(totalLen))
	pkt = binary.LittleEndian.AppendUint32(pkt, 1)    // requestID
	pkt = binary.LittleEndian.AppendUint32(pkt, 0)    // responseTo
	pkt = binary.LittleEndian.AppendUint32(pkt, 2013) // OP_MSG
	pkt = binary.LittleEndian.AppendUint32(pkt, 0)    // flagBits
	pkt = append(pkt, 0x00)                           // payload type 0 (body)
	pkt = append(pkt, doc...)
	return pkt
}

func (m *MongoDBProvider) ShellCommand(cfg *DBConfig) []string {
	return []string{"mongosh", mongoContainerURI(cfg)}
}

func (m *MongoDBProvider) BackupCommand(cfg *DBConfig) []string {
	return []string{"mongodump", "--uri=" + mongoContainerURI(cfg), "--archive", "--gzip"}
}

func (m *MongoDBProvider) RestoreCommand(cfg *DBConfig) []string {
	return []string{"mongorestore", "--uri=" + mongoContainerURI(cfg), "--archive", "--gzip", "--drop"}
}

func (m *MongoDBProvider) SnapshotDumpCommand(cfg *DBConfig) ([]string, string) {
	return m.BackupCommand(cfg), SnapshotFormatMongoArchive
}

func (m *MongoDBProvider) SnapshotRestoreCommand(cfg *DBConfig, _ string) []string {
	return m.RestoreCommand(cfg)
}

func (m *MongoDBProvider) CloneDumpCommand(cfg *DBConfig) []string {
	return m.BackupCommand(cfg)
}

func (m *MongoDBProvider) CloneRestoreCommand(cfg *DBConfig) []string {
	return m.RestoreCommand(cfg)
}

func (m *MongoDBProvider) ListCollectionsCommand(cfg *DBConfig) []string {
	js := `JSON.stringify(db.getCollectionNames().map(n => ({name: n, count: db.getCollection(n).estimatedDocumentCount()})))`
	return []string{"mongosh", "--quiet", mongoContainerURI(cfg), "--eval", js}
}

func (m *MongoDBProvider) SampleCollectionCommand(cfg *DBConfig, collection string, limit, skip int, filterJSON string) []string {
	if limit <= 0 {
		limit = 25
	}
	if skip < 0 {
		skip = 0
	}
	filter := strings.TrimSpace(filterJSON)
	if filter == "" {
		filter = "{}"
	}
	js := fmt.Sprintf(
		`const col = db.getCollection(%q); const f = %s; const total = col.countDocuments(f); const docs = col.find(f).skip(%d).limit(%d).toArray(); print(JSON.stringify({total, docs}, null, 2));`,
		collection, filter, skip, limit,
	)
	return []string{"mongosh", "--quiet", mongoContainerURI(cfg), "--eval", js}
}

func (m *MongoDBProvider) EvalQueryCommand(cfg *DBConfig, expr string) []string {
	return []string{"mongosh", "--quiet", mongoContainerURI(cfg), "--eval", expr}
}
