package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestCatalogArtifactsAreComplete(t *testing.T) {
	tools := []Tool{Ripgrep, Busybox}
	for _, tool := range tools {
		for platform, art := range tool.Platforms {
			if len(art.SHA256) != 64 {
				t.Errorf("%s/%s: sha256 inválido", tool.Name, platform)
			}
			if art.Output == "" {
				t.Errorf("%s/%s: falta Output", tool.Name, platform)
			}
			if art.Kind != ArchiveRaw && art.Member == "" {
				t.Errorf("%s/%s: falta Member para %s", tool.Name, platform, art.Kind)
			}
		}
	}
}

func TestArtifactForUnknownPlatform(t *testing.T) {
	if _, err := Busybox.ArtifactFor("plan9/mips"); err == nil {
		t.Fatal("esperaba error para plataforma no soportada")
	}
}

func TestExtractZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("ripgrep-1.0/rg.exe")
	_, _ = w.Write([]byte("binario"))
	_ = zw.Close()

	got, err := extractZip(buf.Bytes(), "rg.exe")
	if err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	if string(got) != "binario" {
		t.Errorf("contenido = %q", got)
	}
	if _, err := extractZip(buf.Bytes(), "otro.exe"); err == nil {
		t.Error("esperaba error para miembro ausente")
	}
}

func TestExtractTarGz(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("binario")
	_ = tw.WriteHeader(&tar.Header{Name: "ripgrep-1.0/rg", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	got, err := extractTarGz(buf.Bytes(), "rg")
	if err != nil {
		t.Fatalf("extractTarGz: %v", err)
	}
	if string(got) != "binario" {
		t.Errorf("contenido = %q", got)
	}
}

func TestRipgrepDeclaresTermuxPackage(t *testing.T) {
	if Ripgrep.TermuxPackage != "ripgrep" {
		t.Fatalf("Termux package=%q", Ripgrep.TermuxPackage)
	}
}
