package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/connections"
)

// External signals come from connected Plugins, read-only. Fetched content is
// stored as data: excerpts are redacted and nothing in them is ever acted on.

const (
	kindCIFailed      = "ci_failed"
	kindCIPassed      = "ci_passed"
	kindIssueActivity = "issue_activity"
	kindPRActivity    = "pr_activity"
	kindSecurityAlert = "security_alert"
	kindLinearIssue   = "linear_issue"
	kindLinearComment = "linear_comment"

	githubRecheck   = 24 * time.Hour
	githubRateFloor = 100
)

// ExternalReader is the Plugin boundary Watch reads through. Reads honor the
// account switch and the person's per-tool grants, exactly as Brain's calls do.
type ExternalReader interface {
	ReadAccounts(integration string) []connections.ReadAccount
	ReadTool(ctx context.Context, accountID, tool string, args map[string]any) (connections.ReadResult, error)
}

// Coverage states for a project's external source; see Coverage.
const (
	CoverageCollecting   = "collecting"
	CoverageNotConnected = "not_connected"
	CoverageDisabled     = "disabled"
	CoverageNotAllowed   = "not_allowed"
	CoverageUnreachable  = "unreachable"
	CoverageUnlinked     = "unlinked"
)

type githubCursor struct {
	Since       time.Time `json:"since"`
	AlertsSince time.Time `json:"alerts_since"`
	// RunsSince is the latest completion seen. Runs finish out of ID order
	// (a long CI run after a short Pages run), so reads overlap it and rely on
	// signal-ID dedupe. Failing is tracked per workflow.
	RunsSince time.Time       `json:"runs_since"`
	Failing   map[string]bool `json:"failing,omitempty"`
}

const runOverlap = 15 * time.Minute

type githubRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	Event        string    `json:"event"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	RunAttempt   int       `json:"run_attempt"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type githubIssue struct {
	Number      int                    `json:"number"`
	Title       string                 `json:"title"`
	Body        string                 `json:"body"`
	HTMLURL     string                 `json:"html_url"`
	Comments    int                    `json:"comments"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CreatedAt   time.Time              `json:"created_at"`
	User        struct{ Login string } `json:"user"`
	PullRequest *struct{}              `json:"pull_request"`
}

type githubAlert struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
	Rule      struct {
		Description string `json:"description"`
		Severity    string `json:"severity"`
	} `json:"rule"`
	SecurityAdvisory struct {
		Summary  string `json:"summary"`
		Severity string `json:"severity"`
	} `json:"security_advisory"`
	Dependency struct {
		Package struct{ Name string } `json:"package"`
	} `json:"dependency"`
}

func (c *Collector) collectExternal(ctx context.Context) (int, error) {
	if c.external == nil {
		return 0, nil
	}
	total := 0
	added, err := c.collectGitHub(ctx)
	total += added
	c.noteError("external:github", err)
	added, err = c.collectLinear(ctx)
	total += added
	c.noteError("external:linear", err)
	return total, ctx.Err()
}

func (c *Collector) collectGitHub(ctx context.Context) (int, error) {
	accounts := c.external.ReadAccounts("github")
	since := c.horizon()
	hostName := sshHostNames(c.home)
	total := 0
	for _, project := range c.store.Projects() {
		if ctx.Err() != nil || c.now().Before(c.githubPausedUntil) {
			break
		}
		owner, repo, ok := githubRemoteSlug(project.Remotes, hostName)
		if !ok || project.LastSeen.Before(since) {
			continue
		}
		link, reads := c.githubLink(ctx, project, owner, repo, accounts)
		if link.Coverage == CoverageCollecting {
			added, err := c.collectGitHubProject(ctx, project.ID, &link, reads)
			total += added
			c.noteError("external:github:"+project.Name, err)
		}
		if _, err := c.store.UpdateProject(project.ID, func(p *Project) error {
			p.GitHub = &link
			return nil
		}); err != nil {
			return total, err
		}
	}
	return total, nil
}

// githubLink decides, every pass, whether and through which account the
// project's repository is read, and records why when it is not. A working
// account is kept while it stays switched on and allowed, rechecked daily.
func (c *Collector) githubLink(ctx context.Context, project Project, owner, repo string, accounts []connections.ReadAccount) (GitHub, map[string]bool) {
	link := GitHub{Owner: owner, Repo: repo, Endpoints: map[string]string{}}
	if previous := project.GitHub; previous != nil && previous.Owner == owner && previous.Repo == repo {
		link.AccountID, link.Branch, link.CheckedAt = previous.AccountID, previous.Branch, previous.CheckedAt
	}
	var usable []connections.ReadAccount
	for _, account := range accounts {
		if account.Enabled {
			usable = append(usable, account)
		}
	}
	switch {
	case len(accounts) == 0:
		link.AccountID, link.Coverage, link.Reason = "", CoverageNotConnected, "no GitHub plugin is connected"
		return link, nil
	case len(usable) == 0:
		link.AccountID, link.Coverage, link.Reason = "", CoverageDisabled, "the GitHub plugin is switched off"
		return link, nil
	}
	if link.AccountID != "" && c.now().Sub(link.CheckedAt) < githubRecheck {
		for _, account := range usable {
			if account.ID == link.AccountID && account.Reads["get_repository"] {
				link.Coverage, link.Reason = CoverageCollecting, "via "+account.Name
				return link, account.Reads
			}
		}
	}
	link.AccountID, link.Branch, link.CheckedAt = "", "", c.now()
	var failures []string
	allowed := false
	for _, account := range usable {
		if !account.Reads["get_repository"] {
			failures = append(failures, account.Name+": get_repository is not allowed")
			continue
		}
		allowed = true
		result, err := c.external.ReadTool(ctx, account.ID, "get_repository", githubArgs(owner, repo, nil))
		c.noteRate(result)
		if err != nil {
			failures = append(failures, account.Name+": "+readFailure(err))
			continue
		}
		var meta struct {
			DefaultBranch string `json:"default_branch"`
		}
		_ = json.Unmarshal(result.Body, &meta)
		link.AccountID, link.Branch = account.ID, meta.DefaultBranch
		link.Coverage, link.Reason = CoverageCollecting, "via "+account.Name
		return link, account.Reads
	}
	link.Coverage = CoverageUnreachable
	if !allowed {
		link.Coverage = CoverageNotAllowed
	}
	link.Reason = strings.Join(failures, "; ")
	return link, nil
}

func githubArgs(owner, repo string, query map[string]any) map[string]any {
	args := map[string]any{"path": map[string]any{"owner": owner, "repo": repo}}
	if len(query) > 0 {
		args["query"] = query
	}
	return args
}

// readFailure says briefly why a read did not happen.
func readFailure(err error) string {
	var status connections.StatusError
	switch {
	case errors.As(err, &status):
		return "HTTP " + strconv.Itoa(status.Status)
	case errors.Is(err, connections.ErrReadNotAllowed):
		return "not allowed"
	case errors.Is(err, connections.ErrAccountOff):
		return "switched off"
	}
	return err.Error()
}

func (c *Collector) noteRate(result connections.ReadResult) {
	if result.RateRemaining >= 0 && result.RateRemaining < githubRateFloor && !result.RateReset.IsZero() {
		c.githubPausedUntil = result.RateReset
	}
}

// readGitHub runs one read for the project and records the endpoint's state.
// answered is true when the service gave a definite answer (data or a
// refusal), so a cursor may move past it.
func (c *Collector) readGitHub(ctx context.Context, link *GitHub, reads map[string]bool, endpoint, tool string, query map[string]any) (body []byte, answered bool) {
	if !reads[tool] {
		link.Endpoints[endpoint] = "not allowed (" + tool + ")"
		return nil, false
	}
	result, err := c.external.ReadTool(ctx, link.AccountID, tool, githubArgs(link.Owner, link.Repo, query))
	c.noteRate(result)
	if err != nil {
		link.Endpoints[endpoint] = readFailure(err)
		var status connections.StatusError
		return nil, errors.As(err, &status)
	}
	link.Endpoints[endpoint] = "ok"
	return result.Body, true
}

func (c *Collector) collectGitHubProject(ctx context.Context, projectID string, link *GitHub, reads map[string]bool) (int, error) {
	key := "external:github:" + projectID
	var cursor githubCursor
	c.store.Cursor(key, &cursor)
	horizon := c.horizon()
	if cursor.Since.Before(horizon) {
		cursor.Since = horizon
	}
	if cursor.AlertsSince.Before(horizon) {
		cursor.AlertsSince = horizon
	}
	slug := link.Owner + "/" + link.Repo
	refs := func(extra map[string]string) map[string]string {
		extra["repo"] = slug
		return extra
	}
	var signals []Signal
	var body []byte
	pollStarted := c.now()

	// Default-branch CI: each failed run, and each workflow's first pass
	// after it failed.
	if link.Branch != "" {
		if cursor.Failing == nil {
			cursor.Failing = map[string]bool{}
		}
		body, _ = c.readGitHub(ctx, link, reads, "actions", "list_workflow_runs",
			map[string]any{"branch": link.Branch, "status": "completed", "per_page": 50})
		if body != nil {
			var page struct {
				Runs []githubRun `json:"workflow_runs"`
			}
			_ = json.Unmarshal(body, &page)
			sort.Slice(page.Runs, func(i, j int) bool { return page.Runs[i].UpdatedAt.Before(page.Runs[j].UpdatedAt) })
			readFrom := cursor.RunsSince.Add(-runOverlap)
			for _, run := range page.Runs {
				if run.UpdatedAt.Before(horizon) || run.UpdatedAt.Before(readFrom) {
					continue
				}
				if run.UpdatedAt.After(cursor.RunsSince) {
					cursor.RunsSince = run.UpdatedAt
				}
				failed := run.Conclusion == "failure" || run.Conclusion == "timed_out" || run.Conclusion == "startup_failure"
				passed := run.Conclusion == "success"
				kind, summary := "", ""
				switch {
				case failed:
					kind, summary = kindCIFailed, fmt.Sprintf("%s %s on %s: %s", run.Name, run.Conclusion, run.HeadBranch, run.DisplayTitle)
					cursor.Failing[run.Name] = true
				case passed && cursor.Failing[run.Name]:
					kind, summary = kindCIPassed, fmt.Sprintf("%s passed on %s again: %s", run.Name, run.HeadBranch, run.DisplayTitle)
					delete(cursor.Failing, run.Name)
				default:
					continue
				}
				signals = append(signals, Signal{
					ID:     signalID(SourceExternal, "github-run|"+slug+"|"+strconv.FormatInt(run.ID, 10)+"|"+strconv.Itoa(run.RunAttempt)),
					Source: SourceExternal, ProjectID: projectID, Time: run.UpdatedAt, Kind: kind,
					Excerpt: excerpt(summary, 300),
					Refs: refs(map[string]string{
						"run": strconv.FormatInt(run.ID, 10), "workflow": run.Name, "branch": run.HeadBranch,
						"sha": run.HeadSHA, "conclusion": run.Conclusion, "url": run.HTMLURL,
					}),
					Pointer: run.HTMLURL,
				})
			}
		}
	}

	// Open issues and pull requests with activity since the cursor.
	body, _ = c.readGitHub(ctx, link, reads, "issues", "list_updated_issues", map[string]any{
		"state": "open", "sort": "updated", "direction": "desc", "per_page": 50, "since": cursor.Since.Format(time.RFC3339)})
	issuesRead := body != nil
	if issuesRead {
		var issues []githubIssue
		_ = json.Unmarshal(body, &issues)
		for _, issue := range issues {
			kind := kindIssueActivity
			if issue.PullRequest != nil {
				kind = kindPRActivity
			}
			signals = append(signals, Signal{
				ID:     signalID(SourceExternal, "github-issue|"+slug+"|"+strconv.Itoa(issue.Number)+"|"+issue.UpdatedAt.Format(time.RFC3339)),
				Source: SourceExternal, ProjectID: projectID, Time: issue.UpdatedAt, Kind: kind,
				Excerpt: excerpt(fmt.Sprintf("#%d %s — %s", issue.Number, issue.Title, issue.Body), 400),
				Refs: refs(map[string]string{"number": strconv.Itoa(issue.Number), "author": issue.User.Login,
					"comments": strconv.Itoa(issue.Comments), "url": issue.HTMLURL}),
				Pointer: issue.HTMLURL,
			})
		}
	}

	// Security alerts, where the account's grant allows reading them.
	alertsRead := true
	for _, alertKind := range []struct{ endpoint, tool string }{
		{"dependabot", "list_dependabot_alerts"}, {"code-scanning", "list_code_scanning_alerts"},
	} {
		body, answered := c.readGitHub(ctx, link, reads, alertKind.endpoint, alertKind.tool, map[string]any{"state": "open", "per_page": 50})
		alertsRead = alertsRead && answered
		if body == nil {
			continue
		}
		var alerts []githubAlert
		_ = json.Unmarshal(body, &alerts)
		for _, alert := range alerts {
			if alert.UpdatedAt.Before(cursor.AlertsSince) {
				continue
			}
			summary := firstNonEmpty(alert.SecurityAdvisory.Summary, alert.Rule.Description)
			severity := firstNonEmpty(alert.SecurityAdvisory.Severity, alert.Rule.Severity)
			signals = append(signals, Signal{
				ID:     signalID(SourceExternal, "github-alert|"+alertKind.endpoint+"|"+slug+"|"+strconv.Itoa(alert.Number)+"|"+alert.UpdatedAt.Format(time.RFC3339)),
				Source: SourceExternal, ProjectID: projectID, Time: alert.UpdatedAt, Kind: kindSecurityAlert,
				Excerpt: excerpt(fmt.Sprintf("%s alert #%d (%s): %s %s", alertKind.endpoint, alert.Number, severity, alert.Dependency.Package.Name, summary), 300),
				Refs:    refs(map[string]string{"alert": strconv.Itoa(alert.Number), "kind": alertKind.endpoint, "severity": severity, "url": alert.HTMLURL}),
				Pointer: alert.HTMLURL,
			})
		}
	}

	added, err := c.store.Append(signals)
	if err != nil {
		return 0, err
	}
	if issuesRead {
		cursor.Since = pollStarted.Add(-time.Minute)
	}
	if alertsRead {
		cursor.AlertsSince = pollStarted.Add(-time.Minute)
	}
	return added, c.store.SetCursor(key, cursor)
}

// Linear: teams are matched to projects as suggestions that the user confirms
// once; issues and comments are collected only for confirmed teams.

type linearTeamRecord struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// linearIssueRecord matches Linear's list_issues entries, whose id is the
// human identifier (FRE-12) and whose project and status are names.
type linearIssueRecord struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Status     any    `json:"status"`
	Project    any    `json:"project"`
	UpdatedAt  string `json:"updatedAt"`
}

var linearIdentifier = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}-\d+$`)

