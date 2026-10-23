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

	"github.com/daoleno/mewla/daemon/brain"
	"github.com/daoleno/mewla/daemon/calendar"
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
	// Brain, Push and Thread let Watch reach Brain and the phone. Without
	// Brain, Watch collects and judges but sends nothing.
	Brain  Brain
	Push   PushFunc
	Thread func() string
	// Executor names the Brain host's executor; judgment tries it first.
	Executor func() string
	// RoutingPath is routing.md in the Brain workspace, where the Watch
	// judgment row picks the executor, model and reasoning.
	RoutingPath string
	// Calendar holds the morning brief item. Without it there is no brief.
	Calendar Calendar
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
	judge       *judge
	notify      *notifier
	now         func() time.Time
	pause       func()

	pass              sync.Mutex
	statesByID        map[string]*lifecycle.State
	githubPausedUntil time.Time
	linearTeams       int

	mu       sync.Mutex
	presence func(workerID string) Presence

	// engageDelay is how long a foreground event waits for a tap's report.
	engageDelay time.Duration

	queueOnce sync.Once
	queue     chan func()
	errors    map[string]string
	lastPass  *PassResult
	lastRun   map[string]time.Time
	closedAt  map[string]closedAtEntry
}

// NewCollector creates a Collector.
func NewCollector(options Options) *Collector {
	c := &Collector{
		store:       options.Store,
		home:        options.Home,
		logPath:     options.LogPath,
		works:       options.Works,
		admissions:  options.Admissions,
		sessions:    options.Sessions,
		external:    options.External,
		transcripts: defaultTranscripts,
		judge:       newJudge(options.Store, options.Home),
		now:         func() time.Time { return time.Now().UTC() },
		pause:       func() { time.Sleep(2 * time.Millisecond) },
		errors:      map[string]string{},
		lastRun:     map[string]time.Time{},
		engageDelay: 5 * time.Second,
	}
	c.judge.language = c.language
	c.judge.preferred = options.Executor
	c.judge.routingPath = options.RoutingPath
	if options.Brain != nil {
		notify, err := newNotifier(options.Store, options.Brain, options.Push, options.Thread)
		if err != nil {
			log.Printf("WARN watch: notifications unavailable: %v", err)
		} else {
			c.notify = notify
			notify.calendar = options.Calendar
			notify.language = c.language
		}
	}
	return c
}

// language is the language code Watch writes in: the app's, else the one the
// person's own messages use, else English.
func (c *Collector) language() string {
	if c.notify != nil {
		if code := c.notify.deviceLanguage(); code != "" {
			return code
		}
	}
	if code := c.judge.detectedLanguage(); code != "" {
		return code
	}
	return "en"
}

// SetDevice tells Watch the user's timezone and language.
func (c *Collector) SetDevice(device Device) {
	if c.notify != nil {
		c.notify.SetDevice(device)
	}
}

// BriefDue is what the calendar calls when the morning brief item is due.
func (c *Collector) BriefDue(_ calendar.Item, due time.Time) error {
	if c == nil {
		return errors.New("Watch is not running")
	}
	return c.notify.briefDue(due)
}

// BriefCatchUp is how late the calendar may still fire an occurrence of the brief.
const BriefCatchUp = briefCatchUp

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

