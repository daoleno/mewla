package work

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/daoleno/mewla/daemon/agysession"
	"github.com/daoleno/mewla/daemon/platform"
	"github.com/daoleno/mewla/daemon/shellquote"
	"github.com/daoleno/mewla/daemon/statedir"
)

// An Antigravity Session that Mewla starts is not agy's terminal UI. It is
// `mewla agy-session`, a program of Mewla's that runs in the Session's
// terminal and holds `agy acp` (the Agent Client Protocol over stdio) as its
// child. agy's terminal UI has no channel to the outside: nothing tells Mewla
// that a turn ended, what was said or that a tool waits for an answer, and a
// second agy process reading the conversation sees a snapshot, never what the
// first one writes. ACP is agy's documented interface for exactly this: it
// streams every message and tool call as it happens, ends each turn with a
// stop reason and the tokens it used, asks permission as a request with its
// options, lists the models and modes, and takes the Mewla tools as MCP
// servers for the session alone, so nothing is written to the person's
// ~/.gemini.
//
// The program writes what it sees, in order and with the time it saw it, to
// its own log (provider-sessions/antigravity/<id>.jsonl under Mewla's state
// directory). That log is the Session's conversation for the app and for
// Brain, read like any other agent's transcript file. It reports its lifecycle
// to the daemon over a local socket and takes messages, stops, answers and
// model changes back; while the daemon is away (a restart) it carries on and
// tells it everything again when it comes back. In the terminal it draws the
// conversation and a composer, so a person at the terminal can read and type
// too. When agy itself dies the program starts it again on the same
// conversation (session/resume) with the same model and mode.

const (
	antigravitySessionDirName = "antigravity"
	antigravitySessionFlag    = agysession.Flag
	antigravitySessionCommand = agysession.Subcommand
	// antigravityRestartLimit is how many times agy is started again within
	// antigravityRestartWindow before the Session gives up and exits.
	antigravityRestartLimit  = 3
	antigravityRestartWindow = 2 * time.Minute
)

var antigravityStateDir struct {
	sync.Mutex
	dir string
}

// SetAntigravityStateDir records the daemon's state directory, which a launch
// names to the Session program and the readers default to.
func SetAntigravityStateDir(dir string) {
	antigravityStateDir.Lock()
	antigravityStateDir.dir = strings.TrimSpace(dir)
	antigravityStateDir.Unlock()
}

// AntigravityStateDir is the daemon's state directory, "" when unknown.
func AntigravityStateDir() string {
	antigravityStateDir.Lock()
	defer antigravityStateDir.Unlock()
	return antigravityStateDir.dir
}

// AntigravitySessionRoot is where the Sessions keep their logs and state.
func AntigravitySessionRoot(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = AntigravityStateDir()
	}
	if strings.TrimSpace(stateDir) == "" {
		stateDir = os.Getenv("MEWLA_STATE_DIR")
	}
	if strings.TrimSpace(stateDir) == "" {
		home, _ := os.UserHomeDir()
		stateDir = statedir.Default(home)
	}
	return filepath.Join(stateDir, "provider-sessions", antigravitySessionDirName)
}

// AntigravitySessionLogPath is the log of one agy conversation. The log
// belongs to the conversation, not to one run of the program, so a Brain host
// or Worker started again on the same conversation keeps its history.
func AntigravitySessionLogPath(stateDir, conversation string) string {
	return filepath.Join(AntigravitySessionRoot(stateDir), conversation+".jsonl")
}

// AntigravitySessionConversation is the agy conversation the Session on a
// command line is in: the one its program recorded, or the one it was started
// on. "" until agy has opened one.
func AntigravitySessionConversation(command string) string {
	id := AntigravitySessionID(command)
	if id == "" {
		return ""
	}
	state := loadAntigravitySessionState(antigravitySessionStatePath(AntigravitySessionStateDir(command), id))
	if IsAntigravityConversationID(state.Conversation) {
		return state.Conversation
	}
	if conversation := agysession.Option(command, "--conversation"); IsAntigravityConversationID(conversation) {
		return conversation
	}
	return ""
}

// AntigravitySessionLogForCommand is the log the Session on a command line
// writes, "" until agy has opened a conversation.
func AntigravitySessionLogForCommand(command string) string {
	conversation := AntigravitySessionConversation(command)
	if conversation == "" {
		return ""
	}
	return AntigravitySessionLogPath(AntigravitySessionStateDir(command), conversation)
}

func antigravitySessionStatePath(stateDir, id string) string {
	return filepath.Join(AntigravitySessionRoot(stateDir), id+".json")
}

// AntigravityEventsSocket is the daemon's socket the Sessions report to.
func AntigravityEventsSocket(stateDir string) string {
	return platform.SocketPath(filepath.Join(stateDir, "agy-events.sock"))
}

// AntigravitySessionOptions is how one Session runs.
type AntigravitySessionOptions struct {
	ID       string
	Cwd      string
	StateDir string
	// Conversation is the agy conversation to continue when the Session has
	// none of its own yet (a Brain host or Worker resumed by id).
	Conversation string
	Model        string
	Effort       string
	Mode         string
	// SkipPermissions starts agy with --dangerously-skip-permissions.
	SkipPermissions bool
	MCPServers      []ACPMCPServer
	AddDirs         []string
	// Binary is agy; empty finds it on PATH.
	Binary string
	// Socket is the daemon's events socket; empty is AntigravityEventsSocket.
	Socket string
	// Terminal is where the conversation is drawn and typed; nil uses stdio.
	In  io.Reader
	Out io.Writer
}

// ACPMCPServer is one MCP server given to agy for this session only.
type ACPMCPServer struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     []ACPMCPServerEnv `json:"env"`
}