func (r linearIssueRecord) identifier() string {
	if r.Identifier != "" {
		return r.Identifier
	}
	if linearIdentifier.MatchString(r.ID) {
		return r.ID
	}
	return ""
}

type linearCommentRecord struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updatedAt"`
	CreatedAt string `json:"createdAt"`
	Author    struct {
		Name string `json:"name"`
	} `json:"author"`
}

var linearIssueLink = regexp.MustCompile(`linear\.app/[A-Za-z0-9_-]+/issue/([A-Z][A-Z0-9]{0,9})-\d+`)

const linearCommentIssues = 20

func (c *Collector) collectLinear(ctx context.Context) (int, error) {
	accounts := c.external.ReadAccounts("linear")
	teams := map[string]LinearTeam{}
	reads := map[string]map[string]bool{}
	var lastErr error
	usable, teamsAllowed := 0, 0
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		usable++
		reads[account.ID] = account.Reads
		if !account.Reads["list_teams"] {
			continue
		}
		teamsAllowed++
		result, err := c.external.ReadTool(ctx, account.ID, "list_teams", map[string]any{"limit": 100})
		if err != nil {
			lastErr = fmt.Errorf("%s: %s", account.Name, readFailure(err))
			continue
		}
		var records []linearTeamRecord
		collectRecords(result.Body, &records, func(r linearTeamRecord) bool { return r.Key != "" && r.ID != "" })
		for _, record := range records {
			teams[record.Key] = LinearTeam{AccountID: account.ID, TeamID: record.ID, Key: record.Key, Name: record.Name}
		}
	}
	c.linearTeams = len(teams)
	if len(teams) > 0 {
		c.suggestLinear(teams)
	}
	total := 0
	for _, project := range c.store.Projects() {
		if project.Linear == nil || project.Linear.Confirmed == nil {
			continue
		}
		key := project.Linear.Confirmed.Key
		team, seen := teams[key]
		coverage, reason := CoverageCollecting, ""
		switch {
		case len(accounts) == 0:
			coverage, reason = CoverageNotConnected, "no Linear plugin is connected"
		case usable == 0:
			coverage, reason = CoverageDisabled, "the Linear plugin is switched off"
		case teamsAllowed == 0:
			coverage, reason = CoverageNotAllowed, "list_teams is not allowed"
		case !seen:
			coverage, reason = CoverageUnreachable, "no connected Linear account sees team "+key
		case !reads[team.AccountID]["list_issues"]:
			coverage, reason = CoverageNotAllowed, "list_issues is not allowed"
		default:
			added, err := c.collectLinearTeam(ctx, project.ID, team, reads[team.AccountID])
			total += added
			if err != nil {
				lastErr = err
				coverage, reason = CoverageUnreachable, err.Error()
			}
		}
		if _, err := c.store.UpdateProject(project.ID, func(p *Project) error {
			if p.Linear != nil {
				p.Linear.Coverage, p.Linear.Reason = coverage, reason
			}
			return nil
		}); err != nil {
			return total, err
		}
	}
	return total, lastErr
}

