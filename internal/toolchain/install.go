package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// maxArtifactBytes caps downloads so a hostile or broken mirror cannot fill the disk.
const maxArtifactBytes = 64 << 20

// Install downloads, verifies and unpacks a tool into BinDir. It is a no-op
// when the tool is already present unless force is true.
func Install(ctx context.Context, t Tool, force bool, progress func(string)) (string, error) {
	if runtime.GOOS == "android" {
		return installTermuxPackage(ctx, t, force, progress)
	}
	art, err := t.ArtifactFor(Platform())
	if err != nil {
		return "", err
	}
	bin, err := BinDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(bin, art.Output)
	if !force {
		if _, err := os.Stat(dest); err == nil {
			return dest, nil
		}
	}

	log := func(format string, a ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, a...))
		}
	}
	log("Descargando %s (%s)…", t.Name, t.Why)

	data, err := download(ctx, art.URL)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != art.SHA256 {
		return "", fmt.Errorf("%s: checksum inválido (esperado %s, obtenido %s)", t.Name, art.SHA256, got)
	}

	payload, err := extract(art, data)
	if err != nil {
		return "", err
	}
	if err := writeExecutable(dest, payload); err != nil {
		return "", err
	}
	log("Instalado %s en %s", t.Name, dest)
	return dest, nil
}

func installTermuxPackage(ctx context.Context, t Tool, force bool, progress func(string)) (string, error) {
	if !force {
		if current := Lookup(t.Name); current != "" {
			return current, nil
		}
	}
	pkgName := strings.TrimSpace(t.TermuxPackage)
	if pkgName == "" {
		return "", fmt.Errorf("%s: no existe paquete Termux configurado", t.Name)
	}
	pkg, err := exec.LookPath("pkg")
	if err != nil {
		return "", fmt.Errorf("%s: Termux requiere `pkg install %s`: %w", t.Name, pkgName, err)
	}
	if progress != nil {
		progress(fmt.Sprintf("Instalando %s con pkg (%s)…", pkgName, t.Why))
	}
	cmd := exec.CommandContext(ctx, pkg, "install", "-y", pkgName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pkg install %s: %w", pkgName, err)
	}
	installed := Lookup(t.Name)
	if installed == "" {
		return "", fmt.Errorf("pkg instaló %s pero no se encontró %s en PATH", pkgName, t.Name)
	}
	return installed, nil
}

// EnsureAll installs every tool in the catalog that is missing.
func EnsureAll(ctx context.Context, force bool, progress func(string)) error {
	for _, t := range Catalog() {
		if _, err := Install(ctx, t, force, progress); err != nil {
			return err
		}
	}
	return nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("descarga rechazada, se exige https: %s", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("descarga %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("descarga %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArtifactBytes {
		return nil, fmt.Errorf("descarga %s: excede %d bytes", url, maxArtifactBytes)
	}
	return data, nil
}

// extract pulls the wanted member out of the downloaded artifact.
func extract(art Artifact, data []byte) ([]byte, error) {
	switch art.Kind {
	case ArchiveRaw:
		return data, nil
	case ArchiveZip:
		return extractZip(data, art.Member)
	case ArchiveTgz:
		return extractTarGz(data, art.Member)
	default:
		return nil, fmt.Errorf("formato de archivo no soportado: %s", art.Kind)
	}
}

func extractZip(data []byte, member string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != member {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, maxArtifactBytes))
	}
	return nil, fmt.Errorf("no se encontró %s dentro del zip", member)
}

func extractTarGz(data []byte, member string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != member {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, maxArtifactBytes))
	}
	return nil, fmt.Errorf("no se encontró %s dentro del tar.gz", member)
}

// writeExecutable writes atomically with 0700 so only the user can run it.
func writeExecutable(dest string, data []byte) error {
	tmp := fmt.Sprintf("%s.%d.tmp", dest, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o700); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(dest, 0o700) // no-op en Windows
	return nil
}
