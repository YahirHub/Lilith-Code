---
name: termux-release
description: Build, package, publish, install, and update native Termux release artifacts safely from GitHub Actions.
user-invocable: true
allowed-tools: Read, Glob, Grep, Bash, Edit, Write
when_to_use: Use when adding Termux targets, release assets, checksums, install scripts, or upgrade behavior.
---
# Termux release engineering

Use this skill for the release path of a Termux-compatible CLI.

## Release contract

- Give the Android artifact an unambiguous name such as `li-termux-arm64`.
- Build with explicit `GOOS=android`, `GOARCH=arm64`, and `CGO_ENABLED=0`.
- Include the artifact in the same SHA-256 manifest as Linux and Windows binaries.
- Make the common Unix installer detect Termux before generic Linux target selection.
- Install to `$PREFIX/bin/li`; do not use sudo and do not edit `.bashrc`.
- Replace an existing binary atomically while retaining `$HOME/.li` or equivalent configuration.
- Install or clearly report Termux package dependencies through `pkg`.
- Fail before replacement when architecture, checksum, download, or destination validation fails.

Read `references/release-checklist.md` before publishing.
