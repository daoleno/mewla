//go:build ghosttyvt && cgo

package ghosttyvt

/*
#cgo CFLAGS: -I${SRCDIR}/../ghostty-libs/include
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../ghostty-libs/linux_amd64
#cgo linux,arm64 LDFLAGS: -L${SRCDIR}/../ghostty-libs/linux_arm64
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../ghostty-libs/darwin_arm64
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../ghostty-libs/windows_amd64
#cgo windows CFLAGS: -DGHOSTTY_STATIC
#cgo LDFLAGS: -lghostty-vt
#cgo linux LDFLAGS: -lm

#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <ghostty/vt.h>

extern void mewlaVtWritePty(uintptr_t handle, uint8_t* data, size_t len);
extern void mewlaVtProgramStatus(uintptr_t handle, GhosttyTerminalProgramStatus* report);
extern void mewlaVtTitleChanged(uintptr_t handle);
extern void mewlaVtBell(uintptr_t handle);

typedef struct {
	uintptr_t handle;
	const char* xtversion;
	size_t xtversion_len;
} mewla_vt_userdata;

static void mewla_vt_write_pty(GhosttyTerminal t, void* ud, const uint8_t* data, size_t len) {
	mewlaVtWritePty(((mewla_vt_userdata*)ud)->handle, (uint8_t*)data, len);
}

static void mewla_vt_program_status(GhosttyTerminal t, void* ud, const GhosttyTerminalProgramStatus* report) {
	mewlaVtProgramStatus(((mewla_vt_userdata*)ud)->handle, (GhosttyTerminalProgramStatus*)report);
}

static void mewla_vt_title_changed(GhosttyTerminal t, void* ud) {
	mewlaVtTitleChanged(((mewla_vt_userdata*)ud)->handle);
}

static void mewla_vt_bell(GhosttyTerminal t, void* ud) {
	mewlaVtBell(((mewla_vt_userdata*)ud)->handle);
}

static GhosttyString mewla_vt_xtversion(GhosttyTerminal t, void* ud) {
	mewla_vt_userdata* u = ud;
	GhosttyString s = { .ptr = (const uint8_t*)u->xtversion, .len = u->xtversion_len };
	return s;
}

static int mewla_vt_bind(GhosttyTerminal t, mewla_vt_userdata* ud, size_t scrollback, bool kitty) {
	int r = GHOSTTY_SUCCESS;
	r |= ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_USERDATA, ud);
	r |= ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_WRITE_PTY, (const void*)mewla_vt_write_pty);
	r |= ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_PROGRAM_STATUS, (const void*)mewla_vt_program_status);
	r |= ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_TITLE_CHANGED, (const void*)mewla_vt_title_changed);
	r |= ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_BELL, (const void*)mewla_vt_bell);
	r |= ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_XTVERSION, (const void*)mewla_vt_xtversion);
	r |= ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_LINES, &scrollback);
	if (r != GHOSTTY_SUCCESS) return r;
	if (!kitty) {
		uint64_t none = 0;
		// A build without Kitty graphics reports UNSUPPORTED: already off.
		int k = ghostty_terminal_set(t, GHOSTTY_TERMINAL_OPT_KITTY_IMAGE_STORAGE_LIMIT, &none);
		if (k != GHOSTTY_SUCCESS && k != GHOSTTY_UNSUPPORTED) return k;
	}
	return GHOSTTY_SUCCESS;
}

static int mewla_vt_format(GhosttyTerminal t, int vt, uint32_t extras, bool unwrap, bool trim,
                           bool select, GhosttyPoint from, GhosttyPoint to,
                           uint8_t** out, size_t* len) {
	GhosttyFormatter f;
	int r = ghostty_formatter_terminal_new(NULL, &f, t);
	if (r != GHOSTTY_SUCCESS) return r;
	GhosttyFormatterFormat emit = vt ? GHOSTTY_FORMATTER_FORMAT_VT : GHOSTTY_FORMATTER_FORMAT_PLAIN;
	r |= ghostty_formatter_set(f, GHOSTTY_FORMATTER_OPT_EMIT, &emit);
	r |= ghostty_formatter_set(f, GHOSTTY_FORMATTER_OPT_UNWRAP, &unwrap);
	r |= ghostty_formatter_set(f, GHOSTTY_FORMATTER_OPT_TRIM, &trim);
	r |= ghostty_formatter_set(f, GHOSTTY_FORMATTER_OPT_EXTRAS, &extras);
	if (r == GHOSTTY_SUCCESS && select) {
		GhosttySelection sel = { .rectangle = false };
		r = ghostty_terminal_grid_ref(t, from, &sel.start);
		if (r == GHOSTTY_SUCCESS) r = ghostty_terminal_grid_ref(t, to, &sel.end);
		if (r == GHOSTTY_SUCCESS) r = ghostty_formatter_set(f, GHOSTTY_FORMATTER_OPT_SELECTION, &sel);
	}
	if (r == GHOSTTY_SUCCESS) r = ghostty_formatter_format_alloc(f, NULL, out, len);
	ghostty_formatter_free(f);
	return r;
}

// A replay restores every piece of terminal state but the palette: the
// formatter writes all 256 entries, and a viewer keeps its own theme.
enum {
	mewla_vt_replay_extras = GHOSTTY_FORMATTER_EXTRAS_ALL & ~GHOSTTY_FORMATTER_EXTRA_PALETTE,
	mewla_vt_cursor_extras = GHOSTTY_FORMATTER_EXTRA_CURSOR,
};

static GhosttyPoint mewla_vt_point(GhosttyPointTag tag, uint16_t x, uint32_t y) {
	GhosttyPoint p = { .tag = tag };
	p.value.coordinate.x = x;
	p.value.coordinate.y = y;
	return p;
}

static bool mewla_vt_mode(GhosttyTerminal t, uint16_t mode) {
	GhosttyTerminalModeConfig c = { .mode = ghostty_mode_new(mode, false), .value = false };
	if (ghostty_terminal_get(t, GHOSTTY_TERMINAL_DATA_MODE, &c) != GHOSTTY_SUCCESS) return false;
	return c.value;
}

// A row's flags: bit 0 soft-wrapped onto the next row, bit 1 continues the
// row above, bit 2 starts a shell prompt (OSC 133), bit 3 continues one.
static int mewla_vt_row_flags(GhosttyTerminal t, uint32_t y, uint8_t* out) {
	GhosttyGridRef ref = {0};
	GhosttyRow row = 0;
	int r = ghostty_terminal_grid_ref(t, mewla_vt_point(GHOSTTY_POINT_TAG_SCREEN, 0, y), &ref);
	if (r == GHOSTTY_SUCCESS) r = ghostty_grid_ref_row(&ref, &row);
	if (r != GHOSTTY_SUCCESS) return r;
	bool wrap = false, cont = false;
	GhosttyRowSemanticPrompt prompt = GHOSTTY_ROW_SEMANTIC_NONE;
	ghostty_row_get(row, GHOSTTY_ROW_DATA_WRAP, &wrap);
	ghostty_row_get(row, GHOSTTY_ROW_DATA_WRAP_CONTINUATION, &cont);
	ghostty_row_get(row, GHOSTTY_ROW_DATA_SEMANTIC_PROMPT, &prompt);
	*out = (wrap ? 1 : 0) | (cont ? 2 : 0) |
		(prompt == GHOSTTY_ROW_SEMANTIC_PROMPT ? 4 : 0) |
		(prompt == GHOSTTY_ROW_SEMANTIC_PROMPT_CONTINUATION ? 8 : 0);
	return GHOSTTY_SUCCESS;
}

static int mewla_vt_restore(const uint8_t* p, size_t n, GhosttyTerminal* out) {
	GhosttySnapshotDecoder d;
	int r = ghostty_snapshot_decoder_new_buf(NULL, &d, p, n);
	if (r != GHOSTTY_SUCCESS) return r;
	r = ghostty_snapshot_decoder_decode(d, out);
	ghostty_snapshot_decoder_free(d);
	return r;
}

static int mewla_vt_encode_key(GhosttyTerminal t, int key, uint16_t mods, const uint8_t* text, size_t text_len,
                               uint32_t unshifted, uint8_t* buf, size_t buf_len, size_t* written) {
	GhosttyKeyEncoder e;
	int r = ghostty_key_encoder_new(NULL, &e);
	if (r != GHOSTTY_SUCCESS) return r;
	GhosttyKeyEvent ev;
	r = ghostty_key_event_new(NULL, &ev);
	if (r != GHOSTTY_SUCCESS) {
		ghostty_key_encoder_free(e);
		return r;
	}
	r |= ghostty_key_encoder_set_from_terminal(e, t);
	r |= ghostty_key_event_set_action(ev, GHOSTTY_KEY_ACTION_PRESS);
	r |= ghostty_key_event_set_key(ev, (GhosttyKey)key);
	r |= ghostty_key_event_set_mods(ev, mods);
	if (text_len > 0) {
		GhosttyString s = { .ptr = text, .len = text_len };
		r |= ghostty_key_event_set_utf8(ev, s);
	}
	if (unshifted) r |= ghostty_key_event_set_unshifted_codepoint(ev, unshifted);
	if (r == GHOSTTY_SUCCESS) r = ghostty_key_encoder_encode(e, ev, buf, buf_len, written);
	ghostty_key_event_free(ev);
	ghostty_key_encoder_free(e);
	return r;
}

static size_t mewla_vt_version(const uint8_t** ptr) {
	GhosttyString s = { 0 };
	if (ghostty_build_info(GHOSTTY_BUILD_INFO_VERSION_STRING, &s) != GHOSTTY_SUCCESS) return 0;
	*ptr = s.ptr;
	return s.len;
}
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"sync"
	"unsafe"
)

// Available reports whether this build has the terminal engine.
func Available() bool { return true }

// Version is the libghostty-vt version of this build.
func Version() string {
	var ptr *C.uint8_t
	n := C.mewla_vt_version(&ptr)
	if n == 0 || ptr == nil {
		return ""
	}
	return C.GoStringN((*C.char)(unsafe.Pointer(ptr)), C.int(n))
}

// Terminal is one libghostty-vt terminal. Its methods are safe to call from
// several goroutines; each one runs alone.
type Terminal struct {
	mu       sync.Mutex
	t        C.GhosttyTerminal
	handle   cgo.Handle
	userdata *C.mewla_vt_userdata // C memory: libghostty keeps the pointer
	pending  Output               // filled by callbacks during one call
	closed   bool
}

// New makes a terminal of the given size.
func New(opts Options) (*Terminal, error) {
	cols, rows, err := checkSize(opts.Cols, opts.Rows)
	if err != nil {
		return nil, err
	}
	term := &Terminal{}
	if r := C.ghostty_terminal_new(nil, &term.t, cols, rows); r != C.GHOSTTY_SUCCESS {
		return nil, fmt.Errorf("new terminal: libghostty result %d", int(r))
	}
	if err := term.bind(opts); err != nil {
		C.ghostty_terminal_free(term.t)
		return nil, err
	}
	return term, nil
}

// Restore rebuilds a terminal from a Snapshot. The callbacks and options are
// not part of a snapshot and come from opts; its size is ignored.
func Restore(snapshot []byte, opts Options) (*Terminal, error) {
	if len(snapshot) == 0 {
		return nil, fmt.Errorf("restore terminal: empty snapshot")
	}
	term := &Terminal{}
	buf := C.CBytes(snapshot)
	defer C.free(buf)
	if r := C.mewla_vt_restore((*C.uint8_t)(buf), C.size_t(len(snapshot)), &term.t); r != C.GHOSTTY_SUCCESS {
		return nil, fmt.Errorf("restore terminal: libghostty result %d", int(r))
	}
	if err := term.bind(opts); err != nil {
		C.ghostty_terminal_free(term.t)
		return nil, err
	}
	return term, nil
}

func (term *Terminal) bind(opts Options) error {
	xtversion := opts.XTVersion
	if xtversion == "" {
		xtversion = DefaultXTVersion
	}
	scrollback := opts.ScrollbackLines
	if scrollback <= 0 {
		scrollback = DefaultScrollbackLines
	}
	term.handle = cgo.NewHandle(term)
	term.userdata = (*C.mewla_vt_userdata)(C.calloc(1, C.size_t(unsafe.Sizeof(C.mewla_vt_userdata{}))))
	term.userdata.handle = C.uintptr_t(term.handle)
	term.userdata.xtversion = C.CString(xtversion)
	term.userdata.xtversion_len = C.size_t(len(xtversion))
	if r := C.mewla_vt_bind(term.t, term.userdata, C.size_t(scrollback), C.bool(opts.KittyGraphics)); r != C.GHOSTTY_SUCCESS {
		term.release()
		return fmt.Errorf("configure terminal: libghostty result %d", int(r))
	}
	return nil
}

func (term *Terminal) release() {
	if term.userdata != nil {
		C.free(unsafe.Pointer(term.userdata.xtversion))
		C.free(unsafe.Pointer(term.userdata))
		term.userdata = nil
	}
	term.handle.Delete()
}

// takeOutput returns what the callbacks gathered and starts over. Callers
// hold mu.
func (term *Terminal) takeOutput() Output {
	out := term.pending
	term.pending = Output{}
	return out
}

func handleTerminal(handle C.uintptr_t) *Terminal {
	return cgo.Handle(handle).Value().(*Terminal)
}

//export mewlaVtWritePty
func mewlaVtWritePty(handle C.uintptr_t, data *C.uint8_t, n C.size_t) {
	term := handleTerminal(handle)
	term.pending.Reply = append(term.pending.Reply, C.GoBytes(unsafe.Pointer(data), C.int(n))...)
}

//export mewlaVtProgramStatus
func mewlaVtProgramStatus(handle C.uintptr_t, report *C.GhosttyTerminalProgramStatus) {
	term := handleTerminal(handle)
	term.pending.Status = append(term.pending.Status, Status{
		State:    StatusState(report.state),
		Kind:     StatusKind(report.kind),
		Progress: int(report.progress),
		ID:       goString(report.id),
		App:      goString(report.app),
		Title:    goString(report.title),
		Message:  goString(report.message),
	})
}

//export mewlaVtTitleChanged
func mewlaVtTitleChanged(handle C.uintptr_t) {
	handleTerminal(handle).pending.TitleChanged = true
}

//export mewlaVtBell
func mewlaVtBell(handle C.uintptr_t) {
	handleTerminal(handle).pending.Bells++
}

func goString(s C.GhosttyString) string {
	if s.ptr == nil || s.len == 0 {
		return ""
	}
	return C.GoStringN((*C.char)(unsafe.Pointer(s.ptr)), C.int(s.len))
}

// Write feeds the program's output to the terminal.
func (term *Terminal) Write(p []byte) Output {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed || len(p) == 0 {
		return Output{}
	}
	C.ghostty_terminal_vt_write(term.t, (*C.uint8_t)(unsafe.Pointer(&p[0])), C.size_t(len(p)))
	return term.takeOutput()
}

// Resize changes the terminal's size. A program that asked for in-band size
// reports gets one in the returned Output.
func (term *Terminal) Resize(cols, rows int) (Output, error) {
	c, r, err := checkSize(cols, rows)
	if err != nil {
		return Output{}, err
	}
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed {
		return Output{}, fmt.Errorf("resize terminal: closed")
	}
	if res := C.ghostty_terminal_resize(term.t, c, r, 8, 16); res != C.GHOSTTY_SUCCESS {
		return term.takeOutput(), fmt.Errorf("resize terminal: libghostty result %d", int(res))
	}
	return term.takeOutput(), nil
}

// Capture formats the terminal as text.
func (term *Terminal) Capture(opts CaptureOptions) (string, error) {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed {
		return "", fmt.Errorf("capture terminal: closed")
	}
	return term.formatLocked(opts, opts.Styled, C.GHOSTTY_FORMATTER_EXTRAS_NONE)
}

// Replay returns VT that rebuilds what the terminal shows on a fresh terminal
// of the same size: the screen and its styles, the cursor, the modes, the
// keyboard modes, the tab stops and the rest. It leaves out the history and
// the palette, which a viewer keeps as its own theme. While the alternate
// screen is active the primary screen goes first, so a viewer still has it
// when the program leaves the alternate screen.
func (term *Terminal) Replay() ([]byte, error) {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed {
		return nil, fmt.Errorf("replay terminal: closed")
	}
	var out []byte
	var screen C.GhosttyTerminalScreen
	C.ghostty_terminal_get(term.t, C.GHOSTTY_TERMINAL_DATA_ACTIVE_SCREEN, unsafe.Pointer(&screen))
	if screen == C.GHOSTTY_TERMINAL_SCREEN_ALTERNATE {
		primary, err := term.primaryReplayLocked()
		if err != nil {
			return nil, err
		}
		out = append(out, primary...)
	}
	active, err := term.formatLocked(CaptureOptions{Scope: ScopeScreen}, true, C.mewla_vt_replay_extras)
	if err != nil {
		return nil, err
	}
	return append(out, active...), nil
}

// primaryReplayLocked formats the primary screen and its cursor while the
// alternate one is active. The formatter only reads the active screen, so a
// copy of the terminal leaves the alternate screen the way the program
// entered it, which also brings back the cursor that mode 1049 saved.
func (term *Terminal) primaryReplayLocked() (string, error) {
	snapshot, err := term.snapshotLocked()
	if err != nil {
		return "", err
	}
	primary, err := Restore(snapshot, Options{})
	if err != nil {
		return "", err
	}
	defer primary.Close()
	leave := "\x1b[?47l"
	switch {
	case bool(C.mewla_vt_mode(term.t, 1049)):
		leave = "\x1b[?1049l"
	case bool(C.mewla_vt_mode(term.t, 1047)):
		leave = "\x1b[?1047l"
	}
	primary.Write([]byte(leave))
	primary.mu.Lock()
	defer primary.mu.Unlock()
	return primary.formatLocked(CaptureOptions{Scope: ScopeScreen}, true, C.mewla_vt_cursor_extras)
}

func (term *Terminal) formatLocked(opts CaptureOptions, styled bool, extras C.uint32_t) (string, error) {
	vt := C.int(0)
	if styled {
		vt = 1
	}
	selected := C.bool(false)
	var from, to C.GhosttyPoint
	if opts.Scope != ScopeAll {
		var cols, rows C.uint16_t
		var total C.size_t
		C.ghostty_terminal_get(term.t, C.GHOSTTY_TERMINAL_DATA_COLS, unsafe.Pointer(&cols))
		C.ghostty_terminal_get(term.t, C.GHOSTTY_TERMINAL_DATA_ROWS, unsafe.Pointer(&rows))
		C.ghostty_terminal_get(term.t, C.GHOSTTY_TERMINAL_DATA_TOTAL_ROWS, unsafe.Pointer(&total))
		history := int(total) - int(rows) // rows above the screen
		top := history
		to = C.mewla_vt_point(C.GHOSTTY_POINT_TAG_ACTIVE, cols-1, C.uint32_t(rows-1))
		switch opts.Scope {
		case ScopeRecent:
			if opts.History > 0 {
				top -= opts.History
			}
		case ScopeHistory:
			if history <= 0 {
				return "", nil
			}
			top = 0
			if opts.History > 0 {
				top = history - opts.History
			}
			to = C.mewla_vt_point(C.GHOSTTY_POINT_TAG_SCREEN, cols-1, C.uint32_t(history-1))
		}
		top = max(top, 0)
		selected = true
		from = C.mewla_vt_point(C.GHOSTTY_POINT_TAG_SCREEN, 0, C.uint32_t(top))
	}
	var out *C.uint8_t
	var n C.size_t
	res := C.mewla_vt_format(term.t, vt, extras, C.bool(opts.Unwrap), C.bool(opts.Trim), selected, from, to, &out, &n)
	if res != C.GHOSTTY_SUCCESS {
		return "", fmt.Errorf("capture terminal: libghostty result %d", int(res))
	}
	if out == nil {
		return "", nil
	}
	text := C.GoStringN((*C.char)(unsafe.Pointer(out)), C.int(n))
	C.ghostty_free(nil, out, n)
	return text, nil
}

// RowFlags reads the flags of count rows of the primary screen's history and
// screen, from row top (zero is the oldest). It returns nil when a row
// cannot be read; while the alternate screen is active the rows are its own.
func (term *Terminal) RowFlags(top, count int) []uint8 {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed || top < 0 || count <= 0 {
		return nil
	}
	flags := make([]uint8, count)
	for i := range flags {
		var f C.uint8_t
		if C.mewla_vt_row_flags(term.t, C.uint32_t(top+i), &f) != C.GHOSTTY_SUCCESS {
			return nil
		}
		flags[i] = uint8(f)
	}
	return flags
}

// State reads the terminal's shape and modes.
func (term *Terminal) State() State {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed {
		return State{}
	}
	var cols, rows, cx, cy C.uint16_t
	var screen C.GhosttyTerminalScreen
	var cursorVisible, mouse, ground C.bool
	var scrollback, total C.size_t
	var title, pwd C.GhosttyString
	get := func(data C.GhosttyTerminalData, out unsafe.Pointer) bool {
		return C.ghostty_terminal_get(term.t, data, out) == C.GHOSTTY_SUCCESS
	}
	get(C.GHOSTTY_TERMINAL_DATA_COLS, unsafe.Pointer(&cols))
	get(C.GHOSTTY_TERMINAL_DATA_ROWS, unsafe.Pointer(&rows))
	get(C.GHOSTTY_TERMINAL_DATA_CURSOR_X, unsafe.Pointer(&cx))
	get(C.GHOSTTY_TERMINAL_DATA_CURSOR_Y, unsafe.Pointer(&cy))
	get(C.GHOSTTY_TERMINAL_DATA_ACTIVE_SCREEN, unsafe.Pointer(&screen))
	get(C.GHOSTTY_TERMINAL_DATA_CURSOR_VISIBLE, unsafe.Pointer(&cursorVisible))
	get(C.GHOSTTY_TERMINAL_DATA_MOUSE_TRACKING, unsafe.Pointer(&mouse))
	get(C.GHOSTTY_TERMINAL_DATA_VT_GROUND, unsafe.Pointer(&ground))
	get(C.GHOSTTY_TERMINAL_DATA_SCROLLBACK_ROWS, unsafe.Pointer(&scrollback))
	get(C.GHOSTTY_TERMINAL_DATA_TOTAL_ROWS, unsafe.Pointer(&total))
	state := State{
		Cols:           int(cols),
		Rows:           int(rows),
		Alt:            screen == C.GHOSTTY_TERMINAL_SCREEN_ALTERNATE,
		CursorX:        int(cx),
		CursorY:        int(cy),
		CursorVisible:  bool(cursorVisible),
		BracketedPaste: bool(C.mewla_vt_mode(term.t, 2004)),
		AppCursor:      bool(C.mewla_vt_mode(term.t, 1)),
		AltScroll:      bool(C.mewla_vt_mode(term.t, 1007)),
		MouseFormat:    mouseFormat(func(mode uint16) bool { return bool(C.mewla_vt_mode(term.t, C.uint16_t(mode))) }),
		MouseTracking:  bool(mouse),
		ScrollbackRows: int(scrollback),
		TotalRows:      int(total),
		Ground:         bool(ground),
	}
	if get(C.GHOSTTY_TERMINAL_DATA_TITLE, unsafe.Pointer(&title)) {
		state.Title = goString(title)
	}
	if get(C.GHOSTTY_TERMINAL_DATA_PWD, unsafe.Pointer(&pwd)) {
		state.Pwd = goString(pwd)
	}
	return state
}

// Snapshot encodes the terminal's whole state for Restore.
func (term *Terminal) Snapshot() ([]byte, error) {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed {
		return nil, fmt.Errorf("snapshot terminal: closed")
	}
	return term.snapshotLocked()
}

func (term *Terminal) snapshotLocked() ([]byte, error) {
	var out *C.uint8_t
	var n C.size_t
	if r := C.ghostty_snapshot_encode_alloc(term.t, nil, &out, &n); r != C.GHOSTTY_SUCCESS {
		return nil, fmt.Errorf("snapshot terminal: libghostty result %d", int(r))
	}
	data := C.GoBytes(unsafe.Pointer(out), C.int(n))
	C.ghostty_free(nil, out, n)
	return data, nil
}

// Close frees the terminal. Later calls do nothing or fail.
func (term *Terminal) Close() {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed {
		return
	}
	term.closed = true
	C.ghostty_terminal_free(term.t)
	term.release()
}

// EncodeKey returns the bytes a terminal sends for one key press, in tmux's
// key notation ("Enter", "C-c", "BTab", "Up", "q"), encoded for the modes the
// program has set: cursor key mode, the Kitty keyboard protocol and the rest.
func (term *Terminal) EncodeKey(spec string) ([]byte, error) {
	press, err := parseKey(spec)
	if err != nil {
		return nil, err
	}
	key, text := cKey(press)
	var mods C.uint16_t
	if press.shift {
		mods |= C.GHOSTTY_MODS_SHIFT
	}
	if press.ctrl {
		mods |= C.GHOSTTY_MODS_CTRL
	}
	if press.alt {
		mods |= C.GHOSTTY_MODS_ALT
	}
	var textPtr *C.uint8_t
	if len(text) > 0 {
		cText := C.CBytes([]byte(text))
		defer C.free(cText)
		textPtr = (*C.uint8_t)(cText)
	}
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.closed {
		return nil, fmt.Errorf("encode key: terminal closed")
	}
	var buf [128]C.uint8_t
	var written C.size_t
	r := C.mewla_vt_encode_key(term.t, key, mods, textPtr, C.size_t(len(text)), C.uint32_t(press.unshifted()),
		&buf[0], C.size_t(len(buf)), &written)
	if r != C.GHOSTTY_SUCCESS {
		return nil, fmt.Errorf("encode key %q: libghostty result %d", spec, int(r))
	}
	return C.GoBytes(unsafe.Pointer(&buf[0]), C.int(written)), nil
}

// cKey is a parsed key as libghostty's key and the text it types.
func cKey(press keyPress) (C.int, string) {
	switch press.name {
	case keyEnter:
		return C.GHOSTTY_KEY_ENTER, ""
	case keyEscape:
		return C.GHOSTTY_KEY_ESCAPE, ""
	case keyTab:
		return C.GHOSTTY_KEY_TAB, ""
	case keySpace:
		return C.GHOSTTY_KEY_SPACE, " "
	case keyBackspace:
		return C.GHOSTTY_KEY_BACKSPACE, ""
	case keyUp:
		return C.GHOSTTY_KEY_ARROW_UP, ""
	case keyDown:
		return C.GHOSTTY_KEY_ARROW_DOWN, ""
	case keyLeft:
		return C.GHOSTTY_KEY_ARROW_LEFT, ""
	case keyRight:
		return C.GHOSTTY_KEY_ARROW_RIGHT, ""
	case keyHome:
		return C.GHOSTTY_KEY_HOME, ""
	case keyEnd:
		return C.GHOSTTY_KEY_END, ""
	case keyPageUp:
		return C.GHOSTTY_KEY_PAGE_UP, ""
	case keyPageDown:
		return C.GHOSTTY_KEY_PAGE_DOWN, ""
	case keyDelete:
		return C.GHOSTTY_KEY_DELETE, ""
	case keyInsert:
		return C.GHOSTTY_KEY_INSERT, ""
	case keyText:
		text := string(press.text)
		switch r := press.unshifted(); {
		case r >= 'a' && r <= 'z':
			return C.GHOSTTY_KEY_A + C.int(r-'a'), text
		case r >= '0' && r <= '9':
			return C.GHOSTTY_KEY_DIGIT_0 + C.int(r-'0'), text
		}
		if key, ok := punctuationKeys[press.text]; ok {
			return key, text
		}
		return C.GHOSTTY_KEY_UNIDENTIFIED, text
	}
	return C.GHOSTTY_KEY_F1 + C.int(press.name-keyF1), ""
}

var punctuationKeys = map[rune]C.int{
	'`': C.GHOSTTY_KEY_BACKQUOTE, '\\': C.GHOSTTY_KEY_BACKSLASH, '[': C.GHOSTTY_KEY_BRACKET_LEFT,
	']': C.GHOSTTY_KEY_BRACKET_RIGHT, ',': C.GHOSTTY_KEY_COMMA, '=': C.GHOSTTY_KEY_EQUAL,
	'-': C.GHOSTTY_KEY_MINUS, '.': C.GHOSTTY_KEY_PERIOD, '\'': C.GHOSTTY_KEY_QUOTE,
	';': C.GHOSTTY_KEY_SEMICOLON, '/': C.GHOSTTY_KEY_SLASH,
}

func checkSize(cols, rows int) (C.uint16_t, C.uint16_t, error) {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return 0, 0, fmt.Errorf("terminal size %dx%d is outside 1x1..1000x1000", cols, rows)
	}
	return C.uint16_t(cols), C.uint16_t(rows), nil
}
