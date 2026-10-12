package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// Rules flag a few unambiguous problems without a model. Each rule returns
// every subject it currently flags; the registry keeps stable IDs and marks
// subjects no longer flagged inactive.

const (
	RuleCIRed         = "ci_red"
	RuleRepeatedError = "repeated_error"
	RuleWorkerStalls  = "worker_stalls"

	repeatedErrorCount  = 5
	repeatedErrorWindow = 10 * time.Minute
	repeatedErrorRecent = 7 * 24 * time.Hour
	stallCount          = 2
	stallRecent         = 7 * 24 * time.Hour
	maxEvidence         = 20
)

func problemID(rule, subject string) string {
	sum := sha256.Sum256([]byte(rule + "\x00" + subject))
	return "w_" + hex.EncodeToString(sum[:])[:12]
}

func evidenceIDs(signals []Signal) []string {
	ids := make([]string, 0, min(len(signals), maxEvidence))
	for index := len(signals) - 1; index >= 0 && len(ids) < maxEvidence; index-- {
		ids = append(ids, signals[index].ID)
	}
	return ids
}

// evaluateRules runs every rule against the store and records the results.
func evaluateRules(store *Store, now time.Time) (int, error) {
	changed := 0
	for rule, evaluate := range map[string]func(*Store, time.Time) []Problem{
		RuleCIRed:         ruleCIRed,
		RuleRepeatedError: ruleRepeatedError,
		RuleWorkerStalls:  ruleWorkerStalls,
	} {
		n, err := store.RecordProblems(rule, evaluate(store, now))
		if err != nil {
			return changed, err
		}
		changed += n
	}
	return changed, nil
}

// ruleCIRed flags a project whose latest default-branch CI result is a
// failure, with how long it has been red.
func ruleCIRed(store *Store, now time.Time) []Problem {
	byProject := map[string][]Signal{}
	for _, kind := range []string{kindCIFailed, kindCIPassed} {
		for _, signal := range store.Signals(Filter{Source: SourceExternal, Kind: kind}) {
			byProject[signal.ProjectID] = append(byProject[signal.ProjectID], signal)
		}
	}
	var out []Problem
	for projectID, signals := range byProject {
		sort.Slice(signals, func(i, j int) bool { return signals[i].Time.Before(signals[j].Time) })
		latest := signals[len(signals)-1]
		if latest.Kind != kindCIFailed {
			continue
		}
		streak := signals
		for index := len(signals) - 1; index >= 0; index-- {
			if signals[index].Kind == kindCIPassed {
				streak = signals[index+1:]
				break
			}
		}
		since := streak[0].Time
		branch := latest.Refs["branch"]
		out = append(out, Problem{
			ID: problemID(RuleCIRed, projectID), ProjectID: projectID, Rule: RuleCIRed, Subject: projectID,
			Title:    fmt.Sprintf("CI is red on %s", branch),
			Detail:   fmt.Sprintf("%d failed runs since %s (red for %s); latest: %s", len(streak), since.Format(time.RFC3339), now.Sub(since).Round(time.Minute), latest.Excerpt),
			Severe:   true,
			Count:    len(streak),
			Evidence: evidenceIDs(streak), FirstSeen: since, LastSeen: latest.Time,
		})
	}
	return out
}

// ruleRepeatedError flags a daemon log signature seen repeatedly within a
// short window during the last week.
func ruleRepeatedError(store *Store, now time.Time) []Problem {
	since := now.Add(-repeatedErrorRecent)
	bySignature := map[string][]Signal{}
	for _, signal := range store.Signals(Filter{Source: SourceRuntime, Since: since}) {
		if signal.Kind == kindLogLine {
			continue
		}
		key := signal.ProjectID + "|" + signal.Refs["signature"]
		bySignature[key] = append(bySignature[key], signal)
	}
	var out []Problem
	for key, signals := range bySignature {
		if len(signals) < repeatedErrorCount {
			continue
		}
		sort.Slice(signals, func(i, j int) bool { return signals[i].Time.Before(signals[j].Time) })
		burst, start := 0, 0
		for end := range signals {
			for signals[end].Time.Sub(signals[start].Time) > repeatedErrorWindow {
				start++
			}
			burst = max(burst, end-start+1)
		}
		if burst < repeatedErrorCount {
			continue
		}
		latest := signals[len(signals)-1]
		out = append(out, Problem{
			ID: problemID(RuleRepeatedError, key), ProjectID: latest.ProjectID, Rule: RuleRepeatedError, Subject: latest.Refs["signature"],
			Title:    "Repeated daemon error: " + excerpt(latest.Refs["pattern"], 120),
			Detail:   fmt.Sprintf("%d times in the last week, up to %d within %s; latest: %s", len(signals), burst, repeatedErrorWindow, latest.Excerpt),
			Severe:   burst >= 3*repeatedErrorCount,
			Count:    len(signals),
			Evidence: evidenceIDs(signals), FirstSeen: signals[0].Time, LastSeen: latest.Time,
		})
	}
	return out
}

// ruleWorkerStalls flags Work whose Worker was lost or stalled repeatedly
// during the last week.
func ruleWorkerStalls(store *Store, now time.Time) []Problem {
	since := now.Add(-stallRecent)
	byWork := map[string][]Signal{}
	for _, kind := range []string{"turn_lost", "stalled", "lease_expired"} {
		for _, signal := range store.Signals(Filter{Source: SourceProcess, Kind: kind, Since: since}) {
			byWork[signal.Refs["work"]] = append(byWork[signal.Refs["work"]], signal)
		}
	}
	var out []Problem
	for workID, signals := range byWork {
		if workID == "" || len(signals) < stallCount {
			continue
		}
		sort.Slice(signals, func(i, j int) bool { return signals[i].Time.Before(signals[j].Time) })
		latest := signals[len(signals)-1]
		title := firstNonEmpty(latest.Refs["work_title"], workID)
		out = append(out, Problem{
			ID: problemID(RuleWorkerStalls, workID), ProjectID: latest.ProjectID, Rule: RuleWorkerStalls, Subject: workID,
			Title:    fmt.Sprintf("Worker stalled or lost %d times: %s", len(signals), title),
			Detail:   fmt.Sprintf("Work %s; latest %s at %s: %s", workID, latest.Kind, latest.Time.Format(time.RFC3339), latest.Excerpt),
			Count:    len(signals),
			Evidence: evidenceIDs(signals), FirstSeen: signals[0].Time, LastSeen: latest.Time,
		})
	}
	return out
}
