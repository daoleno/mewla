// Package phone is how Mewla's agents use the person's Android phone. The phone's App holds one
// signed WebSocket to the current computer (/phone/device); the daemon sends it one request at a
// time for each agent action and gets the resulting screen back. Everything below is that wire
// contract; the App mirrors it in app/services/phoneProtocol.ts.
package phone

import "encoding/json"

// Version is the protocol version a phone announces in Hello. A daemon that does not know it
// refuses the connection, and the App says the phone needs updating.
const Version = 1

// Message types, in the "type" field of every JSON text frame on /phone/device.
const (
	TypeHello   = "hello"   // phone -> daemon, first frame
	TypeState   = "state"   // phone -> daemon, whenever its state changes
	TypeEvent   = "event"   // phone -> daemon, something the person did
	TypeRequest = "request" // daemon -> phone
	TypeResult  = "result"  // phone -> daemon, answers one request
)

// Phone states, in State.State. Only Ready lets the daemon send actions.
const (
	StateReady          = "ready"             // helper is running and the agent display can be made
	StateSetup          = "setup"             // not paired or the helper is not running yet
	StatePersonInCharge = "person_in_control" // the person took over the agent's screen
	StateStopped        = "stopped"           // the person pressed Stop; actions are refused until they hand it back
)

// Events from the person, in Event.Kind.
const (
	EventStop      = "stop"       // Stop: end whatever the agent is doing now
	EventTakeOver  = "take_over"  // the person drives the agent's screen
	EventGiveBack  = "give_back"  // the person hands it back; the agent may act again
	EventHandedOff = "handed_off" // answers a handoff: the person finished what they were asked to do
)

// Ops the daemon asks of the phone, in Request.Op. Coordinates are pixels of the agent's display
// (Screen.W x Screen.H); screenshots are sent at that size, so a point in the picture is a point
// on the display.
const (
	OpScreen  = "screen"  // read the screen now. Args: ScreenArgs
	OpApps    = "apps"    // list launchable apps
	OpLaunch  = "launch"  // open an app on the agent's display. Args: LaunchArgs
	OpTap     = "tap"     // Args: TapArgs
	OpSwipe   = "swipe"   // Args: SwipeArgs
	OpText    = "text"    // type into the focused field. Args: TextArgs
	OpKey     = "key"     // back, enter, delete, tab, search. Args: KeyArgs
	OpWait    = "wait"    // let the screen settle or time pass. Args: WaitArgs
	OpHandoff = "handoff" // ask the person to do something on the phone; blocks until they are done. Args: HandoffArgs
	OpRelease = "release" // the agent is done: free the agent's display
)

// Result codes, in Result.Code when OK is false.
const (
	CodePersonInControl = "person_in_control" // the person is driving; try again when they give it back
	CodeStopped         = "stopped"           // the person stopped the agent
	CodeNotInstalled    = "not_installed"
	CodeInUse           = "in_use_on_main" // the person is using that app on their own screen
	CodeNotReady        = "not_ready"      // phone is in Setup
	CodeScreenOff       = "screen_off"     // the phone's screen is off: a picture only, no element list
	CodeFailed          = "failed"
)

// WebSocket close codes the daemon ends /phone/device with; the close reason is a sentence for
// the person. The phone does not reconnect after CloseReplaced or CloseUnsupported.
const (
	CloseReplaced    = 4001 // another connection from a paired device took this one's place
	CloseUnsupported = 4002 // the phone's protocol Version is not one this daemon knows
	CloseProtocol    = 4003 // the first frame was not a valid hello, or a frame was not JSON
)

// A handoff ends when the phone answers its request with a Result, or with an Event of kind
// EventHandedOff whose ID is the handoff request's ID. The daemon accepts whichever comes first.

// Hello is the phone's first frame.
type Hello struct {
	Type       string `json:"type"`
	Version    int    `json:"version"`
	Model      string `json:"model"`
	SDK        int    `json:"sdk"`
	AppVersion string `json:"app_version"`
	State      string `json:"state"`
}

