package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
	"rsc.io/qr"
)

// termStyle renders the few emphases Mewla's terminal screens use: bold
// titles and commands, dim secondary text, and one vermilion seal. It is
// plain text unless it writes to a color terminal (not NO_COLOR, TERM=dumb,
// CI or a file), so logs and pipes stay clean.
type termStyle struct {
	color     bool
	trueColor bool
}

func styleForWriter(w io.Writer) termStyle {
	file, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return termStyle{}
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || os.Getenv("CI") != "" {
		return termStyle{}
	}
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	return termStyle{color: true, trueColor: colorTerm == "truecolor" || colorTerm == "24bit"}
}

func (s termStyle) wrap(code, text string) string {
	if !s.color || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s termStyle) bold(text string) string { return s.wrap("1", text) }
func (s termStyle) dim(text string) string  { return s.wrap("2", text) }

// cmd marks text a person types.
func (s termStyle) cmd(text string) string { return s.wrap("1", text) }

// seal is the one vermilion mark on a screen, before its title.
func (s termStyle) seal() string {
	if s.trueColor {
		return s.wrap("38;2;224;69;43", "■")
	}
	return s.wrap("38;5;166", "■")
}

// title is a screen or section heading with the seal.
func (s termStyle) title(text string) string {
	return s.seal() + " " + s.bold(text)
}

// check is one aligned status row: ✓ for ready, ○ for missing or off.
func (s termStyle) check(ok bool, label, value string) string {
	mark := s.dim("○")
	if ok {
		mark = "✓"
	}
	return fmt.Sprintf("  %s %s %s", mark, padRight(label, 16), value)
}

func padRight(text string, width int) string {
	if n := utf8.RuneCountInString(text); n < width {
		return text + strings.Repeat(" ", width-n)
	}
	return text + " "
}

// qrQuietModules is the light border kept around a QR code so phone cameras
// find its edges on light and dark terminals alike.
const qrQuietModules = 2

// qrColumns is how many terminal columns renderQR needs for text.
func qrColumns(text string) int {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return 0
	}
	return code.Size + 2*qrQuietModules + 2
}

// renderQR draws text as a QR code, two modules per character row, indented
// two columns. In color it paints exact black on white, so the code scans the
// same on light and dark backgrounds; in plain text it draws dark modules
// with block characters, which reads correctly on a light background.
func renderQR(w io.Writer, s termStyle, text string) error {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return err
	}
	size := code.Size + 2*qrQuietModules
	dark := func(x, y int) bool {
		x, y = x-qrQuietModules, y-qrQuietModules
		return x >= 0 && y >= 0 && x < code.Size && y < code.Size && code.Black(x, y)
	}
	var out strings.Builder
	for y := 0; y < size; y += 2 {
		out.WriteString("  ")
		for x := 0; x < size; x++ {
			top, bottom := dark(x, y), y+1 < size && dark(x, y+1)
			if s.color {
				fg, bg := "231", "231"
				if top {
					fg = "16"
				}
				if bottom {
					bg = "16"
				}
				out.WriteString("\x1b[38;5;" + fg + ";48;5;" + bg + "m▀")
				continue
			}
			switch {
			case top && bottom:
				out.WriteString("█")
			case top:
				out.WriteString("▀")
			case bottom:
				out.WriteString("▄")
			default:
				out.WriteString(" ")
			}
		}
		if s.color {
			out.WriteString("\x1b[0m")
		}
		out.WriteString("\n")
	}
	_, err = io.WriteString(w, out.String())
	return err
}

// terminalColumns is the width of w when it is a terminal, else 0.
func terminalColumns(w io.Writer) int {
	file, ok := w.(*os.File)
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0
	}
	return width
}

// menuOption is one line of a choice menu.
type menuOption struct {
	label string
	note  string // dim text after the label, e.g. "recommended" or a command
}

