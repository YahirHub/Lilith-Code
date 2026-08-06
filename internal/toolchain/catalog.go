// Package toolchain resolves and installs optional external accelerators and
// compatibility helpers. Core search and POSIX-compatible command execution
// remain available inside the static Go binary when these tools are absent.
package toolchain

import (
	"fmt"
	"runtime"
)

// ArchiveKind describes how a downloaded artifact must be unpacked.
type ArchiveKind string

const (
	ArchiveRaw ArchiveKind = "raw"    // the download is the binary itself
	ArchiveZip ArchiveKind = "zip"    // .zip containing the binary
	ArchiveTgz ArchiveKind = "tar.gz" // .tar.gz containing the binary
)

// Artifact is a single downloadable build of a tool for one os/arch pair.
type Artifact struct {
	URL string
	// SHA256 is mandatory: a download that does not match is discarded.
	SHA256 string
	Kind   ArchiveKind
	// Member is the file name inside the archive (base name match).
	// Empty for ArchiveRaw.
	Member string
	// Output is the file name written into the tools bin directory.
	Output string
}

// Tool is a logical dependency with one artifact per supported platform.
type Tool struct {
	Name string
	// Why is shown to the user when installing.
	Why string
	// TermuxPackage is installed through Termux pkg instead of downloading a
	// generic Linux artifact that may not match Android's runtime/prefix.
	TermuxPackage string
	// Platforms is keyed by "goos/goarch".
	Platforms map[string]Artifact
}

const rgBase = "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-"

// Ripgrep is an optional accelerator for code search; Lilith has a Go fallback.
var Ripgrep = Tool{
	Name:          "rg",
	Why:           "búsqueda de código rápida (ripgrep)",
	TermuxPackage: "ripgrep",
	Platforms: map[string]Artifact{
		"windows/amd64": {
			URL:    rgBase + "x86_64-pc-windows-msvc.zip",
			SHA256: "d0f534024c42afd6cb4d38907c25cd2b249b79bbe6cc1dbee8e3e37c2b6e25a1",
			Kind:   ArchiveZip, Member: "rg.exe", Output: "rg.exe",
		},
		// Windows on ARM ejecuta el binario x64 mediante emulación.
		"windows/arm64": {
			URL:    rgBase + "x86_64-pc-windows-msvc.zip",
			SHA256: "d0f534024c42afd6cb4d38907c25cd2b249b79bbe6cc1dbee8e3e37c2b6e25a1",
			Kind:   ArchiveZip, Member: "rg.exe", Output: "rg.exe",
		},
		"linux/amd64": {
			URL:    rgBase + "x86_64-unknown-linux-musl.tar.gz",
			SHA256: "4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e",
			Kind:   ArchiveTgz, Member: "rg", Output: "rg",
		},
		"linux/arm64": {
			URL:    rgBase + "aarch64-unknown-linux-gnu.tar.gz",
			SHA256: "c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f",
			Kind:   ArchiveTgz, Member: "rg", Output: "rg",
		},
		"darwin/amd64": {
			URL:    rgBase + "x86_64-apple-darwin.tar.gz",
			SHA256: "fc87e78f7cb3fea12d69072e7ef3b21509754717b746368fd40d88963630e2b3",
			Kind:   ArchiveTgz, Member: "rg", Output: "rg",
		},
		"darwin/arm64": {
			URL:    rgBase + "aarch64-apple-darwin.tar.gz",
			SHA256: "24ad76777745fbff131c8fbc466742b011f925bfa4fffa2ded6def23b5b937be",
			Kind:   ArchiveTgz, Member: "rg", Output: "rg",
		},
	},
}

// Busybox remains an optional compatibility helper for legacy workflows and
// hooks on Windows. Agent terminal commands can use the embedded Go shell.
var Busybox = Tool{
	Name: "busybox",
	Why:  "shell POSIX y utilidades básicas en Windows",
	Platforms: map[string]Artifact{
		"windows/amd64": {
			URL:    "https://frippery.org/files/busybox/busybox64.exe",
			SHA256: "07bb1e5b095b00d68a695481f9240879f33c5724b40aa2308f999d54ed78f075",
			Kind:   ArchiveRaw, Output: "busybox.exe",
		},
		"windows/arm64": {
			URL:    "https://frippery.org/files/busybox/busybox64.exe",
			SHA256: "07bb1e5b095b00d68a695481f9240879f33c5724b40aa2308f999d54ed78f075",
			Kind:   ArchiveRaw, Output: "busybox.exe",
		},
	},
}

// Catalog lists every tool Lilith can install, in install order.
func Catalog() []Tool {
	if runtime.GOOS == "windows" {
		return []Tool{Busybox, Ripgrep}
	}
	return []Tool{Ripgrep}
}

// ArtifactFor returns the artifact for the given platform key ("goos/goarch").
func (t Tool) ArtifactFor(platform string) (Artifact, error) {
	a, ok := t.Platforms[platform]
	if !ok {
		return Artifact{}, fmt.Errorf("%s: sin artefacto para %s", t.Name, platform)
	}
	return a, nil
}

// Platform returns the current "goos/goarch" key.
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
