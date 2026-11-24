package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestAllSixProvidersAndPresets(t *testing.T) {
	wantNames := []string{"postgres", "mysql", "mariadb", "redis", "valkey", "mongodb"}
	gotNames := SupportedProviderNames()
	if strings.Join(gotNames, ",") != strings.Join(wantNames, ",") {
		t.Fatalf("SupportedProviderNames() = %v, want %v", gotNames, wantNames)
	}

	if !IsMySQLFamily("mysql") || !IsMySQLFamily("mariadb") || IsMySQLFamily("postgres") {
		t.Errorf("IsMySQLFamily mismatch")
	}

	pg := NewPostgresProvider()
	if img := pg.ResolveImage("16", "pgvector", ""); img != "pgvector/pgvector:pg16" {
		t.Errorf("pgvector image = %q, want pgvector/pgvector:pg16", img)
	}
	if img := pg.ResolveImage("17", "postgis", ""); img != "postgis/postgis:17-3.5" {
		t.Errorf("postgis image = %q, want postgis/postgis:17-3.5", img)
	}
	if img := pg.ResolveImage("16", "timescaledb", ""); img != "timescale/timescaledb:latest-pg16" {
		t.Errorf("timescaledb image = %q, want timescale/timescaledb:latest-pg16", img)
	}
	if img := pg.ResolveImage("16", "pgvector", "custom/pg:dev"); img != "custom/pg:dev" {
		t.Errorf("customImage should override preset, got %q", img)
	}

	if err := ValidatePreset("postgres", "pgvector"); err != nil {
		t.Errorf("ValidatePreset(postgres, pgvector) failed: %v", err)
	}
	if err := ValidatePreset("postgres", "unknown"); err == nil {
		t.Errorf("expected error for unknown postgres preset")
	}
	if err := ValidatePreset("redis", "pgvector"); err == nil {
		t.Errorf("expected error for preset on redis")
	}

	// Verify capability interfaces
	maria, _ := For("mariadb")
	if maria.Kind() != EngineSQL {
		t.Errorf("mariadb kind = %q, want sql", maria.Kind())
	}
	if _, ok := AsSQL(maria); !ok {
		t.Errorf("mariadb must implement SQLProvider")
	}
	if _, ok := AsSnapshot(maria); !ok {
		t.Errorf("mariadb must implement SnapshotProvider")
	}

	for _, name := range []string{"redis", "valkey"} {
		p, _ := For(name)
		if p.Kind() != EngineKV {
			t.Errorf("%s kind = %q, want kv", name, p.Kind())
		}
		if _, ok := AsKV(p); !ok {
			t.Errorf("%s must implement KVProvider", name)
		}
		cfg := &DBConfig{Host: "localhost", Port: 6379, RootPassword: "pw"}
		cmd := strings.Join(p.GetCommand(cfg), " ")
		if !strings.Contains(cmd, "--requirepass pw") {
			t.Errorf("%s GetCommand missing --requirepass: %s", name, cmd)
		}
		if !strings.HasPrefix(p.GetConnectionString(cfg), "redis://:pw@localhost:6379/0") {
			t.Errorf("%s conn string = %q", name, p.GetConnectionString(cfg))
		}
	}

	mongo, _ := For("mongodb")
	if mongo.Kind() != EngineDocument {
		t.Errorf("mongodb kind = %q, want document", mongo.Kind())
	}
	if _, ok := AsDocument(mongo); !ok {
		t.Errorf("mongodb must implement DocumentProvider")
	}
	if _, ok := AsSnapshot(mongo); !ok {
		t.Errorf("mongodb must implement SnapshotProvider")
	}
	mCfg := &DBConfig{Host: "localhost", Port: 27017, RootPassword: "p@ss", DatabaseName: "shop"}
	mURI := mongo.GetConnectionString(mCfg)
	if !strings.Contains(mURI, "mongodb://root:p%40ss@localhost:27017/shop?authSource=admin") {
		t.Errorf("unexpected mongo URI: %q", mURI)
	}
}

func TestSplitCommandArgs(t *testing.T) {
	args, err := splitCommandArgs(`SET "user:1" 'hello world' EX 60`)
	if err != nil {
		t.Fatalf("splitCommandArgs failed: %v", err)
	}
	want := []string{"SET", "user:1", "hello world", "EX", "60"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Errorf("args = %v, want %v", args, want)
	}
	if _, err := splitCommandArgs(`SET "unterminated`); err == nil {
		t.Errorf("expected error for unterminated quote")
	}
}

