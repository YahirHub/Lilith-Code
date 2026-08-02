---
name: termux-release
description: Install, update, compile, and validate Lilith natively inside Termux.
user-invocable: true
allowed-tools: Read, Glob, Grep, Bash, Edit, Write
when_to_use: Use when changing Termux source builds, pkg dependencies, installer behavior, or Android argument handling.
---
# Termux release engineering

Use this skill for the native Termux installation path.

## Release contract

- Do not publish a generic Android binary as a release asset unless it has been verified on a real Termux device.
- Install `git`, `golang`, and runtime tools through `pkg`.
- Clone the requested stable tag and compile `./cmd/li` on the device with `GOOS=android`, `GOARCH=arm64`, and `CGO_ENABLED=0`.
- Install to `$PREFIX/bin/li`; do not use sudo and do not edit `.bashrc`.
- Preserve the managed source checkout under the user's home directory and replace the executable atomically.
- Keep the Android `os.Args` normalization that prevents Cobra from treating the executable path as a subcommand.
- Retain `$HOME/.li` across every update.

Read `references/release-checklist.md` before publishing.
