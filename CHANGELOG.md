# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.5.0] - 2026-11-23

### Added
- `xmdb doctor` command (`--fix`, `-f`, `--json`) to diagnose container runtime health, orphan containers/volumes, stale database state files, and port conflicts.
- TUI demo video/GIF and bundled example seed data (`examples/seed`).

### Changed
- Polished TUI query editor, ERD mouse/FK interactions, group view, and tab navigation.
- Refactored database provisioning, clone, seed, backup, restore, and reset workflows into `internal/service`.
- Extracted TUI components into `internal/tui` and split `tui_dashboard.go` into focused modules.
