// gotui entry: spawn pi --mode rpc, wire app + builtin + extension, run.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"gotui/src/app"
	"gotui/src/builtin"
	"gotui/src/pirpc"
)

func main() {
	cont := flag.Bool("c", false, "resume the most recent pi session")
	provider := flag.String("provider", "", "pi provider (default from ~/.pi)")
	modelFlag := flag.String("model", "", "pi model (default from ~/.pi)")
	noSession := flag.Bool("no-session", false, "don't persist session")
	mouse := flag.Bool("mouse", false, "enable mouse (click sidebar, wheel scroll; disables native text selection)")
	flag.Parse()

	// inject saved API keys (/login) into env for the pi child to inherit
	keyPath := pirpc.KeyPath()
	for env, key := range pirpc.LoadKeys(keyPath) {
		if key != "" && os.Getenv(env) == "" {
			_ = os.Setenv(env, key)
		}
	}

	cwd, _ := os.Getwd()
	opts := pirpc.Options{
		Provider: *provider, Model: *modelFlag,
		Continue: *cont, NoSession: *noSession,
	}
	pi, err := pirpc.Spawn(opts)
	if err != nil {
		fmt.Println("Cannot start pi:", err)
		os.Exit(1)
	}
	defer pi.Close()

	m := app.New(pi, cwd)
	m.Configure(opts, keyPath)
	m.Mouse = *mouse
	m.UseBuiltins(builtin.All(), builtin.Confirmers())
	// Mouse capture off by default so native highlight-to-copy works.
	// Opt-in with --mouse for sidebar click + wheel scroll.
	progOpts := []tea.ProgramOption{tea.WithAltScreen()}
	if *mouse {
		progOpts = append(progOpts, tea.WithMouseCellMotion())
	}
	prog := tea.NewProgram(m, progOpts...)
	app.ProgRef = prog
	app.WireClient(pi)
	if _, err := prog.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