// ACPMCPServerEnv is one environment variable of an MCP server.
type ACPMCPServerEnv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// antigravitySessionState is what survives the program: the conversation and
// the choices agy does not keep with it (a resumed session starts on agy's
// default model and mode).
type antigravitySessionState struct {
	Conversation string `json:"conversation,omitempty"`
	Model        string `json:"model,omitempty"`
	Effort       string `json:"effort,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Turns        int    `json:"turns,omitempty"`
}

// AntigravityLogRecord is one line of a Session's log.
type AntigravityLogRecord struct {
	// T: session (agy started on a conversation), user (a message sent to
	// agy), update (an ACP session/update), turn_end, ask (agy waits on an
	// answer), answer, agent_exit (agy stopped), note (the program's own words),
	// agent_turn (agy went on by itself after a turn, starting one of its own).
	T            string          `json:"t"`
	At           string          `json:"at"`
	Conversation string          `json:"conversation,omitempty"`
	Turn         string          `json:"turn,omitempty"`
	Text         string          `json:"text,omitempty"`
	Update       json.RawMessage `json:"update,omitempty"`
	Stop         string          `json:"stop,omitempty"`
	Usage        json.RawMessage `json:"usage,omitempty"`
	Error        string          `json:"error,omitempty"`
	Ask          *AntigravityAsk `json:"ask,omitempty"`
	Option       string          `json:"option,omitempty"`
	By           string          `json:"by,omitempty"`
	Model        string          `json:"model,omitempty"`
	Effort       string          `json:"effort,omitempty"`
	Mode         string          `json:"mode,omitempty"`
	Resumed      bool            `json:"resumed,omitempty"`
	Version      string          `json:"version,omitempty"`
	// Answers names the question a user record answers (picked in the app);
	// Chat shows the answer on the question, not as a message.
	Answers string `json:"answers,omitempty"`
	// Cwd is the folder agy works in (session records, for Stats).
	Cwd string `json:"cwd,omitempty"`
	// Replayed marks history agy replayed when Mewla first opened a
	// conversation it had not logged; its times are when it was imported.
	Replayed bool `json:"replayed,omitempty"`
}

// AntigravityAsk is a decision agy waits on: a tool permission (answered as
// the ACP request's outcome) or a question it asked with its ask_question
// tool (answered as the next message).
type AntigravityAsk struct {
	ID      string                 `json:"id"`
	Kind    string                 `json:"kind"`
	Tool    string                 `json:"tool,omitempty"`
	Summary string                 `json:"summary,omitempty"`
	Options []AntigravityAskOption `json:"options"`
	// Multi says a question takes more than one option.
	Multi bool `json:"multi,omitempty"`
}

// AntigravityAskOption is one answer agy offers.
type AntigravityAskOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Scope string `json:"scope,omitempty"`
}

// IsAntigravitySessionID reports a Session id Mewla made.
func IsAntigravitySessionID(id string) bool { return agysession.IsID(id) }

// NewAntigravitySessionID names a new Session.
func NewAntigravitySessionID() string { return "agy-" + uuid.NewString() }

// AntigravitySessionID is the Session id on a `mewla agy-session` command line.
func AntigravitySessionID(command string) string { return agysession.ID(command) }

// IsAntigravitySessionCommand reports a `mewla agy-session` command line.
func IsAntigravitySessionCommand(command string) bool { return agysession.IsCommand(command) }

// AntigravitySessionStateDir is the --state-dir of an agy-session command line.
func AntigravitySessionStateDir(command string) string {
	return agysession.Option(command, "--state-dir")
}

// antigravityLaunchOptions are the agy flags an agy-session takes over, read
// from an `agy …` command.
type antigravityLaunchOptions struct {
	conversation string
	model        string
	effort       string
	mode         string
	skip         bool
	addDirs      []string
	mcpConfig    string
}

// AntigravitySessionLaunchCommand turns an `agy [flags]` launch into the
// Session program that holds agy over ACP. ok is false for a command it does
// not take over (a subcommand, print mode, an environment prefix, a flag it
// does not know): that command is launched as it is, as agy's own terminal UI.
// mewla is the program to run, stateDir the daemon's state directory.
func AntigravitySessionLaunchCommand(command, mewla, stateDir string) (string, bool) {
	options, ok := parseAntigravityLaunch(command)
	if !ok || strings.TrimSpace(mewla) == "" {
		return command, false
	}
	parts := []string{shellquote.Word(mewla), antigravitySessionCommand, antigravitySessionFlag, NewAntigravitySessionID()}
	if strings.TrimSpace(stateDir) != "" {
		parts = append(parts, "--state-dir", shellquote.Word(stateDir))
	}
	if options.conversation != "" {
		parts = append(parts, "--conversation", options.conversation)
	}
	if options.model != "" {
		parts = append(parts, "--model", shellquote.Word(options.model))
	}
	if options.effort != "" {
		parts = append(parts, "--effort", shellquote.Word(options.effort))
	}
	if options.mode != "" {
		parts = append(parts, "--mode", shellquote.Word(options.mode))
	}
	if options.skip {
		parts = append(parts, "--dangerously-skip-permissions")
	}
	for _, dir := range options.addDirs {
		parts = append(parts, "--add-dir", shellquote.Word(dir))
	}
	if options.mcpConfig != "" {
		parts = append(parts, "--mcp-config", shellquote.Word(options.mcpConfig))
	}
	return strings.Join(parts, " "), true
}

func parseAntigravityLaunch(command string) (antigravityLaunchOptions, bool) {
	var options antigravityLaunchOptions
	fields, ok := shellquote.Fields(command)
	if !ok || len(fields) == 0 || filepath.Base(fields[0]) != "agy" {
		return options, false
	}
	for index := 1; index < len(fields); index++ {
		field := fields[index]
		name, value, inline := strings.Cut(field, "=")
		takes := func() (string, bool) {
			if inline {
				return value, true
			}
			if index+1 >= len(fields) {
				return "", false
			}
			index++
			return fields[index], true
		}
		switch name {
		case "--dangerously-skip-permissions":
			if inline {
				return options, false
			}
			options.skip = true
		case "--conversation", "--model", "--effort", "--mode", "--add-dir", "--settings", "--mcp-config":
			v, ok := takes()
			if !ok {
				return options, false
			}
			switch name {
			case "--conversation":
				if !IsAntigravityConversationID(v) {
					return options, false
				}
				options.conversation = v
			case "--model":
				options.model = v
			case "--effort":
				options.effort = v
			case "--mode":
				options.mode = v
			case "--add-dir":
				options.addDirs = append(options.addDirs, v)
			case "--mcp-config":
				options.mcpConfig = v
			case "--settings":
				// The settings an inline --settings carries are for agy's
				// terminal UI (folder trust, review gates); the Session's agy
				// has no folder dialog and takes permissions from the flag.
			}
		default:
			return options, false
		}
	}
	return options, true
}

// ParseACPMCPConfig reads a Claude-style {"mcpServers":{name:{command,args,env}}}
// object into ACP's list form.
func ParseACPMCPConfig(raw string) ([]ACPMCPServer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var config struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			EnvFrom []string          `json:"envFrom"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return nil, fmt.Errorf("MCP servers are not valid JSON: %w", err)
	}
	var out []ACPMCPServer
	for name, server := range config.MCPServers {
		if strings.TrimSpace(server.Command) == "" {
			continue
		}
		entry := ACPMCPServer{Name: name, Command: server.Command, Args: append([]string{}, server.Args...), Env: []ACPMCPServerEnv{}}
		for key, value := range server.Env {
			entry.Env = append(entry.Env, ACPMCPServerEnv{Name: key, Value: value})
		}
		for _, key := range server.EnvFrom {
			if value, ok := os.LookupEnv(key); ok {
				entry.Env = append(entry.Env, ACPMCPServerEnv{Name: key, Value: value})
			}
		}
		out = append(out, entry)
	}
	sortACPServers(out)
	return out, nil
}

