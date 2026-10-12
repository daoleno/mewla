package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

// ExternalReader is the read-only Plugin boundary Watch uses.
type ExternalReader interface {
	WatchAccounts(integration string) []connections.Account
	WatchGitHub(ctx context.Context, accountID, path string, query url.Values) (connections.WatchResponse, error)
	WatchLinear(ctx context.Context, accountID, tool string, args map[string]any) ([]byte, error)
}

type githubCursor struct {
	Since       time.Time `json:"since"`
	CIRunID     int64     `json:"ci_run_id,omitempty"`
	CIFailing   bool      `json:"ci_failing,omitempty"`
	AlertsSince time.Time `json:"alerts_since"`
}

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
	accounts := c.external.WatchAccounts("github")
	since := c.horizon()
	hostName := sshHostNames(c.home)
	total := 0
	for _, project := range c.store.Projects() {
		if ctx.Err() != nil || time.Now().Before(c.githubPausedUntil) {
			break
		}
		owner, repo, ok := githubRemoteSlug(project.Remotes, hostName)
		if !ok || project.LastSeen.Before(since) {
			continue
		}
		link, err := c.githubLink(ctx, project, owner, repo, accounts)
		if err != nil || link.AccountID == "" {
			continue
		}
		added, err := c.collectGitHubProject(ctx, project.ID, link)
		total += added
		c.noteError("external:github:"+project.Name, err)
	}
	return total, nil
}

// githubLink finds and records the connected account that can read the
// repository, rechecking once a day.
func (c *Collector) githubLink(ctx context.Context, project Project, owner, repo string, accounts []connections.Account) (GitHub, error) {
	if link := project.GitHub; link != nil && link.Owner == owner && link.Repo == repo &&
		time.Since(link.CheckedAt) < githubRecheck {
		return *link, nil
	}
	link := GitHub{Owner: owner, Repo: repo, Endpoints: map[string]string{}, CheckedAt: time.Now().UTC()}
	if len(accounts) == 0 {
		link.Endpoints["repo"] = "no connected GitHub account"
	}
	var failures []string
	for _, account := range accounts {
		response, err := c.external.WatchGitHub(ctx, account.ID, "/repos/"+owner+"/"+repo, nil)
		c.noteRate(response)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", account.Name, statusText(response.Status, err)))
			link.Endpoints["repo"] = strings.Join(failures, "; ")
			continue
		}
		var meta struct {
			DefaultBranch string `json:"default_branch"`
		}
		_ = json.Unmarshal(response.Body, &meta)
		link.AccountID, link.Branch = account.ID, meta.DefaultBranch
		link.Endpoints["repo"] = "ok via " + account.Name
		break
	}
	_, err := c.store.UpdateProject(project.ID, func(p *Project) error {
		p.GitHub = &link
		return nil
	})
	return link, err
}

func statusText(status int, err error) string {
	if status > 0 {
		return "HTTP " + strconv.Itoa(status)
	}
	return err.Error()
}

func (c *Collector) noteRate(response connections.WatchResponse) {
	if response.RateRemaining >= 0 && response.RateRemaining < githubRateFloor && !response.RateReset.IsZero() {
		c.githubPausedUntil = response.RateReset
	}
}

