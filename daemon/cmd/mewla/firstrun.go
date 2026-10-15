package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/daoleno/mewla/daemon/atomicfile"
	"github.com/daoleno/mewla/daemon/auth"
	"github.com/daoleno/mewla/daemon/enrollment"
	"github.com/daoleno/mewla/daemon/remote"
	"golang.org/x/term"
)

const startupChoiceFile = "startup-choice.json"

type startupChoice string

const (
	choiceRemote startupChoice = "remote"
	choiceLAN    startupChoice = "lan"
	choiceOwn    startupChoice = "own"
	choiceLater  startupChoice = "later"
)

type savedStartupChoice struct {
	Choice   startupChoice `json:"choice"`
	ChosenAt time.Time     `json:"chosen_at"`
}

func loadStartupChoice(stateDir string) (startupChoice, bool) {
	raw, err := os.ReadFile(filepath.Join(stateDir, startupChoiceFile))
	if err != nil {
		return "", false
	}
	var saved savedStartupChoice
	if json.Unmarshal(raw, &saved) != nil {
		return "", false
	}
	return saved.Choice, true
}

func saveStartupChoice(stateDir string, choice startupChoice) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(savedStartupChoice{Choice: choice, ChosenAt: time.Now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(stateDir, startupChoiceFile), raw, 0o600)
}

// firstRunPromptAllowed asks only when a person starts a brand-new Mewla by
// hand: no listen flags, a terminal on both ends, not CI or a service
// manager, and no daemon identity yet in the state directory.
func firstRunPromptAllowed(args []string, stateDir string, interactive bool) bool {
	if !interactive || os.Getenv("CI") != "" || daemonSupervised() {
		return false
	}
	if !onlyStateDirArgs(args) {
		return false
	}
	if _, err := os.Stat(filepath.Join(stateDir, "identity.json")); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	if _, chosen := loadStartupChoice(stateDir); chosen {
		return false
	}
	return true
}

func onlyStateDirArgs(args []string) bool {
	for index := 0; index < len(args); index++ {
		switch arg := args[index]; {
		case arg == "-state-dir" || arg == "--state-dir":
			index++
		case strings.HasPrefix(arg, "-state-dir=") || strings.HasPrefix(arg, "--state-dir="):
		default:
			return false
		}
	}
	return true
}

func stdioIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

// runFirstRunChoice asks how phones and browsers should reach this computer
// and remembers the answer, so it is asked once.
func runFirstRunChoice(ctx context.Context, reader *bufio.Reader, w io.Writer, stateDir string) (startupChoice, error) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "How should your phone and browsers reach this computer?")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  1. Sign in, reach it from anywhere (recommended)")
	fmt.Fprintln(w, "  2. Only on the same Wi-Fi")
	fmt.Fprintln(w, "  3. I'll set up Tailscale or HTTPS myself")
	fmt.Fprintln(w, "  4. Decide later")
	fmt.Fprintln(w)
	choice := startupChoice("")
	for choice == "" {
		fmt.Fprint(w, "Choose 1-4 [1]: ")
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return choiceLater, nil
		}
		switch strings.TrimSpace(line) {
		case "", "1":
			choice = choiceRemote
		case "2":
			choice = choiceLAN
		case "3":
			choice = choiceOwn
		case "4":
			choice = choiceLater
		default:
			fmt.Fprintln(w, "Type 1, 2, 3 or 4.")
		}
	}
	switch choice {
	case choiceRemote:
		if _, err := remoteLogin(ctx, w, stateDir, remoteLoginOptions{openBrowser: true}); err != nil {
			fmt.Fprintf(w, "\nRemote access is not on: %v\n", err)
			fmt.Fprintln(w, "Mewla starts for this computer only. Try again any time: mewla login")
			return choiceLater, nil
		}
	case choiceLAN:
		fmt.Fprintln(w, "\nMewla listens on your Wi-Fi/LAN (plain HTTP; use trusted networks only).")
		fmt.Fprintln(w, "It keeps doing so when you start it with plain mewla.")
	case choiceOwn:
		fmt.Fprintln(w, "\nStart with mewla -addr \"$(tailscale ip -4):9876\" for Tailscale, or put HTTPS in front")
		fmt.Fprintln(w, "of 127.0.0.1:9876 and run mewla address add https://…  Guide: docs/connect-and-pair.md")
	case choiceLater:
		fmt.Fprintln(w, "\nMewla starts for this computer only. Reach it from anywhere later: mewla login")
	}
	if err := saveStartupChoice(stateDir, choice); err != nil {
		return choice, err
	}
	return choice, nil
}

// remoteHint is the one line non-interactive starts print while signed out.
func remoteHint(stateDir string) string {
	if _, ok, _ := remote.LoadConfig(stateDir); ok {
		return ""
	}
	return "Reach this computer from anywhere: mewla login"
}

// applyStartupChoice runs the first-run conversation when allowed (host
// dependencies, how to connect, start in the background) and otherwise a
// quick dependency check; then it applies a saved "only on the same Wi-Fi"
// answer when no listen flags were given.
func applyStartupChoice(cfg *daemonConfig, args []string, stderr io.Writer) error {
	prepareToolPath()
	if !onlyStateDirArgs(args) {
		quickDepsCheck(stderr)
		return nil
	}
	stateDir, err := auth.ResolveStorageDir(cfg.stateDir)
	if err != nil {
		return nil
	}
	choice, chosen := loadStartupChoice(stateDir)
	if !chosen && firstRunPromptAllowed(args, stateDir, stdioIsTerminal()) {
		reader := bufio.NewReader(os.Stdin)
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Welcome to Mewla.")
		deps := newDepsPrompt(reader, stderr, true)
		tmuxReady := deps.ensureTmux()
		agentReady := deps.ensureAgent()
		if tmuxReady && agentReady {
			fmt.Fprintln(stderr, "This computer is ready for Brain.")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		choice, err = runFirstRunChoice(ctx, reader, stderr, stateDir)
		stop()
		if err != nil {
			return err
		}
		offerBoot(deps, cfg.stateDir, choice)
	} else {
		quickDepsCheck(stderr)
	}
	if choice == choiceLAN {
		cfg.lan, cfg.addr = true, "0.0.0.0:9876"
	}
	return nil
}

// printEnrollmentRequest shows a browser's join request on this computer's
// terminal. The person approves only when the browser shows the same number.
func printEnrollmentRequest(w io.Writer, stateDir string, request enrollment.Request) {
	// Name and origin come from the unauthenticated request; keep terminal
	// control sequences out.
	name := printableText(request.DeviceName)
	if name == "" {
		name = "A browser"
	}
	command := "mewla devices approve"
	if strings.TrimSpace(stateDir) != "" {
		command += " -state-dir " + stateDir
	}
	fmt.Fprintf(w, "\n%s at %s asks to use this computer.\n", name, printableText(request.Origin))
	fmt.Fprintf(w, "  Number: %s   Approve only if the browser shows the same number:\n", request.Number)
	fmt.Fprintf(w, "  %s -id %s -number %s\n", command, request.ID, request.Number)
	fmt.Fprintf(w, "  It expires at %s. Ignore it to refuse.\n\n", request.ExpiresAt.Local().Format("15:04"))
}

func printableText(value string) string {
	return strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsPrint(character) {
			return character
		}
		return -1
	}, value))
}
