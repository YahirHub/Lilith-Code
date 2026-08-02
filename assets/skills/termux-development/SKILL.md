---
name: termux-development
description: Design, implement, and debug CLI/TUI compatibility for Termux on Android, including paths, shells, packages, storage, subprocesses, and terminal behavior.
user-invocable: true
allowed-tools: Read, Glob, Grep, Bash, Edit, Write, WebFetch, WebSearch
when_to_use: Use when a terminal application must run directly in Termux or when Linux behavior fails on Android.
---
# Termux development

Use this skill when adapting a CLI or TUI to run directly inside Termux.

## Required approach

1. Detect Termux from runtime/platform and environment rather than treating every Android shell as a normal Linux distribution.
2. Use `$PREFIX/bin` for installed executables and `$HOME` for user data. Resolve commands through `PATH`.
3. Never hardcode `/bin/sh`, `/usr/bin`, `/usr/local/bin`, systemd, sudo, or root-only paths in Termux code paths.
4. Prefer packages from `pkg` for tools that depend on Termux's prefix or Android runtime. Do not download arbitrary GNU/Linux dynamic binaries as replacements.
5. For Go releases, prefer an explicit `GOOS=android`, `GOARCH=arm64`, `CGO_ENABLED=0` artifact when the project and dependencies compile for it.
6. Keep the TUI usable with software keyboards, touch-generated key sequences, narrow screens, terminal resize events, SSH sessions, and interrupted networks.
7. Preserve user data during upgrades and use checksum verification plus atomic replacement.

Read `references/runtime.md` before changing build, installer, shell, or toolchain behavior.

## Validation

- Verify the generated artifact name and build environment.
- Search for hardcoded FHS paths and shell executables.
- Validate the installer with Termux-like `$PREFIX`, `PATH`, and `uname -m` values.
- Confirm required tools are installed or explained through `pkg`.
- Run normal Linux and Windows regression tests after shared code changes.
- Distinguish device-tested behavior from compile-only validation.