// State reports the phone's state and the size of the agent's display (0 until it exists).
type State struct {
	Type  string `json:"type"`
	State string `json:"state"`
	W     int    `json:"w,omitempty"`
	H     int    `json:"h,omitempty"`
	// Problem is why the phone is in setup when its last attempt to start the helper failed.
	Problem string `json:"problem,omitempty"`
}

// Event is something the person did on the phone.
type Event struct {
	Type string `json:"type"`
	Kind string `json:"kind"`
	// ID answers a handoff request (EventHandedOff) and is empty for the other kinds.
	ID string `json:"id,omitempty"`
}

// Request is one thing for the phone to do. Worker names who asked, for the phone's own notice.
type Request struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	Op         string          `json:"op"`
	Worker     string          `json:"worker,omitempty"`
	WorkerName string          `json:"worker_name,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	// Image asks for a screenshot with the answer. Acting ops answer with the screen they
	// leave behind, so the agent need not ask again.
	Image bool `json:"image,omitempty"`
}

type ScreenArgs struct{}
type LaunchArgs struct {
	Package string `json:"package"`
}
type TapArgs struct {
	X    int  `json:"x"`
	Y    int  `json:"y"`
	Long bool `json:"long,omitempty"`
}
type SwipeArgs struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
	// MS is how long the stroke takes; 0 means the phone's default.
	MS int `json:"ms,omitempty"`
}
type TextArgs struct {
	Value string `json:"value"`
	// Replace sets the field's whole text; otherwise Value is added after what is there.
	Replace bool `json:"replace,omitempty"`
}
type KeyArgs struct {
	Name string `json:"name"`
}
type WaitArgs struct {
	// MS to wait; 0 waits for the screen to stop changing, up to a few seconds.
	MS int `json:"ms,omitempty"`
}
type HandoffArgs struct {
	// Reason is one short line shown to the person: "Sign in to Instagram".
	Reason string `json:"reason"`
	// TimeoutMS bounds the wait; 0 means ten minutes.
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

// Result answers one Request.
type Result struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	OK   bool   `json:"ok"`
	Code string `json:"code,omitempty"`
	// Error is one sentence for the agent.
	Error string `json:"error,omitempty"`
	// Screen is what is on the agent's display after the op (nil for apps, release and when the
	// screen cannot be read: see CodeScreenOff, which still carries Image).
	Screen *Screen `json:"screen,omitempty"`
	// Image is a base64 JPEG of the agent's display, when asked for.
	Image string `json:"image,omitempty"`
	Apps  []App  `json:"apps,omitempty"`
	// Note is extra for the agent, such as "the app was already open".
	Note string `json:"note,omitempty"`
}

type App struct {
	Package string `json:"package"`
	Label   string `json:"label"`
}

// Screen is the helper's reading of the agent's display: its windows top first, each with the
// visible nodes that have text, a description, or something to press.
type Screen struct {
	W       int      `json:"w"`
	H       int      `json:"h"`
	Windows []Window `json:"windows"`
}

type Window struct {
	ID      int    `json:"id"`
	Type    int    `json:"type"`
	Layer   int    `json:"layer"`
	Active  bool   `json:"active"`
	Focused bool   `json:"focused"`
	Pkg     string `json:"pkg"`
	Title   string `json:"title"`
	Bounds  [4]int `json:"bounds"`
	Nodes   []Node `json:"nodes"`
}

// Node flag bits, in Node.F.
const (
	FlagClickable     = 1
	FlagLongClickable = 2
	FlagEditable      = 4
	FlagScrollable    = 8
	FlagCheckable     = 16
	FlagChecked       = 32
	FlagEnabled       = 64
	FlagFocused       = 128
	FlagSelected      = 256
	FlagPassword      = 512
)

// Node is one accessibility node in tree order. D is its depth in the window, C its class
// without the package, R its view id without the package, T its text (never a password's),
// X its content description, H its hint, B its bounds [left, top, right, bottom] and F its flags.
type Node struct {
	D int    `json:"d"`
	C string `json:"c"`
	R string `json:"r,omitempty"`
	T string `json:"t,omitempty"`
	X string `json:"x,omitempty"`
	H string `json:"h,omitempty"`
	B [4]int `json:"b"`
	F int    `json:"f"`
}
