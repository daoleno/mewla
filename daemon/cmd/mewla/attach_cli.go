package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/mewla/daemon/control"
	"github.com/daoleno/mewla/daemon/sessionhost"
	"golang.org/x/term"
)

// detachKey is Ctrl-\, as in dtach and abduco.
const detachKey = 0x1c

const (
	// enterAttach gives the Session a screen of its own, as tmux attach does.
	enterAttach = "\x1b[?1049h\x1b[H\x1b[2J"
	// leaveAttach undoes what the Session's program may have turned on (styles,
	// a hidden cursor, cursor key and keypad modes, mouse and focus reports,
	// bracketed paste, the Kitty and modifyOtherKeys keyboard modes), then
	// gives the terminal its own screen back.
	leaveAttach = "\x1b[0m\x1b[?25h\x1b[?1l\x1b>\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1004l\x1b[?2004l\x1b[=0;1u\x1b[>4m\x1b[?1049l"
	// redraw starts the screen over before a fresh replay.
	redraw = "\x1b[0m\x1b[H\x1b[2J"
)

func runAttachCommand(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mewla attach", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir, socket string
	var tileAll bool
	fs.BoolVar(&tileAll, "all", false, "tile every live Session as a grid that updates; pick one to open")
	fs.StringVar(&stateDir, "state-dir", "", "state directory whose session host to use (default: ~/.mewla)")
	fs.StringVar(&socket, "socket", "", "session host socket (default: derived from the state directory)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla attach [--all] [--state-dir DIR] [--socket PATH] [SESSION]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Show a live Session in this terminal and type into it. SESSION is a Worker's")
		fmt.Fprintln(stderr, "name (any case, or the start of one name), \"brain\", an id (mh-... or %N). With")
		fmt.Fprintln(stderr, "none, pick from the live Sessions: arrow keys and typing filter, Enter attaches")
		fmt.Fprintln(stderr, "(without a terminal the list is printed). Ctrl-\\ detaches; the Session keeps")
		fmt.Fprintln(stderr, "running. This terminal's size becomes the Session's. A Session still in tmux")
		fmt.Fprintln(stderr, "opens in tmux, which detaches with its own prefix key.")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "--all (or Tab in the picker) tiles every live Session in this terminal and keeps")
		fmt.Fprintln(stderr, "them updating, without touching them. Arrow keys, Tab, 1-9 or a click choose;")
		fmt.Fprintln(stderr, "Enter opens one to type into; Ctrl-] returns to the grid; q or Ctrl-\\ leaves.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return fmt.Errorf("name one Session to attach to")
	}
	query := strings.TrimSpace(fs.Arg(0))
	if tileAll && query != "" {
		return fmt.Errorf("--all shows every Session; it takes no name")
	}
	in, out := os.Stdin, os.Stdout
	interactive := term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
	if (query != "" || tileAll) && !interactive {
		return fmt.Errorf("mewla attach needs a terminal")
	}
	path, err := hostSocketPath(stateDir, socket)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	client, err := sessionhost.Dial(dialCtx, path, hostVersion())
	dialCancel()
	hostDown := errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
	if err != nil && !hostDown {
		return err
	}
	if client != nil {
		defer client.Close()
	}
	all, err := gatherAttachEntries(ctx, client, stateDir)
	if err != nil {
		return err
	}
	var entry attachEntry
	wantGrid := tileAll
	if query == "" && !wantGrid {
		listed := listedAttachEntries(all)
		if !interactive || len(listed) == 0 {
			if len(listed) == 0 {
				fmt.Fprintln(stdout, "No live Sessions.")
				if hostDown {
					fmt.Fprintf(stdout, "No session host is running on %s.\n", path)
				}
				return nil
			}
			writeAttachTable(stdout, listed, time.Now(), true)
			return nil
		}
		chosen, grid, err := pickAttachEntry(in, out, listed)
		if err != nil || (chosen == nil && !grid) {
			return err
		}
		if grid {
			wantGrid = true
		} else {
			entry = *chosen
		}
	} else if !wantGrid {
		entry, err = resolveAttach(all, query)
		var missing *attachNotFoundError
		if errors.As(err, &missing) && hostDown {
			return fmt.Errorf("%w (no session host is running on %s)", err, path)
		}
		if err != nil {
			return err
		}
	}
	if wantGrid {
		if client == nil {
			return fmt.Errorf("no session host is running on %s", path)
		}
		outside := 0
		for _, listed := range listedAttachEntries(all) {
			if listed.Tmux != nil {
				outside++
			}
		}
		outcome, err := runGridCommand(ctx, client, outside, in, out)
		if err != nil {
			return err
		}
		if outcome != "" {
			fmt.Fprintln(stdout, outcome)
		}
		return nil
	}
	if entry.Tmux != nil {
		return runTmuxAttach(entry)
	}
	if client == nil {
		return fmt.Errorf("no session host is running on %s", path)
	}
	fmt.Fprintf(out, "Attaching to %s (%s). Ctrl-\\ detaches; the Session keeps running.\r\n", entry.Name, entry.ID)
	saved, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("raw terminal: %w", err)
	}
	resized := make(chan os.Signal, 1)
	notifyTerminalResize(resized)
	defer signal.Stop(resized)
	size := func() (int, int) {
		cols, rows, err := term.GetSize(int(out.Fd()))
		if err != nil {
			return 0, 0
		}
		return cols, rows
	}
	outcome, err := attachLoop(ctx, client, entry.ID, in, out, size, resized)
	_ = term.Restore(int(in.Fd()), saved)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, outcome)
	return nil
}

