package watch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/lifecycle"
)

// Backfill is how far back the first collection reads.
const Backfill = 21 * 24 * time.Hour

// Poll intervals. Local sources are cheap; external sources respect the
// services' rate limits.
const (
	runtimeEvery  = 30 * time.Second
	localEvery    = time.Minute
	externalEvery = 7 * time.Minute
	startupDelay  = 20 * time.Second
)

// SessionLister reports live Sessions and their working directories.
type SessionLister interface {
	Workers() []*classifier.Worker
}

// Options wires a Collector to the daemon's stores.
type Options struct {
	Store      *Store
	Home       string
	LogPath    string
	Works      WorkSource
	Admissions AdmissionSource
	Sessions   SessionLister
	External   ExternalReader
}

// Collector runs the collectors and then the rules. Passes never overlap.
type Collector struct {
	store       *Store
	home        string
	logPath     string
	works       WorkSource
	admissions  AdmissionSource
	sessions    SessionLister
	external    ExternalReader
	transcripts transcriptSource
	now         func() time.Time
	pause       func()

	pass              sync.Mutex
	statesByID        map[string]*lifecycle.State
	githubPausedUntil time.Time
	linearTeams       int

	mu       sync.Mutex
	errors   map[string]string
	lastPass *PassResult
	lastRun  map[string]time.Time
}

// NewCollector creates a Collector.
func NewCollector(options Options) *Collector {
	return &Collector{
		store:       options.Store,
		home:        options.Home,
		logPath:     options.LogPath,
		works:       options.Works,
		admissions:  options.Admissions,
		sessions:    options.Sessions,
		external:    options.External,
		transcripts: defaultTranscripts,
		now:         func() time.Time { return time.Now().UTC() },
		pause:       func() { time.Sleep(2 * time.Millisecond) },
		errors:      map[string]string{},
		lastRun:     map[string]time.Time{},
	}
}

// Store returns the collector's store.
func (c *Collector) Store() *Store { return c.store }

func (c *Collector) horizon() time.Time { return c.now().Add(-Backfill) }

// noteError records a source failure, logging it only when it changes so a
// persistent failure cannot flood the daemon log Watch itself reads.
func (c *Collector) noteError(source string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	message := ""
	if err != nil && !errors.Is(err, context.Canceled) {
		message = err.Error()
	}
	if c.errors[source] == message {
		return
	}
	if message == "" {
		delete(c.errors, source)
		return
	}
	c.errors[source] = message
	log.Printf("WARN watch: %s collection failed: %s", source, message)
}

// Run collects until ctx ends. It never returns an error: a failing source is
// recorded and retried on its next interval.
func (c *Collector) Run(ctx context.Context) error {
	timer := time.NewTimer(startupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		c.Collect(ctx, nil, false)
		timer.Reset(runtimeEvery)
	}
}