// suggestLinear proposes team mappings by name and by issue links seen in a
// project's own transcripts.
func (c *Collector) suggestLinear(teams map[string]LinearTeam) {
	linked := map[string]map[string]int{}
	for _, signal := range c.store.Signals(Filter{Source: SourceHuman}) {
		if signal.ProjectID == "" {
			continue
		}
		for _, match := range linearIssueLink.FindAllStringSubmatch(signal.Excerpt, -1) {
			if linked[signal.ProjectID] == nil {
				linked[signal.ProjectID] = map[string]int{}
			}
			linked[signal.ProjectID][match[1]]++
		}
	}
	for _, project := range c.store.Projects() {
		if project.Builtin {
			continue
		}
		var suggested []LinearTeam
		name := strings.ToLower(project.Name)
		for key, team := range teams {
			reason := ""
			switch {
			case strings.EqualFold(team.Key, project.Name) || strings.EqualFold(team.Name, project.Name):
				reason = "team name matches the project"
			case len(name) >= 4 && (strings.Contains(strings.ToLower(team.Name), name) || strings.Contains(name, strings.ToLower(team.Name)) && len(team.Name) >= 4):
				reason = "team name resembles the project"
			case linked[project.ID][key] > 0:
				reason = fmt.Sprintf("%d issue links in this project's sessions", linked[project.ID][key])
			}
			if reason != "" {
				team.Reason = reason
				suggested = append(suggested, team)
			}
		}
		sort.Slice(suggested, func(i, j int) bool { return suggested[i].Key < suggested[j].Key })
		current := project.Linear
		if current == nil && len(suggested) == 0 {
			continue
		}
		if current != nil && sameTeams(current.Suggested, suggested) {
			continue
		}
		_, _ = c.store.UpdateProject(project.ID, func(p *Project) error {
			if p.Linear == nil {
				p.Linear = &Linear{}
			}
			p.Linear.Suggested = suggested
			return nil
		})
	}
}

