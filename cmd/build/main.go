// Command build prepares the local toolchain: it downloads and verifies the
// external binaries Lilith needs (busybox shell on Windows, ripgrep) into
// ~/.li/tools/bin.
//
//	go run ./cmd/build check      muestra el estado
//	go run ./cmd/build install    instala lo que falte
//	go run ./cmd/build install -f reinstala todo
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/lilith/li/internal/toolchain"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	action := "check"
	force := false
	targetDir := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "check", "install":
			action = arg
		case "-f", "--force":
			force = true
		case "-dir", "--dir":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "falta valor para -dir")
				os.Exit(2)
			}
			targetDir = args[i+1]
			i++
		default:
			if strings.HasPrefix(arg, "--dir=") {
				targetDir = strings.TrimPrefix(arg, "--dir=")
				break
			}
			fmt.Fprintf(os.Stderr, "argumento desconocido: %s\n", arg)
			os.Exit(2)
		}
	}
	if targetDir != "" {
		if err := os.Setenv("LI_TOOLS_DIR", targetDir); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	if err := run(ctx, action, force); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, action string, force bool) error {
	bin, err := toolchain.BinDir()
	if err != nil {
		return err
	}
	fmt.Printf("Plataforma: %s\nDirectorio: %s\n\n", toolchain.Platform(), bin)

	if action == "check" {
		for _, t := range toolchain.Catalog() {
			if p := toolchain.Lookup(t.Name); p != "" {
				fmt.Printf("  ok       %-8s %s\n", t.Name, p)
			} else {
				fmt.Printf("  falta    %-8s %s\n", t.Name, t.Why)
			}
		}
		if sh, prefix, ok := toolchain.ShellCommand(); ok {
			fmt.Printf("\nShell: %s %v\n", sh, prefix)
		} else {
			fmt.Printf("\nShell: no disponible (ejecuta `go run ./cmd/build install`)\n")
		}
		return nil
	}

	if err := toolchain.EnsureAll(ctx, force, func(msg string) {
		fmt.Println(" ", msg)
	}); err != nil {
		return err
	}
	fmt.Println("\nToolchain lista.")
	return nil
}