// Run collects until ctx ends, and judges in the background while Watch is
// enabled. It never returns an error: a failing source is recorded and
// retried on its next interval.
func (c *Collector) Run(ctx context.Context) error {
	go c.judge.loop(ctx)
	timer := time.NewTimer(startupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		c.Collect(ctx, nil, false)
		c.notify.tick()
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
	if ctx.Err() == nil {
		c.noteError("placement", c.placeFromIndex())
	}
	if err := c.store.Flush(); err != nil {
		c.noteError("store", err)
	}
	c.settleNotifications()
	changed, err := evaluateRules(c.store, c.now())
	c.noteError("rules", err)
	result.Problems = changed
	c.applyLinks()
	return result
}

// Status summarizes Watch for the CLI.
type Status struct {
	Config      Config                    `json:"config"`
	Signals     int                       `json:"signals"`
	BySource    map[string]map[string]int `json:"by_source"`
	Problems    int                       `json:"problems"`
	Placed      map[string]Placement      `json:"placed"`
	Judgment    JudgeStatus               `json:"judgment"`
	LinearTeams int                       `json:"linear_teams"`
	LastPass    *PassResult               `json:"last_pass,omitempty"`
	Errors      map[string]string         `json:"errors,omitempty"`
}

// Status reports counts per source and project and the last pass.
func (c *Collector) Status() Status {
	status := Status{Config: c.store.Config(), BySource: map[string]map[string]int{}, Placed: map[string]Placement{}}
	for _, signal := range c.store.Signals(Filter{}) {
		status.Signals++
		placed := status.Placed[signal.Source]
		placed.Total++
		switch {
		case signal.ProjectID == "":
		case signal.PlacedBy == PlacedByIndex:
			placed.ByIndex++
		case signal.PlacedBy == PlacedByJudgment:
			placed.ByJudgment++
		default:
			placed.AtCollection++
		}
		status.Placed[signal.Source] = placed
		if status.BySource[signal.Source] == nil {
			status.BySource[signal.Source] = map[string]int{}
		}
		status.BySource[signal.Source][firstNonEmpty(signal.ProjectID, "(none)")]++
	}
	for _, problem := range c.store.Problems(Filter{}) {
		if problem.Active && problem.MergedInto == "" {
			status.Problems++
		}
	}
	status.Judgment = c.judge.status()
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

// Placement counts how many of a source's signals have a project, and how
// they got it.
type Placement struct {
	Total        int `json:"total"`
	AtCollection int `json:"at_collection"`
	ByIndex      int `json:"by_index"`
	ByJudgment   int `json:"by_judgment"`
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
	ProblemID string   `json:"problem_id,omitempty"`
	Label     string   `json:"label,omitempty"`
	Note      string   `json:"note,omitempty"`
	Batches   int      `json:"batches,omitempty"`
	WorkID    string   `json:"work_id,omitempty"`
	By        string   `json:"by,omitempty"`
	Backlog   bool     `json:"backlog,omitempty"`
	// For is how long a "later" label rests: 90m, 12h, 1d, 2w.
	For string `json:"for,omitempty"`
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
	Problem  *Problem    `json:"problem,omitempty"`
	Runs     []JudgeRun  `json:"runs,omitempty"`
	// Sent is how many problems a triage or brief handed to Brain.
	Sent *int `json:"sent,omitempty"`
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
	case "judge":
		runs, err := c.judge.runBatches(ctx, request.Batches)
		return Response{Runs: runs}, err
	case "label":
		var rest time.Duration
		if strings.TrimSpace(request.For) != "" {
			var err error
			if rest, err = ParseRest(request.For); err != nil {
				return Response{}, err
			}
		}
		problem, err := c.store.LabelProblem(request.ProblemID, request.Label, request.Note, request.By, rest)
		return Response{Problem: &problem}, err
	case "triage":
		if c.notify == nil {
			return Response{}, errors.New("Watch cannot reach Brain")
		}
		if !c.store.Config().Enabled {
			return Response{}, errors.New("Watch is off; run mewla watch enable first")
		}
		sent := c.notify.Triage(request.Backlog)
		return Response{Sent: &sent}, nil
	case "brief":
		if c.notify == nil {
			return Response{}, errors.New("Watch cannot reach Brain")
		}
		if !c.store.Config().Enabled {
			return Response{}, errors.New("Watch is off; run mewla watch enable first")
		}
		sent, err := c.notify.Brief()
		return Response{Sent: &sent}, err
	case "link":
		if err := c.requireWork(request.WorkID); err != nil {
			return Response{}, err
		}
		by := LinkUser
		if request.By == LabelByBrain {
			by = LinkBrain
		}
		problem, err := c.store.LinkWork(request.ProblemID, request.WorkID, by)
		if err == nil {
			c.applyLinks()
			problem = c.store.Problem(request.ProblemID)
		}
		return Response{Problem: &problem}, err
	case "unlink":
		problem, err := c.store.UnlinkWork(request.ProblemID, request.WorkID)
		if err == nil {
			c.applyLinks()
			problem = c.store.Problem(request.ProblemID)
		}
		return Response{Problem: &problem}, err
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

// placeFromIndex places signals collected before Watch knew where their
// turn, Session or Work ran. It is deterministic and free; judgment places
// what remains by content.
func (c *Collector) placeFromIndex() error {
	changes := map[string]Annotation{}
	for _, signal := range c.store.Unplaced() {
		for _, place := range []struct{ kind, key string }{
			{"turn", signal.Refs["turn"]}, {"work", signal.Refs["work"]}, {"session", signal.Refs["session"]},
		} {
			if place.key == "" {
				continue
			}
			cwd := c.store.PlaceOf(place.kind, place.key)
			if cwd == "" || cwd == signal.Refs["cwd"] {
				continue
			}
			if projectID, ok := c.store.ProjectForCWD(cwd, signal.Time); ok {
				changes[signal.ID] = Annotation{ProjectID: projectID, PlacedBy: PlacedByIndex, PlaceReason: "its " + place.kind + " ran in " + cwd}
				break
			}
		}
	}
	return c.store.Annotate(changes)
}

// applyLinks ties problems to Work and moves them by that Work's state.
func (c *Collector) applyLinks() {
	if c.works == nil {
		return
	}
	works, err := c.works.ListWork()
	if err != nil {
		c.noteError("links", err)
		return
	}
	_, err = c.store.ApplyLinks(works, c.workClosedAt)
	c.noteError("links", err)
}

type closedAtEntry struct {
	revision uint64
	at       time.Time
}

// workClosedAt is when a Work's last event happened: its acceptance, for Work
// that is done. Looked up once per terminal revision.
func (c *Collector) workClosedAt(work brain.Work) time.Time {
	c.mu.Lock()
	if cached, ok := c.closedAt[work.ID]; ok && cached.revision == work.TerminalRevision && !cached.at.IsZero() {
		c.mu.Unlock()
		return cached.at
	}
	c.mu.Unlock()
	at := work.CreatedAt
	if events, err := c.works.ListWorkEvents(work.ID); err == nil {
		for _, event := range events {
			if event.CreatedAt.After(at) {
				at = event.CreatedAt
			}
		}
	}
	c.mu.Lock()
	if c.closedAt == nil {
		c.closedAt = map[string]closedAtEntry{}
	}
	c.closedAt[work.ID] = closedAtEntry{revision: work.TerminalRevision, at: at}
	c.mu.Unlock()
	return at
}

func (c *Collector) requireWork(id string) error {
	if c.works == nil {
		return errors.New("Work is not available")
	}
	works, err := c.works.ListWork()
	if err != nil {
		return err
	}
	for _, work := range works {
		if work.ID == strings.TrimSpace(id) {
			return nil
		}
	}
	return fmt.Errorf("work %q not found", id)
}