func TestRESPPingScanExecDumpRestore(t *testing.T) {
	// Lightweight in-memory mock RESP2 server over loopback TCP
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	store := map[string]string{
		"user:1": "alice",
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					req, err := readRESPValue(r)
					if err != nil {
						return
					}
					if len(req.array) == 0 {
						return
					}
					cmd := strings.ToUpper(req.array[0].asString())
					switch cmd {
					case "AUTH":
						if len(req.array) > 1 && req.array[1].asString() == "secret" {
							_, _ = io.WriteString(c, "+OK\r\n")
						} else {
							_, _ = io.WriteString(c, "-ERR invalid password\r\n")
						}
					case "PING":
						_, _ = io.WriteString(c, "+PONG\r\n")
					case "SCAN":
						var b strings.Builder
						fmt.Fprintf(&b, "*2\r\n$1\r\n0\r\n*%d\r\n", len(store))
						for k := range store {
							fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(k), k)
						}
						_, _ = io.WriteString(c, b.String())
					case "TYPE":
						_, _ = io.WriteString(c, "+string\r\n")
					case "TTL":
						_, _ = io.WriteString(c, ":-1\r\n")
					case "PTTL":
						_, _ = io.WriteString(c, ":-1\r\n")
					case "GET":
						v, ok := store[req.array[1].asString()]
						if !ok {
							_, _ = io.WriteString(c, "$-1\r\n")
						} else {
							_, _ = fmt.Fprintf(c, "$%d\r\n%s\r\n", len(v), v)
						}
					case "DUMP":
						v := store[req.array[1].asString()]
						_, _ = fmt.Fprintf(c, "$%d\r\n%s\r\n", len(v), v)
					case "FLUSHDB":
						for k := range store {
							delete(store, k)
						}
						_, _ = io.WriteString(c, "+OK\r\n")
					case "RESTORE":
						store[req.array[1].asString()] = req.array[3].asString()
						_, _ = io.WriteString(c, "+OK\r\n")
					default:
						_, _ = io.WriteString(c, "+OK\r\n")
					}
				}
			}(conn)
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	cfg := &DBConfig{Host: "127.0.0.1", Port: port, RootPassword: "secret"}
	prov := NewRedisProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := prov.Ping(ctx, cfg); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	keys, err := prov.ScanKeys(ctx, cfg, "*", 50)
	if err != nil || len(keys) != 1 || keys[0].Key != "user:1" || keys[0].Type != "string" {
		t.Fatalf("ScanKeys unexpected: %+v, err=%v", keys, err)
	}

	detail, err := prov.GetKeyDetail(ctx, cfg, "user:1")
	if err != nil || detail.Value != "alice" {
		t.Fatalf("GetKeyDetail unexpected: %+v, err=%v", detail, err)
	}

	out, _, err := prov.ExecKV(ctx, cfg, "GET user:1")
	if err != nil || out != "alice" {
		t.Fatalf("ExecKV unexpected: %q, err=%v", out, err)
	}

	var buf bytes.Buffer
	if err := prov.DumpKV(ctx, cfg, &buf); err != nil {
		t.Fatalf("DumpKV failed: %v", err)
	}
	delete(store, "user:1")
	if err := prov.RestoreKV(ctx, cfg, &buf); err != nil {
		t.Fatalf("RestoreKV failed: %v", err)
	}
	if store["user:1"] != "alice" {
		t.Fatalf("expected restored key user:1=alice, got %+v", store)
	}
}

func TestMongoPingWireHandshake(t *testing.T) {
	pkt := buildMongoPingOPMsg()
	if len(pkt) < 21 {
		t.Fatalf("packet too short: %d", len(pkt))
	}
	if gotLen := int(binary.LittleEndian.Uint32(pkt[0:4])); gotLen != len(pkt) {
		t.Fatalf("header length %d != len(pkt) %d", gotLen, len(pkt))
	}
	if op := binary.LittleEndian.Uint32(pkt[12:16]); op != 2013 {
		t.Fatalf("opcode = %d, want 2013 (OP_MSG)", op)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var hdr [16]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return
		}
		n := int(binary.LittleEndian.Uint32(hdr[0:4]))
		if n > 16 {
			_, _ = io.CopyN(io.Discard, conn, int64(n-16))
		}
		// Reply with minimal valid 21-byte OP_MSG frame
		reply := make([]byte, 21)
		binary.LittleEndian.PutUint32(reply[0:4], 21)
		binary.LittleEndian.PutUint32(reply[12:16], 2013)
		_, _ = conn.Write(reply)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	prov := NewMongoDBProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := prov.Ping(ctx, &DBConfig{Host: "127.0.0.1", Port: port}); err != nil {
		t.Fatalf("Mongo Ping failed: %v", err)
	}
}
