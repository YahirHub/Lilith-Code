# Termux release checklist

## Source build

- [ ] Version source changed to a tag that does not already exist.
- [ ] `go test ./...` passes on the release runner.
- [ ] No unverified `li-termux-*` binary is attached to the release.
- [ ] `install.sh` resolves the requested tag and clones it over HTTPS.
- [ ] `pkg install -y git golang ripgrep` is executed before the build.
- [ ] `./cmd/li` is compiled natively with Android ARM64 settings.

## Installer

- [ ] Termux detection occurs before generic Linux asset selection.
- [ ] Destination is exactly `$PREFIX/bin/li`.
- [ ] Existing installation is replaced atomically.
- [ ] Managed source is preserved under `$HOME`.
- [ ] No shell profile reload is required.
- [ ] User configuration and sessions are not deleted.

## Runtime

- [ ] Duplicate Android executable arguments are normalized before Cobra runs.
- [ ] No Termux code path hardcodes `/bin/sh` or uses `sudo`.
- [ ] Shell commands and tools are resolved through `PATH` and `$PREFIX`.
- [ ] TUI starts and accepts Enter, Ctrl+C, Esc, paste, and resize events on a real Termux device.

## Release

- [ ] Release notes use installer URLs from `raw.githubusercontent.com`.
- [ ] A clean native build and an update from the previous version were tested.
- [ ] Linux and Windows release assets and checksums remain valid.
