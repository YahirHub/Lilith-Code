# Termux release checklist

## Build

- [ ] Version source changed to a tag that does not already exist.
- [ ] `go test ./...` passes on the release runner.
- [ ] Android ARM64 target is present in the central build target list.
- [ ] `li-termux-arm64` is generated and non-empty.
- [ ] Linux and Windows targets still build.

## Installer

- [ ] Termux detection occurs before generic Linux asset selection.
- [ ] Only supported Termux architectures are accepted.
- [ ] Destination is exactly `$PREFIX/bin/li`.
- [ ] Existing installation is replaced atomically.
- [ ] Checksum is verified before replacement.
- [ ] No shell profile reload is required.
- [ ] Missing `git` and `ripgrep` are handled through `pkg` or reported clearly.
- [ ] User configuration and sessions are not deleted.

## Runtime

- [ ] No Termux code path hardcodes `/bin/sh`.
- [ ] Shell commands are resolved through `PATH`.
- [ ] Tool installation does not fetch incompatible GNU/Linux artifacts.
- [ ] TUI starts and accepts Enter, Ctrl+C, Esc, paste, and resize events on a real Termux device.

## Release

- [ ] The Termux artifact and installer are included in `SHA256SUMS.txt`.
- [ ] Release notes contain the Termux installation command.
- [ ] A clean install and an update from the previous version were tested.