// gatherAttachEntries lists every Session this computer has: those the session
// host runs (client is nil when none runs), and those still in tmux, which the
// daemon knows. A daemon that does not answer only means no tmux Sessions are
// listed and no prompt is reported.
func gatherAttachEntries(ctx context.Context, client *sessionhost.Client, stateDir string) ([]attachEntry, error) {
	var workers []control.Worker
	if socketPath, err := control.DefaultSocketPath(resolveCLIStateDir(stateDir)); err == nil {
		if resp, err := control.CallWithTimeout(socketPath, control.Request{Type: "worker_list"}, 3*time.Second); err == nil && resp.OK {
			workers = resp.Workers
		}
	}
	needsInput := map[string]bool{}
	for _, worker := range workers {
		if worker.PromptID != "" {
			needsInput[worker.ID] = true
		}
	}
	var all []attachEntry
	hostIDs := map[string]bool{}
	if client != nil {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		infos, err := client.List(callCtx)
		if err != nil {
			return nil, err
		}
		all = hostAttachEntries(infos, needsInput)
		for _, info := range infos {
			hostIDs[info.ID] = true
		}
	}
	return append(all, tmuxAttachEntries(workers, hostIDs, listTmuxPanes)...), nil
}

// pickAttachEntry shows the picker on this terminal and returns the chosen
// Session; nil when the person cancels, with grid set when they asked for the
// grid of every Session instead.
func pickAttachEntry(in, out *os.File, entries []attachEntry) (*attachEntry, bool, error) {
	saved, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, false, fmt.Errorf("raw terminal: %w", err)
	}
	defer term.Restore(int(in.Fd()), saved)
	size := func() (int, int) {
		cols, rows, err := term.GetSize(int(out.Fd()))
		if err != nil {
			return 0, 0
		}
		return cols, rows
	}
	return runPicker(in, out, entries, size, styleForWriter(out), time.Now)
}

// attachLoop shows one host Session on out and sends what in reads to it,
// until Ctrl-\ is read or the Session ends. It reports how it ended.
func attachLoop(ctx context.Context, client *sessionhost.Client, id string, in io.Reader, out io.Writer, size func() (int, int), resized <-chan os.Signal) (string, error) {
	outcome, _, err := attachLoopTyped(ctx, client, id, startInputPump(ctx, in), out, size, resized, 0)
	return outcome, err
}