func (c *Collector) collectGitHubProject(ctx context.Context, projectID string, link GitHub) (int, error) {
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
	base := "/repos/" + link.Owner + "/" + link.Repo
	refs := func(extra map[string]string) map[string]string {
		extra["repo"] = link.Owner + "/" + link.Repo
		return extra
	}
	endpoints := map[string]string{}
	var signals []Signal
	pollStarted := time.Now().UTC()

	// Default-branch CI: each failed run, and the first pass after failures.
	if link.Branch != "" {
		query := url.Values{"branch": {link.Branch}, "status": {"completed"}, "per_page": {"30"}}
		response, err := c.external.WatchGitHub(ctx, link.AccountID, base+"/actions/runs", query)
		c.noteRate(response)
		endpoints["actions"] = endpointState(response, err)
		if err == nil {
			var page struct {
				Runs []githubRun `json:"workflow_runs"`
			}
			_ = json.Unmarshal(response.Body, &page)
			sort.Slice(page.Runs, func(i, j int) bool { return page.Runs[i].UpdatedAt.Before(page.Runs[j].UpdatedAt) })
			for _, run := range page.Runs {
				if run.ID <= cursor.CIRunID && run.RunAttempt <= 1 || run.UpdatedAt.Before(horizon) {
					if run.ID > cursor.CIRunID {
						cursor.CIRunID = run.ID
					}
					continue
				}
				failed := run.Conclusion == "failure" || run.Conclusion == "timed_out" || run.Conclusion == "startup_failure"
				passed := run.Conclusion == "success"
				runRefs := refs(map[string]string{
					"run": strconv.FormatInt(run.ID, 10), "workflow": run.Name, "branch": run.HeadBranch,
					"sha": run.HeadSHA, "conclusion": run.Conclusion, "url": run.HTMLURL,
				})
				switch {
				case failed:
					signals = append(signals, Signal{
						ID:     signalID(SourceExternal, "github-run|"+link.Owner+"/"+link.Repo+"|"+strconv.FormatInt(run.ID, 10)+"|"+strconv.Itoa(run.RunAttempt)),
						Source: SourceExternal, ProjectID: projectID, Time: run.UpdatedAt, Kind: kindCIFailed,
						Excerpt: excerpt(fmt.Sprintf("%s %s on %s: %s", run.Name, run.Conclusion, run.HeadBranch, run.DisplayTitle), 300),
						Refs:    runRefs, Pointer: run.HTMLURL,
					})
					cursor.CIFailing = true
				case passed && cursor.CIFailing:
					signals = append(signals, Signal{
						ID:     signalID(SourceExternal, "github-run|"+link.Owner+"/"+link.Repo+"|"+strconv.FormatInt(run.ID, 10)+"|"+strconv.Itoa(run.RunAttempt)),
						Source: SourceExternal, ProjectID: projectID, Time: run.UpdatedAt, Kind: kindCIPassed,
						Excerpt: excerpt(fmt.Sprintf("%s passed on %s again: %s", run.Name, run.HeadBranch, run.DisplayTitle), 300),
						Refs:    runRefs, Pointer: run.HTMLURL,
					})
					cursor.CIFailing = false
				}
				if run.ID > cursor.CIRunID {
					cursor.CIRunID = run.ID
				}
			}
		}
	}

	// Open issues and pull requests with activity since the cursor.
	query := url.Values{"state": {"open"}, "sort": {"updated"}, "direction": {"desc"}, "per_page": {"50"},
		"since": {cursor.Since.Format(time.RFC3339)}}
	response, err := c.external.WatchGitHub(ctx, link.AccountID, base+"/issues", query)
	c.noteRate(response)
	endpoints["issues"] = endpointState(response, err)
	issuesRead := err == nil
	if issuesRead {
		var issues []githubIssue
		_ = json.Unmarshal(response.Body, &issues)
		for _, issue := range issues {
			kind := kindIssueActivity
			if issue.PullRequest != nil {
				kind = kindPRActivity
			}
			signals = append(signals, Signal{
				ID:     signalID(SourceExternal, "github-issue|"+link.Owner+"/"+link.Repo+"|"+strconv.Itoa(issue.Number)+"|"+issue.UpdatedAt.Format(time.RFC3339)),
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
	for _, endpoint := range []string{"dependabot", "code-scanning"} {
		response, err := c.external.WatchGitHub(ctx, link.AccountID, base+"/"+endpoint+"/alerts",
			url.Values{"state": {"open"}, "per_page": {"50"}})
		c.noteRate(response)
		endpoints[endpoint] = endpointState(response, err)
		if err != nil {
			alertsRead = alertsRead && response.Status != 0
			continue
		}
		var alerts []githubAlert
		_ = json.Unmarshal(response.Body, &alerts)
		for _, alert := range alerts {
			if alert.UpdatedAt.Before(cursor.AlertsSince) {
				continue
			}
			summary := firstNonEmpty(alert.SecurityAdvisory.Summary, alert.Rule.Description)
			severity := firstNonEmpty(alert.SecurityAdvisory.Severity, alert.Rule.Severity)
			signals = append(signals, Signal{
				ID:     signalID(SourceExternal, "github-alert|"+endpoint+"|"+link.Owner+"/"+link.Repo+"|"+strconv.Itoa(alert.Number)+"|"+alert.UpdatedAt.Format(time.RFC3339)),
				Source: SourceExternal, ProjectID: projectID, Time: alert.UpdatedAt, Kind: kindSecurityAlert,
				Excerpt: excerpt(fmt.Sprintf("%s alert #%d (%s): %s %s", endpoint, alert.Number, severity, alert.Dependency.Package.Name, summary), 300),
				Refs:    refs(map[string]string{"alert": strconv.Itoa(alert.Number), "kind": endpoint, "severity": severity, "url": alert.HTMLURL}),
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
	_, _ = c.store.UpdateProject(projectID, func(p *Project) error {
		if p.GitHub != nil {
			for name, state := range endpoints {
				p.GitHub.Endpoints[name] = state
			}
		}
		return nil
	})
	return added, c.store.SetCursor(key, cursor)
}

func endpointState(response connections.WatchResponse, err error) string {
	if err == nil {
		return "ok"
	}
	return statusText(response.Status, err)
}

// Linear: teams are matched to projects as suggestions that the user confirms
// once; issues and comments are collected only for confirmed teams.

type linearTeamRecord struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type linearIssueRecord struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      any    `json:"status"`
	UpdatedAt  string `json:"updatedAt"`
}

type linearCommentRecord struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	UpdatedAt string `json:"updatedAt"`
	CreatedAt string `json:"createdAt"`
}

var linearIssueLink = regexp.MustCompile(`linear\.app/[A-Za-z0-9_-]+/issue/([A-Z][A-Z0-9]{0,9})-\d+`)

func (c *Collector) collectLinear(ctx context.Context) (int, error) {
	accounts := c.external.WatchAccounts("linear")
	if len(accounts) == 0 {
		return 0, nil
	}
	teams := map[string]LinearTeam{}
	var lastErr error
	for _, account := range accounts {
		raw, err := c.external.WatchLinear(ctx, account.ID, "list_teams", map[string]any{"limit": 100})
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", account.Name, err)
			continue
		}
		var records []linearTeamRecord
		collectRecords(raw, &records, func(r linearTeamRecord) bool { return r.Key != "" && r.ID != "" })
		for _, record := range records {
			teams[record.Key] = LinearTeam{AccountID: account.ID, TeamID: record.ID, Key: record.Key, Name: record.Name}
		}
	}
	c.linearTeams = len(teams)
	if len(teams) == 0 {
		return 0, lastErr
	}
	c.suggestLinear(teams)
	total := 0
	for _, project := range c.store.Projects() {
		if project.Linear == nil || project.Linear.Confirmed == nil {
			continue
		}
		added, err := c.collectLinearTeam(ctx, project.ID, *project.Linear.Confirmed)
		total += added
		if err != nil {
			lastErr = err
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

func (c *Collector) collectLinearTeam(ctx context.Context, projectID string, team LinearTeam) (int, error) {
	key := "external:linear:" + projectID + ":" + team.Key
	var cursor linearCursor
	c.store.Cursor(key, &cursor)
	if horizon := c.horizon(); cursor.Since.Before(horizon) {
		cursor.Since = horizon
	}
	pollStarted := time.Now().UTC()
	raw, err := c.external.WatchLinear(ctx, team.AccountID, "list_issues", map[string]any{
		"team": team.TeamID, "updatedAt": cursor.Since.Format(time.RFC3339), "orderBy": "updatedAt", "limit": 50,
	})
	if err != nil {
		return 0, err
	}
	var issues []linearIssueRecord
	collectRecords(raw, &issues, func(r linearIssueRecord) bool { return r.Identifier != "" })
	var signals []Signal
	for index, issue := range issues {
		updated, _ := time.Parse(time.RFC3339, issue.UpdatedAt)
		if updated.IsZero() {
			updated = pollStarted
		}
		refs := map[string]string{"issue": issue.Identifier, "team": team.Key, "url": issue.URL}
		if state, ok := issue.State.(string); ok {
			refs["state"] = state
		}
		signals = append(signals, Signal{
			ID:     signalID(SourceExternal, "linear-issue|"+issue.Identifier+"|"+issue.UpdatedAt),
			Source: SourceExternal, ProjectID: projectID, Time: updated, Kind: kindLinearIssue,
			Excerpt: excerpt(issue.Identifier+" "+issue.Title, 300), Refs: refs, Pointer: issue.URL,
		})
		if index >= 20 {
			continue
		}
		rawComments, err := c.external.WatchLinear(ctx, team.AccountID, "list_comments", map[string]any{"issueId": issue.Identifier, "limit": 20})
		if err != nil {
			continue
		}
		var comments []linearCommentRecord
		collectRecords(rawComments, &comments, func(r linearCommentRecord) bool { return r.ID != "" && r.Body != "" })
		for _, comment := range comments {
			at, _ := time.Parse(time.RFC3339, firstNonEmpty(comment.UpdatedAt, comment.CreatedAt))
			if at.Before(cursor.Since) {
				continue
			}
			signals = append(signals, Signal{
				ID:     signalID(SourceExternal, "linear-comment|"+comment.ID+"|"+comment.UpdatedAt),
				Source: SourceExternal, ProjectID: projectID, Time: at, Kind: kindLinearComment,
				Excerpt: excerpt(issue.Identifier+": "+comment.Body, 400),
				Refs:    map[string]string{"issue": issue.Identifier, "team": team.Key, "comment": comment.ID},
				Pointer: issue.URL,
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
			if depth < 3 && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
				var inner any
				if json.Unmarshal([]byte(trimmed), &inner) == nil {
					walk(inner, depth+1)
				}
			}
		}
	}
	walk(value, 0)
}