func sortACPServers(servers []ACPMCPServer) {
	for i := 1; i < len(servers); i++ {
		for j := i; j > 0 && servers[j].Name < servers[j-1].Name; j-- {
			servers[j], servers[j-1] = servers[j-1], servers[j]
		}
	}
}

// antigravityLog appends records to the log of the conversation the Session
// is in; until agy has opened one there is nowhere to write.
type antigravityLog struct {
	mu   sync.Mutex
	file *os.File
	path string
	now  func() time.Time
	// replayed marks the records of a history imported from agy.
	replayed bool
}

// open makes path the log; a log already open on it is kept.
func (l *antigravityLog) open(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.path == path && l.file != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	l.file, l.path = file, path
	return nil
}

func (l *antigravityLog) setReplayed(replayed bool) {
	l.mu.Lock()
	l.replayed = replayed
	l.mu.Unlock()
}

func (l *antigravityLog) write(record AntigravityLogRecord) {
	if l == nil {
		return
	}
	if record.At == "" {
		record.At = l.now().UTC().Format(time.RFC3339Nano)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	record.Replayed = record.Replayed || l.replayed
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	_, _ = l.file.Write(append(line, '\n'))
}

func (l *antigravityLog) close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

func loadAntigravitySessionState(path string) antigravitySessionState {
	var state antigravitySessionState
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &state)
	}
	return state
}