// startInputPump reads in on a goroutine and hands each chunk on; the channel
// closes when in ends. A caller that reads one terminal through several views
// in turn (the grid, then a Session) keeps one pump, so no view leaves a read
// behind to eat the next one's keys.
func startInputPump(ctx context.Context, in io.Reader) <-chan []byte {
	typed := make(chan []byte)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				select {
				case typed <- append([]byte(nil), buf[:n]...):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				close(typed)
				return
			}
		}
	}()
	return typed
}

// attachLoopTyped is attachLoop over keys that arrive on typed. A nonzero
// backKey ends it too, with back set, for a caller that shows a Session from a
// grid; keys after it in the same read are dropped, as after Ctrl-\.
func attachLoopTyped(ctx context.Context, client *sessionhost.Client, id string, typed <-chan []byte, out io.Writer, size func() (int, int), resized <-chan os.Signal, backKey byte) (outcome string, back bool, err error) {
	attach := func() (*sessionhost.Attachment, error) {
		cols, rows := size()
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		view, err := client.Attach(callCtx, sessionhost.AttachParams{ID: id, Cols: cols, Rows: rows})
		var hostErr *sessionhost.Error
		if errors.As(err, &hostErr) && hostErr.Code == sessionhost.ErrCodeNotFound {
			return nil, fmt.Errorf("no Session %s in the session host", id)
		}
		return view, err
	}
	view, err := attach()
	if err != nil {
		return "", false, err
	}
	defer func() { view.Close() }()
	io.WriteString(out, enterAttach)
	defer io.WriteString(out, leaveAttach)
	out.Write(view.Replay)

	send := func(data []byte) error {
		if len(data) == 0 {
			return nil
		}
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, err := client.Input(callCtx, id, []sessionhost.InputStep{{Bytes: data}})
		return err
	}
	for {
		select {
		case chunk, ok := <-view.Output():
			if ok {
				out.Write(chunk)
				continue
			}
			end, err := view.End()
			switch {
			case err != nil:
				return "", false, fmt.Errorf("the session host stopped showing %s: %w", id, err)
			case end.Reason == sessionhost.AttachBehind:
				// This terminal fell behind: start the screen over.
				view.Close()
				if view, err = attach(); err != nil {
					return "", false, err
				}
				io.WriteString(out, redraw)
				out.Write(view.Replay)
			case end.Reason == sessionhost.AttachExited && end.ExitCode != nil:
				return fmt.Sprintf("[%s exited with code %d]", id, *end.ExitCode), false, nil
			default:
				return fmt.Sprintf("[%s ended: %s]", id, end.Reason), false, nil
			}
		case data, ok := <-typed:
			if !ok {
				return fmt.Sprintf("[detached from %s]", id), false, nil
			}
			cut, isBack := bytes.IndexByte(data, detachKey), false
			if backKey != 0 {
				if i := bytes.IndexByte(data, backKey); i >= 0 && (cut < 0 || i < cut) {
					cut, isBack = i, true
				}
			}
			if cut >= 0 {
				if err := send(data[:cut]); err != nil {
					return "", false, err
				}
				return fmt.Sprintf("[detached from %s]", id), isBack, nil
			}
			if err := send(data); err != nil {
				return "", false, err
			}
		case <-resized:
			if cols, rows := size(); cols > 0 && rows > 0 {
				callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := client.Resize(callCtx, id, cols, rows)
				cancel()
				if err != nil {
					return "", false, err
				}
			}
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
}

// runGridCommand tiles the live Sessions on this terminal until the person
// leaves, reporting how it ended ("" when they left from the grid).
func runGridCommand(ctx context.Context, client *sessionhost.Client, outside int, in, out *os.File) (string, error) {
	saved, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return "", fmt.Errorf("raw terminal: %w", err)
	}
	defer term.Restore(int(in.Fd()), saved)
	resized := make(chan os.Signal, 1)
	notifyTerminalResize(resized)
	defer signal.Stop(resized)
	size := func() (int, int) {
		cols, rows, err := term.GetSize(int(out.Fd()))
		if err != nil {
			return 0, 0
		}
		return cols, rows
	}
	pumpCtx, stopPump := context.WithCancel(ctx)
	defer stopPump()
	return runGrid(ctx, client, outside, startInputPump(pumpCtx, in), out, size, resized, styleForWriter(out))
}
