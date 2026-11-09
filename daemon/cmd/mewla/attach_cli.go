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
	"syscall"
	"time"

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
	fs.StringVar(&stateDir, "state-dir", "", "state directory whose session host to use (default: ~/.mewla)")
	fs.StringVar(&socket, "socket", "", "session host socket (default: derived from the state directory)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mewla attach [--state-dir DIR] [--socket PATH] SESSION")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Show a Session that runs in the session host in this terminal and type into")
		fmt.Fprintln(stderr, "it. Ctrl-\\ detaches; the Session keeps running. This terminal's size becomes")
		fmt.Fprintln(stderr, "the Session's. Sessions in tmux are attached with tmux itself.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("name one Session to attach to")
	}
	id := fs.Arg(0)
	in, out := os.Stdin, os.Stdout
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
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
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("no session host is running on %s", path)
	}
	if err != nil {
		return err
	}
	defer client.Close()

	saved, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("raw terminal: %w", err)
	}
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	size := func() (int, int) {
		cols, rows, err := term.GetSize(int(out.Fd()))
		if err != nil {
			return 0, 0
		}
		return cols, rows
	}
	outcome, err := attachLoop(ctx, client, id, in, out, size, resized)
	_ = term.Restore(int(in.Fd()), saved)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, outcome)
	return nil
}

// attachLoop shows one host Session on out and sends what in reads to it,
// until Ctrl-\ is read or the Session ends. It reports how it ended.
func attachLoop(ctx context.Context, client *sessionhost.Client, id string, in io.Reader, out io.Writer, size func() (int, int), resized <-chan os.Signal) (string, error) {
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
		return "", err
	}
	defer func() { view.Close() }()
	io.WriteString(out, enterAttach)
	defer io.WriteString(out, leaveAttach)
	out.Write(view.Replay)

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
				return "", fmt.Errorf("the session host stopped showing %s: %w", id, err)
			case end.Reason == sessionhost.AttachBehind:
				// This terminal fell behind: start the screen over.
				view.Close()
				if view, err = attach(); err != nil {
					return "", err
				}
				io.WriteString(out, redraw)
				out.Write(view.Replay)
			case end.Reason == sessionhost.AttachExited && end.ExitCode != nil:
				return fmt.Sprintf("[%s exited with code %d]", id, *end.ExitCode), nil
			default:
				return fmt.Sprintf("[%s ended: %s]", id, end.Reason), nil
			}
		case data, ok := <-typed:
			if !ok {
				return fmt.Sprintf("[detached from %s]", id), nil
			}
			if i := bytes.IndexByte(data, detachKey); i >= 0 {
				if err := send(data[:i]); err != nil {
					return "", err
				}
				return fmt.Sprintf("[detached from %s]", id), nil
			}
			if err := send(data); err != nil {
				return "", err
			}
		case <-resized:
			if cols, rows := size(); cols > 0 && rows > 0 {
				callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := client.Resize(callCtx, id, cols, rows)
				cancel()
				if err != nil {
					return "", err
				}
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