func saveAntigravitySessionState(path string, state antigravitySessionState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// antigravityModels is what agy offers for a session.
type antigravityModels struct {
	Current   string                 `json:"current"`
	Models    []antigravityModelInfo `json:"models"`
	Effort    string                 `json:"effort,omitempty"`
	Efforts   []antigravityChoice    `json:"efforts,omitempty"`
	Mode      string                 `json:"mode,omitempty"`
	Modes     []antigravityChoice    `json:"modes,omitempty"`
	ModelBase string                 `json:"model_base,omitempty"`
	Bases     []antigravityChoice    `json:"bases,omitempty"`
}

type antigravityModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type antigravityChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// sessionSetup is the answer to session/new, session/resume and session/load.
type sessionSetup struct {
	SessionID string `json:"sessionId"`
	Models    struct {
		Current   string `json:"currentModelId"`
		Available []struct {
			ModelID string `json:"modelId"`
			Name    string `json:"name"`
		} `json:"availableModels"`
	} `json:"models"`
	Modes struct {
		Current   string `json:"currentModeId"`
		Available []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"availableModes"`
	} `json:"modes"`
	ConfigOptions []acpConfigOption `json:"configOptions"`
}

type acpConfigOption struct {
	ID           string `json:"id"`
	Category     string `json:"category"`
	CurrentValue string `json:"currentValue"`
	Options      []struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	} `json:"options"`
}

func (m *antigravityModels) apply(setup sessionSetup) {
	m.Current = firstNonEmpty(setup.Models.Current, m.Current)
	if len(setup.Models.Available) > 0 {
		m.Models = m.Models[:0]
		for _, model := range setup.Models.Available {
			m.Models = append(m.Models, antigravityModelInfo{ID: model.ModelID, Name: model.Name})
		}
	}
	if len(setup.Modes.Available) > 0 {
		m.Mode = firstNonEmpty(setup.Modes.Current, m.Mode)
		m.Modes = m.Modes[:0]
		for _, mode := range setup.Modes.Available {
			m.Modes = append(m.Modes, antigravityChoice{ID: mode.ID, Name: mode.Name})
		}
	}
	m.applyConfig(setup.ConfigOptions)
}

func (m *antigravityModels) applyConfig(options []acpConfigOption) {
	for _, option := range options {
		var choices []antigravityChoice
		for _, value := range option.Options {
			choices = append(choices, antigravityChoice{ID: value.Value, Name: value.Name})
		}
		switch option.ID {
		case "thought_level":
			m.Effort, m.Efforts = option.CurrentValue, choices
		case "mode":
			m.Mode, m.Modes = option.CurrentValue, choices
		case "model":
			m.ModelBase, m.Bases = option.CurrentValue, choices
		}
	}
	// The full id is the family at its thinking level, when agy lists one.
	for _, model := range m.Models {
		if m.ModelBase != "" && (model.ID == m.ModelBase+"-"+m.Effort || model.ID == m.ModelBase) {
			m.Current = model.ID
		}
	}
}

// setFull takes a full model id agy accepted (a family at a thinking level,
// "gemini-4-neon-low"): agy's own config update for it can come after the
// reply, so the family and level are read from the id at once.
func (m *antigravityModels) setFull(id string) {
	m.Current = id
	for _, base := range m.Bases {
		level, ok := strings.CutPrefix(id, base.ID+"-")
		if !ok {
			continue
		}
		for _, effort := range m.Efforts {
			if effort.ID == level {
				m.ModelBase, m.Effort = base.ID, level
				return
			}
		}
	}
}

// antigravitySession is one running Session program.
type antigravitySession struct {
	opts  AntigravitySessionOptions
	now   func() time.Time
	log   *antigravityLog
	state antigravitySessionState
	view  *antigravityView
	link  *antigravityLink

	mu      sync.Mutex
	agent   *acpClient
	running bool
	turn    string
	// agentTurn marks a turn agy started by itself (agentStep); quiet
	// settles it once agy has gone quiet after reporting the step's usage.
	agentTurn bool
	quiet     *time.Timer
	queue     []antigravityQueued
	asks      map[string]*antigravityPending
	models    antigravityModels
	version   string
	toolNames map[string]string
	ctx       context.Context
}

type antigravityQueued struct {
	id   string
	text string
	// answers is the question this message answers (an answer picked in
	// the app), "" for a message of the person's own.
	answers string
}

type antigravityPending struct {
	ask AntigravityAsk
	// rpcID is the ACP request to answer; empty for a question.
	rpcID json.RawMessage
	// callID is the question's tool call.
	callID string
	// native maps Mewla's option ids to agy's.
	native map[string]string
}

// RunAntigravitySession runs one Session until its terminal closes, the
// context ends or agy cannot be kept running.
func RunAntigravitySession(ctx context.Context, opts AntigravitySessionOptions) error {
	if !IsAntigravitySessionID(opts.ID) {
		return fmt.Errorf("invalid Antigravity Session identity")
	}
	if opts.StateDir == "" {
		opts.StateDir = filepath.Dir(filepath.Dir(AntigravitySessionRoot("")))
	}
	if opts.Socket == "" {
		opts.Socket = AntigravityEventsSocket(opts.StateDir)
	}
	if opts.Binary == "" {
		path, err := exec.LookPath("agy")
		if err != nil {
			return fmt.Errorf("Antigravity (agy) is not installed on this computer")
		}
		opts.Binary = path
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	root := AntigravitySessionRoot(opts.StateDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	unlock, err := lockDSHSession(filepath.Join(root, opts.ID+".lock"))
	if err != nil {
		return fmt.Errorf("this Antigravity Session is already open elsewhere: %w", err)
	}
	defer unlock()
	logFile := &antigravityLog{now: time.Now}
	defer logFile.close()
	s := &antigravitySession{
		opts:      opts,
		now:       time.Now,
		log:       logFile,
		state:     loadAntigravitySessionState(antigravitySessionStatePath(opts.StateDir, opts.ID)),
		asks:      map[string]*antigravityPending{},
		toolNames: map[string]string{},
		ctx:       ctx,
	}
	// The launch's choices win over what an earlier run of the same Session
	// kept only when the Session has none yet: a model chosen in the app since
	// then is the person's later choice.
	if s.state.Model == "" {
		s.state.Model = opts.Model
	}
	if s.state.Effort == "" {
		s.state.Effort = opts.Effort
	}
	if s.state.Mode == "" {
		s.state.Mode = opts.Mode
	}
	if s.state.Conversation == "" {
		s.state.Conversation = opts.Conversation
	}
	s.view = newAntigravityView(opts.Out)
	s.link = newAntigravityLink(opts.Socket, opts.ID, s)
	linkCtx, stopLink := context.WithCancel(ctx)
	defer stopLink()
	go s.link.run(linkCtx)

	exited := make(chan error, 1)
	go func() { exited <- s.supervise(ctx) }()
	terminalDone := make(chan error, 1)
	go func() { terminalDone <- s.view.runInput(ctx, opts.In, s) }()
	select {
	case err := <-exited:
		s.view.note("Antigravity stopped: " + errorText(err))
		return err
	case err := <-terminalDone:
		s.shutdown()
		return err
	case <-ctx.Done():
		s.shutdown()
		return ctx.Err()
	}
}

func errorText(err error) string {
	if err == nil {
		return "it exited"
	}
	return err.Error()
}

func (s *antigravitySession) shutdown() {
	s.mu.Lock()
	agent := s.agent
	s.mu.Unlock()
	if agent != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		agent.Close(ctx)
		cancel()
	}
	s.link.send(map[string]any{"t": "bye"})
}

// supervise keeps agy running on the Session's conversation.
func (s *antigravitySession) supervise(ctx context.Context) error {
	var starts []time.Time
	for {
		now := s.now()
		recent := starts[:0]
		for _, at := range starts {
			if now.Sub(at) < antigravityRestartWindow {
				recent = append(recent, at)
			}
		}
		starts = recent
		if len(starts) >= antigravityRestartLimit {
			return fmt.Errorf("agy stopped %d times in %s", len(starts), antigravityRestartWindow)
		}
		starts = append(starts, now)
		agent, err := s.start(ctx)
		if err != nil {
			s.log.write(AntigravityLogRecord{T: "agent_exit", Error: err.Error()})
			s.view.note("Antigravity could not start: " + err.Error())
			if !s.waitRestart(ctx) {
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-agent.Done():
		}
		reason := "agy exited"
		if exitErr := agent.ExitErr(); exitErr != nil {
			reason = "agy exited: " + exitErr.Error()
		}
		s.agentGone(agent, reason)
		s.view.note("Antigravity stopped; starting it again on the same conversation.")
		if !s.waitRestart(ctx) {
			return ctx.Err()
		}
	}
}

func (s *antigravitySession) waitRestart(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Second):
		return true
	}
}

// agentGone settles what the dead agy left open: a running turn ended without
// an answer, and every pending decision is withdrawn.
func (s *antigravitySession) agentGone(agent *acpClient, reason string) {
	s.mu.Lock()
	if s.agent == agent {
		s.agent = nil
	}
	turn, running := s.turn, s.running
	s.running = false
	s.endAgentTurnLocked()
	asks := s.asks
	s.asks = map[string]*antigravityPending{}
	s.mu.Unlock()
	s.log.write(AntigravityLogRecord{T: "agent_exit", Error: reason, Turn: turn})
	for id := range asks {
		s.log.write(AntigravityLogRecord{T: "answer", Ask: &AntigravityAsk{ID: id}, By: "withdrawn"})
		s.link.send(map[string]any{"t": "resolved", "id": id, "by": "withdrawn"})
	}
	if running {
		s.log.write(AntigravityLogRecord{T: "turn_end", Turn: turn, Stop: "error", Error: reason})
		s.link.send(map[string]any{"t": "turn_end", "turn": turn, "stop": "error", "error": reason})
	}
}

// start runs agy and opens the Session's conversation on it.
func (s *antigravitySession) start(ctx context.Context) (*acpClient, error) {
	var args []string
	if s.opts.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	for _, dir := range s.opts.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	stderr, _ := os.OpenFile(filepath.Join(AntigravitySessionRoot(s.opts.StateDir), s.opts.ID+".agy.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	agent, err := startACPClient(s.opts.Binary, s.opts.Cwd, args, nil, stderr, s.onNotify, s.onRequest)
	if err != nil {
		if stderr != nil {
			stderr.Close()
		}
		return nil, err
	}
	go func() {
		<-agent.Done()
		if stderr != nil {
			stderr.Close()
		}
	}()
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var initialized struct {
		ProtocolVersion int `json:"protocolVersion"`
		AgentInfo       struct {
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	if err := agent.Call(callCtx, "initialize", map[string]any{
		"protocolVersion":    antigravityACPProtocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
	}, &initialized); err != nil {
		agent.Close(callCtx)
		return nil, fmt.Errorf("agy did not start its session server: %w", err)
	}
	if initialized.ProtocolVersion != antigravityACPProtocolVersion {
		agent.Close(callCtx)
		return nil, fmt.Errorf("agy speaks session protocol %d; Mewla speaks %d", initialized.ProtocolVersion, antigravityACPProtocolVersion)
	}
	servers := s.opts.MCPServers
	if servers == nil {
		servers = []ACPMCPServer{}
	}
	var setup sessionSetup
	resumed := false
	s.mu.Lock()
	conversation := s.state.Conversation
	s.mu.Unlock()
	if conversation != "" {
		path := AntigravitySessionLogPath(s.opts.StateDir, conversation)
		_, statErr := os.Stat(path)
		logged := statErr == nil
		if err := s.log.open(path); err != nil {
			agent.Close(callCtx)
			return nil, err
		}
		method := "session/resume"
		if !logged {
			// A conversation Mewla has not logged (begun in agy's own UI, or
			// before this Session): agy replays it once into the log.
			method = "session/load"
			s.log.setReplayed(true)
		}
		err := agent.Call(callCtx, method, map[string]any{"sessionId": conversation, "cwd": s.opts.Cwd, "mcpServers": servers}, &setup)
		s.log.setReplayed(false)
		if err != nil {
			s.view.note("Could not reopen the earlier conversation (" + err.Error() + "); starting a new one.")
			s.log.write(AntigravityLogRecord{T: "note", Text: "Could not reopen conversation " + conversation + ": " + err.Error()})
			conversation = ""
		} else {
			resumed = true
		}
	}
	if conversation == "" {
		if err := agent.Call(callCtx, "session/new", map[string]any{"cwd": s.opts.Cwd, "mcpServers": servers}, &setup); err != nil {
			agent.Close(callCtx)
			return nil, fmt.Errorf("agy could not open a conversation: %w", err)
		}
	}
	if strings.TrimSpace(setup.SessionID) == "" {
		setup.SessionID = conversation
	}
	if err := s.log.open(AntigravitySessionLogPath(s.opts.StateDir, setup.SessionID)); err != nil {
		agent.Close(callCtx)
		return nil, err
	}
	turns := antigravityLoggedTurns(AntigravitySessionLogPath(s.opts.StateDir, setup.SessionID))
	s.mu.Lock()
	s.agent = agent
	s.version = initialized.AgentInfo.Version
	s.models.apply(setup)
	if s.state.Conversation != setup.SessionID || s.state.Turns < turns {
		// Turn numbers go on from the conversation's own log.
		s.state.Turns = turns
	}
	s.state.Conversation = setup.SessionID
	state := s.state
	s.mu.Unlock()
	// agy does not keep the model, thinking level or mode with a conversation:
	// a resumed one starts on agy's defaults, so the Session's own choices are
	// applied again on every start.
	if err := s.applyChoices(callCtx, state); err != nil {
		s.view.note("Could not apply the model: " + err.Error())
	}
	_ = saveAntigravitySessionState(antigravitySessionStatePath(s.opts.StateDir, s.opts.ID), s.snapshotState())
	s.mu.Lock()
	models := s.models
	s.mu.Unlock()
	s.log.write(AntigravityLogRecord{T: "session", Conversation: setup.SessionID, Resumed: resumed, Model: models.Current, Effort: models.Effort, Mode: models.Mode, Version: initialized.AgentInfo.Version, Cwd: s.opts.Cwd})
	s.view.header(s.opts.Cwd, modelName(models), resumed)
	s.link.hello()
	s.drain()
	return agent, nil
}

func modelName(models antigravityModels) string {
	for _, model := range models.Models {
		if model.ID == models.Current {
			return model.Name
		}
	}
	return models.Current
}

func (s *antigravitySession) snapshotState() antigravitySessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// applyChoices sets the model, thinking level and mode agy should use.
func (s *antigravitySession) applyChoices(ctx context.Context, state antigravitySessionState) error {
	s.mu.Lock()
	agent, conversation := s.agent, s.state.Conversation
	s.mu.Unlock()
	if agent == nil {
		return errACPClosed
	}
	var errs []error
	if state.Model != "" {
		s.mu.Lock()
		family := false
		for _, base := range s.models.Bases {
			family = family || base.ID == state.Model
		}
		s.mu.Unlock()
		if family {
			// A model family the app picked (its thinking level is its own
			// choice, below).
			var result struct {
				ConfigOptions []acpConfigOption `json:"configOptions"`
			}
			if err := agent.Call(ctx, "session/set_config_option", map[string]any{"sessionId": conversation, "configId": "model", "value": state.Model}, &result); err != nil {
				errs = append(errs, fmt.Errorf("model %s: %w", state.Model, err))
			} else {
				s.mu.Lock()
				s.models.applyConfig(result.ConfigOptions)
				s.mu.Unlock()
			}
		} else if err := agent.Call(ctx, "session/set_model", map[string]any{"sessionId": conversation, "modelId": state.Model}, nil); err != nil {
			errs = append(errs, fmt.Errorf("model %s: %w", state.Model, err))
		} else {
			s.mu.Lock()
			s.models.setFull(state.Model)
			s.mu.Unlock()
		}
	}
	if state.Effort != "" {
		var result struct {
			ConfigOptions []acpConfigOption `json:"configOptions"`
		}
		if err := agent.Call(ctx, "session/set_config_option", map[string]any{"sessionId": conversation, "configId": "thought_level", "value": state.Effort}, &result); err != nil {
			errs = append(errs, fmt.Errorf("thinking level %s: %w", state.Effort, err))
		} else {
			s.mu.Lock()
			s.models.Effort = state.Effort
			s.models.applyConfig(result.ConfigOptions)
			s.mu.Unlock()
		}
	}
	if state.Mode != "" {
		if err := agent.Call(ctx, "session/set_mode", map[string]any{"sessionId": conversation, "modeId": state.Mode}, nil); err != nil {
			errs = append(errs, fmt.Errorf("mode %s: %w", state.Mode, err))
		} else {
			s.mu.Lock()
			s.models.Mode = state.Mode
			s.mu.Unlock()
		}
	}
	return errors.Join(errs...)
}

// setChoices is a model change from the app: applied to agy now and kept for
// every later start.
func (s *antigravitySession) setChoices(ctx context.Context, model, effort, mode string) error {
	s.mu.Lock()
	next := antigravitySessionState{Model: strings.TrimSpace(model), Effort: strings.TrimSpace(effort), Mode: strings.TrimSpace(mode)}
	s.mu.Unlock()
	if err := s.applyChoices(ctx, next); err != nil {
		return err
	}
	s.mu.Lock()
	if next.Model != "" {
		s.state.Model = next.Model
	}
	if next.Effort != "" {
		s.state.Effort = next.Effort
	}
	if next.Mode != "" {
		s.state.Mode = next.Mode
	}
	models := s.models
	s.mu.Unlock()
	_ = saveAntigravitySessionState(antigravitySessionStatePath(s.opts.StateDir, s.opts.ID), s.snapshotState())
	s.log.write(AntigravityLogRecord{T: "session", Conversation: s.snapshotState().Conversation, Model: models.Current, Effort: models.Effort, Mode: models.Mode, Version: s.version, Cwd: s.opts.Cwd})
	s.view.note("Model: " + modelName(models))
	return nil
}

func (s *antigravitySession) modelsSnapshot() antigravityModels {
	s.mu.Lock()
	defer s.mu.Unlock()
	models := s.models
	models.Models = append([]antigravityModelInfo(nil), s.models.Models...)
	return models
}

// submit queues one message and sends it as soon as agy is free. id is the
// sender's receipt.
func (s *antigravitySession) submit(id, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("an empty message")
	}
	if id == "" {
		id = uuid.NewString()
	}
	return s.enqueue(antigravityQueued{id: id, text: text})
}

// enqueue queues one message for agy; while agy waits on its own question,
// the message is the answer to it.
func (s *antigravitySession) enqueue(next antigravityQueued) error {
	s.mu.Lock()
	// A message while agy waits on its own question is the answer to it.
	var answered []string
	for key, pending := range s.asks {
		if pending.ask.Kind == "question" {
			delete(s.asks, key)
			answered = append(answered, key)
		}
	}
	s.queue = append(s.queue, next)
	s.mu.Unlock()
	for _, key := range answered {
		s.log.write(AntigravityLogRecord{T: "answer", Ask: &AntigravityAsk{ID: key}, By: "message"})
		s.link.send(map[string]any{"t": "resolved", "id": key, "by": "message"})
	}
	if len(answered) > 0 {
		s.view.answered()
	}
	s.drain()
	return nil
}

// drain sends the next queued message while agy has no turn running.
func (s *antigravitySession) drain() {
	s.mu.Lock()
	if s.running && s.agentTurn && s.agent != nil && len(s.queue) > 0 {
		// A message ends the turn agy started by itself: what agy said in it
		// is in the log, and the message is a turn of its own.
		turn := s.turn
		s.mu.Unlock()
		s.finishTurn(turn, "end_turn", nil, "")
		return
	}
	if s.running || s.agent == nil || len(s.queue) == 0 {
		s.mu.Unlock()
		return
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	s.running = true
	s.state.Turns++
	conversation := s.state.Conversation
	s.turn = fmt.Sprintf("%s:%d", conversation, s.state.Turns)
	turn, agent := s.turn, s.agent
	s.mu.Unlock()
	_ = saveAntigravitySessionState(antigravitySessionStatePath(s.opts.StateDir, s.opts.ID), s.snapshotState())
	content, err := dshPromptContentIn(next.text, filepath.Join(s.opts.StateDir, "uploads"))
	if err != nil {
		content = []map[string]any{{"type": "text", "text": next.text}}
	}
	// The message is in the log the moment it goes to agy, once per message,
	// so what Mewla sent and what the conversation shows are the same text.
	s.log.write(AntigravityLogRecord{T: "user", Turn: turn, Text: next.text, Answers: next.answers})
	s.view.user(next.text)
	sum := sha256.Sum256([]byte(next.text))
	s.link.send(map[string]any{"t": "input", "id": next.id, "turn": turn, "sha": fmt.Sprintf("%x", sum)})
	s.link.send(map[string]any{"t": "turn_start", "turn": turn})
	s.view.status("working")
	go func() {
		var result struct {
			StopReason string          `json:"stopReason"`
			Usage      json.RawMessage `json:"usage"`
		}
		err := agent.Call(s.ctx, "session/prompt", map[string]any{"sessionId": conversation, "prompt": content}, &result)
		select {
		case <-agent.Done():
			// agentGone settles the turn.
			return
		default:
		}
		stop, text := result.StopReason, ""
		if err != nil {
			stop, text = "error", err.Error()
		}
		s.finishTurn(turn, stop, result.Usage, text)
	}()
}

func (s *antigravitySession) finishTurn(turn, stop string, usage json.RawMessage, text string) {
	s.mu.Lock()
	if s.turn != turn || !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.endAgentTurnLocked()
	// A permission request does not outlive its turn.
	var withdrawn []string
	for key, pending := range s.asks {
		if pending.ask.Kind != "question" {
			delete(s.asks, key)
			withdrawn = append(withdrawn, key)
		}
	}
	s.mu.Unlock()
	for _, key := range withdrawn {
		s.log.write(AntigravityLogRecord{T: "answer", Ask: &AntigravityAsk{ID: key}, By: "withdrawn"})
		s.link.send(map[string]any{"t": "resolved", "id": key, "by": "withdrawn"})
	}
	if len(withdrawn) > 0 {
		s.view.answered()
	}
	s.log.write(AntigravityLogRecord{T: "turn_end", Turn: turn, Stop: stop, Usage: usage, Error: text})
	s.link.send(map[string]any{"t": "turn_end", "turn": turn, "stop": stop, "error": text, "usage": usage})
	if text != "" {
		s.view.note("The turn failed: " + text)
	}
	s.view.status("done")
	s.drain()
}

// cancel stops the running turn.
func (s *antigravitySession) cancel() bool {
	s.mu.Lock()
	agent, running, conversation := s.agent, s.running, s.state.Conversation
	agentTurn, turn := s.agentTurn, s.turn
	s.mu.Unlock()
	if agent == nil || !running {
		return false
	}
	_ = agent.Notify("session/cancel", map[string]any{"sessionId": conversation})
	if agentTurn {
		// No prompt answers for a turn agy started; it ends here.
		s.finishTurn(turn, "cancelled", nil, "")
	}
	return true
}

// antigravityAgentTurnQuiet is how long agy stays quiet after reporting a
// step's usage before a turn it started by itself counts as settled.
var antigravityAgentTurnQuiet = 2 * time.Second

// agentStep follows agy working by itself, outside any prompt: a command it
// left running in the background finishes and agy goes on to answer
// (measured on agy 1.5.14: the tool's completion, then thoughts and text, then
// a usage_update, all after the prompt's end_turn). Such work is a turn of its
// own, reported like any other so Chat, the Session line and a Worker's
// result follow it; it settles once agy has reported the step's usage and
// stays quiet. A late tool completion alone belongs to the earlier turn.
func (s *antigravitySession) agentStep(update antigravityACPUpdate) {
	s.mu.Lock()
	if s.agent == nil {
		s.mu.Unlock()
		return
	}
	if s.agentTurn && s.quiet != nil {
		s.quiet.Stop()
		s.quiet = nil
	}
	if update.Kind == "usage_update" {
		if s.agentTurn {
			turn := s.turn
			s.quiet = time.AfterFunc(antigravityAgentTurnQuiet, func() { s.finishTurn(turn, "end_turn", nil, "") })
		}
		s.mu.Unlock()
		return
	}
	switch update.Kind {
	case "agent_message_chunk", "agent_thought_chunk", "tool_call", "plan":
	default:
		s.mu.Unlock()
		return
	}
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running, s.agentTurn = true, true
	s.state.Turns++
	s.turn = fmt.Sprintf("%s:%d", s.state.Conversation, s.state.Turns)
	turn := s.turn
	s.mu.Unlock()
	_ = saveAntigravitySessionState(antigravitySessionStatePath(s.opts.StateDir, s.opts.ID), s.snapshotState())
	s.log.write(AntigravityLogRecord{T: "agent_turn", Turn: turn})
	s.link.send(map[string]any{"t": "turn_start", "turn": turn})
	s.view.status("working")
}

// endAgentTurnLocked clears an agent-started turn's bookkeeping; the caller
// holds s.mu.
func (s *antigravitySession) endAgentTurnLocked() {
	s.agentTurn = false
	if s.quiet != nil {
		s.quiet.Stop()
		s.quiet = nil
	}
}

func (s *antigravitySession) onNotify(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var envelope struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &envelope) != nil {
		return
	}
	var update antigravityACPUpdate
	if json.Unmarshal(envelope.Update, &update) != nil {
		return
	}
	s.log.mu.Lock()
	replaying := s.log.replayed
	s.log.mu.Unlock()
	if !replaying {
		s.agentStep(update)
	}
	s.mu.Lock()
	turn := s.turn
	if update.Kind == "config_option_update" {
		var options struct {
			ConfigOptions []acpConfigOption `json:"configOptions"`
		}
		if json.Unmarshal(envelope.Update, &options) == nil {
			s.models.applyConfig(options.ConfigOptions)
		}
	}
	if update.Kind == "current_mode_update" {
		var mode struct {
			CurrentModeID string `json:"currentModeId"`
		}
		if json.Unmarshal(envelope.Update, &mode) == nil && mode.CurrentModeID != "" {
			s.models.Mode = mode.CurrentModeID
		}
	}
	s.mu.Unlock()
	switch update.Kind {
	case "available_commands_update", "usage_update":
		// Not part of the conversation.
		return
	}
	s.log.write(AntigravityLogRecord{T: "update", Turn: turn, Update: envelope.Update})
	s.view.update(update)
	if replaying {
		return
	}
	switch update.Kind {
	case "tool_call", "tool_call_update":
		s.mu.Lock()
		if update.Name != "" {
			s.toolNames[update.ToolCallID] = update.Name
		}
		name := s.toolNames[update.ToolCallID]
		s.mu.Unlock()
		if update.Kind == "tool_call" {
			summary := firstNonEmpty(update.Title, name)
			if name == "ask_question" {
				// The Session line says what agy asks, as it does for Claude.
				summary = firstNonEmpty(antigravityQuestionText(update), summary)
			}
			s.link.send(map[string]any{"t": "activity", "turn": turn, "tool": name, "summary": summary})
		} else {
			s.link.send(map[string]any{"t": "progress", "turn": turn})
		}
		if name == "ask_question" && strings.EqualFold(update.Status, "pending") {
			s.question(update)
		}
		if name == "ask_question" && (strings.EqualFold(update.Status, "failed") || strings.EqualFold(update.Status, "completed")) {
			s.questionSettled(update.ToolCallID)
		}
	default:
		s.link.send(map[string]any{"t": "progress", "turn": turn})
	}
}

// question records agy's ask_question as a decision with its options; the
// answer is the next message.
// antigravityQuestionText is the first question of agy's ask_question call.
func antigravityQuestionText(update antigravityACPUpdate) string {
	var args struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	if json.Unmarshal([]byte(firstNonEmptyRaw(update.Args, update.RawInput)), &args) != nil || len(args.Questions) == 0 {
		return ""
	}
	return strings.TrimSpace(args.Questions[0].Question)
}

func (s *antigravitySession) question(update antigravityACPUpdate) {
	var args struct {
		Questions []struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
			Multi    bool     `json:"is_multi_select"`
		} `json:"questions"`
	}
	if json.Unmarshal([]byte(firstNonEmptyRaw(update.Args, update.RawInput)), &args) != nil || len(args.Questions) == 0 {
		return
	}
	first := args.Questions[0]
	ask := AntigravityAsk{ID: "q-" + update.ToolCallID, Kind: "question", Tool: "ask_question", Summary: first.Question, Multi: first.Multi, Options: []AntigravityAskOption{}}
	if len(args.Questions) > 1 {
		var parts []string
		for _, q := range args.Questions {
			parts = append(parts, q.Question)
		}
		ask.Summary = strings.Join(parts, "\n")
	}
	for index, option := range first.Options {
		ask.Options = append(ask.Options, AntigravityAskOption{ID: fmt.Sprintf("%d", index+1), Label: option})
	}
	s.mu.Lock()
	if _, known := s.asks[ask.ID]; known {
		s.mu.Unlock()
		return
	}
	s.asks[ask.ID] = &antigravityPending{ask: ask, callID: update.ToolCallID}
	s.mu.Unlock()
	s.log.write(AntigravityLogRecord{T: "ask", Ask: &ask})
	s.view.ask(ask)
	s.link.send(map[string]any{"t": "ask", "ask": ask})
}

func (s *antigravitySession) questionSettled(callID string) {
	id := "q-" + callID
	s.mu.Lock()
	_, pending := s.asks[id]
	delete(s.asks, id)
	s.mu.Unlock()
	if pending {
		s.log.write(AntigravityLogRecord{T: "answer", Ask: &AntigravityAsk{ID: id}, By: "agent"})
		s.link.send(map[string]any{"t": "resolved", "id": id, "by": "agent"})
		s.view.answered()
	}
}

func (s *antigravitySession) onRequest(id json.RawMessage, method string, params json.RawMessage) {
	s.mu.Lock()
	agent := s.agent
	s.mu.Unlock()
	if method != "session/request_permission" {
		if agent != nil {
			_ = agent.respondError(id, -32601, "method not supported")
		}
		return
	}
	var request struct {
		ToolCall antigravityACPUpdate `json:"toolCall"`
		Options  []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if json.Unmarshal(params, &request) != nil {
		return
	}
	ask := AntigravityAsk{ID: "p-" + request.ToolCall.ToolCallID, Kind: "permission", Tool: antigravityToolLabel(request.ToolCall), Summary: firstNonEmpty(request.ToolCall.Title, request.ToolCall.Name), Options: []AntigravityAskOption{}}
	if ask.ID == "p-" {
		ask.ID = "p-" + uuid.NewString()
	}
	// Mewla names the answers by their kind ("allow", "deny", as every
	// agent's approval does); agy's own option ids are kept to answer with.
	native := map[string]string{}
	for _, option := range request.Options {
		scope, mewlaID := "once", option.OptionID
		switch option.Kind {
		case "allow_once":
			mewlaID = "allow"
		case "allow_always":
			scope, mewlaID = "always", "allow_always"
		case "reject_once":
			scope, mewlaID = "deny", "deny"
		case "reject_always":
			scope, mewlaID = "deny", "deny_always"
		}
		native[mewlaID] = option.OptionID
		ask.Options = append(ask.Options, AntigravityAskOption{ID: mewlaID, Label: option.Name, Scope: scope})
	}
	s.mu.Lock()
	s.asks[ask.ID] = &antigravityPending{ask: ask, rpcID: append(json.RawMessage(nil), id...), native: native}
	s.mu.Unlock()
	s.log.write(AntigravityLogRecord{T: "ask", Ask: &ask})
	s.view.ask(ask)
	s.link.send(map[string]any{"t": "ask", "ask": ask})
}

// answer settles a decision agy waits on. by says who answered (the app,
// Brain, the terminal).
func (s *antigravitySession) answer(id, option, by string) error {
	s.mu.Lock()
	pending := s.asks[id]
	agent := s.agent
	if pending != nil {
		delete(s.asks, id)
	}
	s.mu.Unlock()
	if pending == nil {
		return fmt.Errorf("that decision is not pending")
	}
	s.log.write(AntigravityLogRecord{T: "answer", Ask: &AntigravityAsk{ID: id}, Option: option, By: by})
	s.link.send(map[string]any{"t": "resolved", "id": id, "by": by, "option": option})
	s.view.answered()
	if pending.ask.Kind == "question" {
		label := option
		for _, choice := range pending.ask.Options {
			if choice.ID == option {
				label = choice.Label
			}
		}
		return s.enqueue(antigravityQueued{id: uuid.NewString(), text: label, answers: id})
	}
	if agent == nil {
		return errACPClosed
	}
	valid := false
	for _, choice := range pending.ask.Options {
		valid = valid || choice.ID == option
	}
	if !valid {
		return fmt.Errorf("agy did not offer %q", option)
	}
	if nativeID := pending.native[option]; nativeID != "" {
		option = nativeID
	}
	return agent.respond(pending.rpcID, map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": option}})
}

func (s *antigravitySession) pendingAsks() []AntigravityAsk {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AntigravityAsk
	for _, pending := range s.asks {
		out = append(out, pending.ask)
	}
	return out
}

func (s *antigravitySession) helloFacts() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The conversation is named once agy has it open, not while it starts.
	conversation := ""
	if s.agent != nil {
		conversation = s.state.Conversation
	}
	return map[string]any{
		"t":            "hello",
		"session":      s.opts.ID,
		"conversation": conversation,
		"running":      s.running,
		"turn":         s.turn,
		"log":          AntigravitySessionLogPath(s.opts.StateDir, s.state.Conversation),
		"agent_pid":    pidOf(s.agent),
	}
}

func pidOf(agent *acpClient) int {
	if agent == nil {
		return 0
	}
	return agent.Pid()
}

// dshPromptContentIn is the message as ACP prompt content: the text as
// written (an attachment envelope names its files by path, which agy can read)
// plus every attached image as an image block, so agy sees the picture itself.
// Only files already admitted to Mewla's upload directory become image bytes.
func dshPromptContentIn(text, uploads string) ([]map[string]any, error) {
	return acpPromptContent(text, uploads)
}

func bindACPProcess(cmd *exec.Cmd) { bindDSHProcess(cmd) }

// antigravityToolLabel is what a permission card calls agy's tool: the kind of
// action ACP gives it, in words, never agy's internal tool name.
func antigravityToolLabel(call antigravityACPUpdate) string {
	switch call.ToolKind {
	case "execute":
		return "Command"
	case "edit":
		return "Edit"
	case "delete":
		return "Delete"
	case "move":
		return "Move"
	case "read":
		return "Read"
	case "search":
		return "Search"
	case "fetch":
		return "Web"
	}
	name := strings.ReplaceAll(strings.TrimSpace(call.Name), "_", " ")
	if name == "" {
		return "Tool"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// antigravityLoggedTurns counts the messages already in a conversation's log.
func antigravityLoggedTurns(path string) int {
	records, err := ReadAntigravitySessionLog(path)
	if err != nil {
		return 0
	}
	turns := 0
	for _, record := range records {
		if record.T == "user" || record.T == "agent_turn" {
			turns++
		}
	}
	return turns
}
