# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.7.0] - 2026-11-24

### Added
- **Multi-Model Database Providers**: added full support for **MariaDB** (`mariadb`), **Redis** (`redis`), **Valkey** (`valkey`), and **MongoDB** (`mongodb`) alongside PostgreSQL and MySQL — including native protocol readiness probes, interactive shells (`mariadb`, `redis-cli`, `valkey-cli`, `mongosh`), backups/restores, online logical snapshots, and live/snapshot clones.
- **PostgreSQL Extension Presets & Custom Image Override**: `--preset pgvector|postgis|timescaledb` (and `preset:` in `xmdb.yaml` + TUI create wizard) with automatic `CREATE EXTENSION IF NOT EXISTS ...` on init and reset, plus `--image` override for any custom compatible Docker image.
- **Adaptive TUI Dashboard**:
  - Redis & Valkey display `[3] Keys` (SCAN list with type & TTL + value preview) and `[4] Console` (inline RESP command runner).
  - MongoDB displays `[3] Collections` (collection counts + JSON document preview) and `[4] Query` (inline `mongosh --eval` runner).
  - MariaDB shares full relational ERD, table inspection, and SQL query support.

## [1.6.0] - 2026-11-24

### Added
- **Managed Logical Snapshots & Rollback** (`xmdb snapshot create|list|restore|delete` / alias `snap`): online compressed snapshots stored under `~/.xmdb/snapshots/<db>/`, automatic safety snapshots before `xmdb reset` and `xmdb restore` (retaining last 5 auto-snapshots; skip with `--no-snapshot`), and `xmdb clone --from-snapshot <name-or-id>`.
- **Multi-DB Project Manifest (`xmdb.yaml`)**: `xmdb init [--from-group <group>]`, idempotent `xmdb up [--reseed] [--sync-env]`, and `xmdb down [--remove]` with `<project>_<service>` instance naming and automatic port conflict reassignment.
- **Safe `.env` Sync & Process Runner**: `xmdb env`, `xmdb env sync` (updates only `# xmdb:start ... # xmdb:end` without touching other variables), and `xmdb run [db] -- <cmd>` to inject database environment variables directly into child processes.
- **Database Organizer CLI**: `xmdb rename <old> <new>` (renames state, container, and snapshot directory) and `xmdb move <name> -g <group|global>`.

### Changed
- Refactored `internal/providers` into a capability-based architecture (`Provider`, `SQLProvider`, `SnapshotProvider`) to cleanly decouple lifecycle, SQL access, and snapshot/backup operations ahead of multi-model provider expansion.

## [1.5.0] - 2026-11-23

### Added
- `xmdb doctor` command (`--fix`, `-f`, `--json`) to diagnose container runtime health, orphan containers/volumes, stale database state files, and port conflicts.
- TUI demo video/GIF and bundled example seed data (`examples/seed`).

### Changed
- Polished TUI query editor, ERD mouse/FK interactions, group view, and tab navigation.
- Refactored database provisioning, clone, seed, backup, restore, and reset workflows into `internal/service`.
- Extracted TUI components into `internal/tui` and split `tui_dashboard.go` into focused modules.
