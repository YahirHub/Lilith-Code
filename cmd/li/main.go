// Command li is the entry point of Lilith.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/logx"
	"github.com/lilith/li/internal/providers"
	"github.com/lilith/li/internal/providers/openai"
	"github.com/lilith/li/internal/session"
	"github.com/lilith/li/internal/tui"
	buildversion "github.com/lilith/li/internal/version"
)

var (
	version = buildversion.Current
	commit  = "none"
)

var continueLast bool

func main() {
	root := &cobra.Command{
		Use:   "li",
		Short: "Lilith — CLI agéntico compatible con OpenAI",
		RunE:  runTUI,
	}
	root.Flags().BoolVarP(&continueLast, "continue", "c", false,
		"Reanuda la última conversación de este proyecto")
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Muestra la versión",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("Lilith %s (%s)\n", version, commit)
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "where",
		Short: "Muestra la ruta de configuración",
		Run: func(_ *cobra.Command, _ []string) {
			dir, err := config.Dir()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println(dir)
		},
	})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runTUI(_ *cobra.Command, _ []string) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	// Redirect logs to a file so they don't corrupt the TUI.
	logPath := filepath.Join(dir, "lilith.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		logx.SetWriter(f)
	}

	settings, err := config.Load(dir)
	if err != nil {
		return err
	}
	firstRun := settings.OnboardingVersion < config.CurrentOnboardingVersion

	provCfg, err := providers.LoadWithBundled(dir)
	if err != nil {
		return err
	}

	ctx := &tui.AppContext{
		ConfigDir: dir,
		Providers: provCfg,
		Client:    openai.NewClient(dir),
		Styles:    tui.NewStyles(tui.DefaultTheme()),
		FirstRun:  firstRun,
	}
	if continueLast {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		last, err := session.NewStore(dir).Latest(cwd)
		if err != nil {
			return err
		}
		if last == nil {
			fmt.Fprintln(os.Stderr, "No hay conversaciones guardadas en este proyecto.")
		}
		ctx.Resume = last
	}

	root := tui.NewRootModel(ctx)

	// tview owns the interactive terminal runtime on every platform. Windows
	// supplies an explicit Tcell VT screen; other systems let tview create it.
	// Existing screen models remain behind the compatibility adapter while the
	// visual layer is migrated incrementally.
	if err := tui.RunRoot(root); err != nil {
		return err
	}

	// If we finished onboarding, persist the version.
	if firstRun {
		provCfg2, _ := providers.LoadWithBundled(dir)
		if provCfg2.ActiveProviderID != "" {
			_ = config.Complete(dir)
		}
	}
	return nil
}