// Collect runs one pass. With no sources it runs whichever sources are due;
// force runs the named (or all) sources now.
func (c *Collector) Collect(ctx context.Context, sources []string, force bool) (result PassResult) {
	c.pass.Lock()
	defer c.pass.Unlock()
	result = PassResult{StartedAt: c.now(), Added: map[string]int{}}
	defer func() {
		if recovered := recover(); recovered != nil {
			c.noteError("watch", fmt.Errorf("pass panicked: %v\n%s", recovered, debug.Stack()))
		}
		result.FinishedAt = c.now()
		c.mu.Lock()
		result.Errors = map[string]string{}
		for source, message := range c.errors {
			result.Errors[source] = message
		}
		copied := result
		c.lastPass = &copied
		c.mu.Unlock()
	}()
	wanted := map[string]bool{}
	for _, source := range sources {
		wanted[strings.TrimSpace(source)] = true
	}
	due := func(source string, every time.Duration) bool {
		if len(wanted) > 0 && !wanted[source] {
			return false
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if !force && c.now().Sub(c.lastRun[source]) < every {
			return false
		}
		c.lastRun[source] = c.now()
		return true
	}
	c.statesByID = nil
	if c.sessions != nil {
		for _, worker := range c.sessions.Workers() {
			if worker != nil {
				c.store.Place("session", worker.ID, worker.Cwd)
			}
		}
	}
	run := func(source string, collect func(context.Context) (int, error)) {
		// A cancelled pass stops: a later source must not run on an earlier
		// source's partial results (Process needs Human's turn places).
		if ctx.Err() != nil {
			return
		}
		added, err := collect(ctx)
		result.Added[source] += added
		c.noteError(source, err)
	}
	// Runtime goes first so a long transcript read never delays daemon log
	// lines. Human runs before Process: it learns where each turn ran, which
	// places Work in its project.
	if due(SourceRuntime, runtimeEvery) {
		run(SourceRuntime, c.collectRuntime)
	}
	if due(SourceHuman, localEvery) {
		run(SourceHuman, c.collectHuman)
	}
	if due(SourceProcess, localEvery) {
		run(SourceProcess, c.collectProcess)
	}
	if due(SourceExternal, externalEvery) {
		run(SourceExternal, c.collectExternal)
	}
	if err := c.store.Flush(); err != nil {
		c.noteError("store", err)
	}
	changed, err := evaluateRules(c.store, c.now())
	c.noteError("rules", err)
	result.Problems = changed
	return result
}

// Status summarizes Watch for the CLI.
type Status struct {
	Config      Config                    `json:"config"`
	Signals     int                       `json:"signals"`
	BySource    map[string]map[string]int `json:"by_source"`
	Problems    int                       `json:"problems"`
	LinearTeams int                       `json:"linear_teams"`
	LastPass    *PassResult               `json:"last_pass,omitempty"`
	Errors      map[string]string         `json:"errors,omitempty"`
}

// Status reports counts per source and project and the last pass.
func (c *Collector) Status() Status {
	status := Status{Config: c.store.Config(), BySource: map[string]map[string]int{}}
	for _, signal := range c.store.Signals(Filter{}) {
		status.Signals++
		if status.BySource[signal.Source] == nil {
			status.BySource[signal.Source] = map[string]int{}
		}
		status.BySource[signal.Source][firstNonEmpty(signal.ProjectID, "(none)")]++
	}
	for _, problem := range c.store.Problems(Filter{}) {
		if problem.Active {
			status.Problems++
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	status.LastPass = c.lastPass
	status.LinearTeams = c.linearTeams
	if len(c.errors) > 0 {
		status.Errors = map[string]string{}
		for source, message := range c.errors {
			status.Errors[source] = message
		}
	}
	return status
}

// Request is one control-socket call from the CLI.
type Request struct {
	Action    string   `json:"action"`
	Filter    Filter   `json:"filter"`
	ProjectID string   `json:"project_id,omitempty"`
	Name      string   `json:"name,omitempty"`
	TeamKey   string   `json:"team_key,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	Enabled   bool     `json:"enabled,omitempty"`
}

// Response answers a Request.
type Response struct {
	Status   *Status     `json:"status,omitempty"`
	Projects []Project   `json:"projects,omitempty"`
	Project  *Project    `json:"project,omitempty"`
	Signals  []Signal    `json:"signals,omitempty"`
	Problems []Problem   `json:"problems,omitempty"`
	Pass     *PassResult `json:"pass,omitempty"`
	Config   *Config     `json:"config,omitempty"`
}

// Handle serves a CLI request.
func (c *Collector) Handle(ctx context.Context, request Request) (Response, error) {
	switch request.Action {
	case "status":
		status := c.Status()
		return Response{Status: &status}, nil
	case "projects":
		projects := c.store.Projects()
		if request.ProjectID != "" {
			projects = filterProjects(projects, request.ProjectID)
		}
		return Response{Projects: projects}, nil
	case "signals":
		if request.Filter.Limit == 0 {
			request.Filter.Limit = 50
		}
		return Response{Signals: c.store.Signals(request.Filter)}, nil
	case "problems":
		return Response{Problems: c.store.Problems(request.Filter)}, nil
	case "collect":
		pass := c.Collect(ctx, request.Sources, true)
		return Response{Pass: &pass}, nil
	case "set_enabled":
		config, err := c.store.SetEnabled(request.Enabled)
		return Response{Config: &config}, err
	case "rename":
		name := strings.TrimSpace(request.Name)
		if name == "" {
			return Response{}, errors.New("a project name is required")
		}
		project, err := c.store.UpdateProject(request.ProjectID, func(p *Project) error {
			p.Name, p.Renamed = name, true
			return nil
		})
		return Response{Project: &project}, err
	case "link_linear":
		project, err := c.store.UpdateProject(request.ProjectID, func(p *Project) error {
			if p.Linear == nil {
				return errors.New("no Linear team is suggested for this project yet")
			}
			for _, team := range p.Linear.Suggested {
				if strings.EqualFold(team.Key, request.TeamKey) {
					confirmed := team
					p.Linear.Confirmed = &confirmed
					return nil
				}
			}
			keys := make([]string, 0, len(p.Linear.Suggested))
			for _, team := range p.Linear.Suggested {
				keys = append(keys, team.Key)
			}
			sort.Strings(keys)
			return fmt.Errorf("team %q is not among the suggestions (%s)", request.TeamKey, strings.Join(keys, ", "))
		})
		return Response{Project: &project}, err
	case "unlink_linear":
		project, err := c.store.UpdateProject(request.ProjectID, func(p *Project) error {
			if p.Linear != nil {
				p.Linear.Confirmed = nil
			}
			return nil
		})
		return Response{Project: &project}, err
	default:
		return Response{}, fmt.Errorf("unknown watch action %q", request.Action)
	}
}

func filterProjects(projects []Project, id string) []Project {
	for _, project := range projects {
		if project.ID == id || strings.EqualFold(project.Name, id) {
			return []Project{project}
		}
	}
	return nil
}