func sameTeams(left, right []LinearTeam) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type linearCursor struct {
	Since time.Time `json:"since"`
}

func (c *Collector) collectLinearTeam(ctx context.Context, projectID string, team LinearTeam, reads map[string]bool) (int, error) {
	// Step 1 read issues under "external:linear:" but could not parse them,
	// so that cursor moved past issues it never stored.
	key := "external:linear-issues:" + projectID + ":" + team.Key
	var cursor linearCursor
	c.store.Cursor(key, &cursor)
	if horizon := c.horizon(); cursor.Since.Before(horizon) {
		cursor.Since = horizon
	}
	pollStarted := c.now()
	result, err := c.external.ReadTool(ctx, team.AccountID, "list_issues", map[string]any{
		"team": team.TeamID, "updatedAt": cursor.Since.Format(time.RFC3339), "orderBy": "updatedAt", "limit": 100,
	})
	if err != nil {
		return 0, fmt.Errorf("list_issues: %s", readFailure(err))
	}
	var issues []linearIssueRecord
	collectRecords(result.Body, &issues, func(r linearIssueRecord) bool { return r.identifier() != "" && r.Title != "" })
	var signals []Signal
	for index, issue := range issues {
		identifier := issue.identifier()
		updated, _ := time.Parse(time.RFC3339, issue.UpdatedAt)
		if updated.IsZero() {
			updated = pollStarted
		}
		refs := map[string]string{"issue": identifier, "team": team.Key, "url": issue.URL}
		if state, ok := issue.Status.(string); ok {
			refs["state"] = state
		}
		// The Linear project is kept as evidence; the issue belongs to the
		// project the user confirmed for its team.
		if name, ok := issue.Project.(string); ok && name != "" {
			refs["linear_project"] = name
		}
		signals = append(signals, Signal{
			ID:     signalID(SourceExternal, "linear-issue|"+identifier+"|"+issue.UpdatedAt),
			Source: SourceExternal, ProjectID: projectID, Time: updated, Kind: kindLinearIssue,
			Excerpt: excerpt(identifier+" "+issue.Title, 300), Refs: refs, Pointer: issue.URL,
		})
		if index >= linearCommentIssues || !reads["list_comments"] {
			continue
		}
		comments, err := c.external.ReadTool(ctx, team.AccountID, "list_comments", map[string]any{"issueId": identifier, "limit": 20})
		if err != nil {
			continue
		}
		var records []linearCommentRecord
		collectRecords(comments.Body, &records, func(r linearCommentRecord) bool { return r.ID != "" && r.Body != "" })
		for _, comment := range records {
			at, _ := time.Parse(time.RFC3339, firstNonEmpty(comment.UpdatedAt, comment.CreatedAt))
			if at.Before(cursor.Since) {
				continue
			}
			commentRefs := map[string]string{"issue": identifier, "team": team.Key, "comment": comment.ID, "author": comment.Author.Name}
			if name := refs["linear_project"]; name != "" {
				commentRefs["linear_project"] = name
			}
			signals = append(signals, Signal{
				ID:     signalID(SourceExternal, "linear-comment|"+comment.ID+"|"+comment.UpdatedAt),
				Source: SourceExternal, ProjectID: projectID, Time: at, Kind: kindLinearComment,
				Excerpt: excerpt(identifier+": "+comment.Body, 400), Refs: commentRefs, Pointer: issue.URL,
			})
		}
	}
	added, err := c.store.Append(signals)
	if err != nil {
		return 0, err
	}
	cursor.Since = pollStarted.Add(-time.Minute)
	return added, c.store.SetCursor(key, cursor)
}

// collectRecords finds every JSON object in raw that decodes into T and
// passes keep. Linear's MCP results wrap lists in varying envelopes.
func collectRecords[T any](raw []byte, out *[]T, keep func(T) bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	var walk func(any, int)
	walk = func(node any, depth int) {
		if depth > 6 {
			return
		}
		switch typed := node.(type) {
		case []any:
			for _, item := range typed {
				walk(item, depth+1)
			}
		case map[string]any:
			encoded, _ := json.Marshal(typed)
			var record T
			if json.Unmarshal(encoded, &record) == nil && keep(record) {
				*out = append(*out, record)
				return
			}
			for _, child := range typed {
				walk(child, depth+1)
			}
		case string:
			// Some MCP servers return JSON as text content.
			trimmed := strings.TrimSpace(typed)
			if depth < 5 && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
				var inner any
				if json.Unmarshal([]byte(trimmed), &inner) == nil {
					walk(inner, depth+1)
				}
			}
		}
	}
	walk(value, 0)
}