// chooseOption asks for one option; the first is the default. On a real
// terminal it is an arrow-key menu (numbers still jump); otherwise it prints
// a numbered list and reads "Choose 1-N [1]:". It returns the 0-based index,
// or -1 when the input closed or was interrupted.
func chooseOption(in *bufio.Reader, w io.Writer, options []menuOption) int {
	if stdin, ok := menuTerminal(w); ok {
		if choice, err := arrowMenu(stdin, in, w, options); err == nil {
			return choice
		}
	}
	return numberMenu(in, w, options)
}

func menuTerminal(w io.Writer) (*os.File, bool) {
	if os.Getenv("TERM") == "dumb" || !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, false
	}
	if file, ok := w.(*os.File); !ok || !term.IsTerminal(int(file.Fd())) {
		return nil, false
	}
	return os.Stdin, true
}

func numberMenu(in *bufio.Reader, w io.Writer, options []menuOption) int {
	s := styleForWriter(w)
	for index, option := range options {
		line := fmt.Sprintf("  %d. %s", index+1, option.label)
		if option.note != "" {
			line = fmt.Sprintf("  %d. %s %s", index+1, padRight(option.label, 13), s.dim(option.note))
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
	fmt.Fprintln(w)
	for {
		fmt.Fprintf(w, "Choose 1-%d [1]: ", len(options))
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(w)
			return -1
		}
		answer := strings.TrimSpace(line)
		if answer == "" {
			return 0
		}
		for index := range options {
			if answer == fmt.Sprint(index+1) {
				return index
			}
		}
		if len(options) == 4 {
			fmt.Fprintln(w, "Type 1, 2, 3 or 4.")
		} else {
			fmt.Fprintf(w, "Type a number from 1 to %d.\n", len(options))
		}
	}
}

// arrowMenu draws the options with a pointer, redraws in place on ↑/↓ (or
// k/j, or a number), and confirms with Enter. Ctrl-C or Esc cancels.
func arrowMenu(stdin *os.File, in *bufio.Reader, w io.Writer, options []menuOption) (int, error) {
	state, err := term.MakeRaw(int(stdin.Fd()))
	if err != nil {
		return 0, err
	}
	defer term.Restore(int(stdin.Fd()), state)
	s := styleForWriter(w)
	selected := 0
	draw := func(first bool) {
		if !first {
			fmt.Fprintf(w, "\x1b[%dA", len(options)+1)
		}
		for index, option := range options {
			pointer, label := "  ", option.label
			if index == selected {
				pointer = s.seal()
				if !s.color {
					pointer = ">"
				}
				pointer += " "
				label = s.bold(label)
			} else {
				pointer = "  "
			}
			line := "  " + pointer + label
			if option.note != "" {
				line += strings.Repeat(" ", max(1, 15-utf8.RuneCountInString(option.label))) + s.dim(option.note)
			}
			fmt.Fprint(w, "\r\x1b[2K"+line+"\r\n")
		}
		fmt.Fprint(w, "\r\x1b[2K"+s.dim("  ↑/↓ to move, Enter to choose")+"\r\n")
	}
	draw(true)
	for {
		key, err := in.ReadByte()
		if err != nil {
			return -1, nil
		}
		switch key {
		case '\r', '\n':
			fmt.Fprintf(w, "\x1b[1A\r\x1b[2K")
			return selected, nil
		case 3, 4: // Ctrl-C, Ctrl-D
			fmt.Fprint(w, "\r\n")
			return -1, nil
		case 'k':
			selected = (selected + len(options) - 1) % len(options)
		case 'j':
			selected = (selected + 1) % len(options)
		case 0x1b:
			next, _ := in.ReadByte()
			if next != '[' && next != 'O' {
				fmt.Fprint(w, "\r\n")
				return -1, nil
			}
			switch arrow, _ := in.ReadByte(); arrow {
			case 'A':
				selected = (selected + len(options) - 1) % len(options)
			case 'B':
				selected = (selected + 1) % len(options)
			}
		default:
			if key >= '1' && int(key-'0') <= len(options) {
				selected = int(key - '1')
			}
		}
		draw(false)
	}
}
