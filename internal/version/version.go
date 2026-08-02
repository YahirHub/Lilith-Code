// Package version contains the single source of truth for Lilith releases.
//
// To publish a new version, change Current and run the manual GitHub Actions
// release workflow. The builder, CLI and release tag all consume this value.
package version

const Current = "0.1.0"
