package main

import (
	"log"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/agentevents"
	"github.com/daoleno/mewla/daemon/answered"
	"github.com/daoleno/mewla/daemon/classifier"
	"github.com/daoleno/mewla/daemon/server"
)

// recordAnswered writes one answered request; a failure is logged, never
// returned: the answer itself already happened.
func recordAnswered(ledger *answered.Ledger, record answered.Request) {
	if ledger == nil {
		return
	}
	if _, _, err := ledger.Record(record); err != nil {
		log.Printf("answered request %s not recorded: %v", record.ID, err)
	}
}

// approvalWire is a pending approval as the app sees it.
func approvalWire(a agentevents.Approval) server.ApprovalWire {
	wire := server.ApprovalWire{ID: a.ID, Kind: a.Kind, Tool: a.Tool, Summary: a.Summary}
	for _, option := range a.Options {
		wire.Options = append(wire.Options, server.ApprovalOptionWire{ID: option.ID, Label: option.Label, Scope: option.Scope})
	}
	return wire
}

// recordAgentResolution records an approval the agent settled without
// Mewla: answered in the agent's own terminal UI, or timed out unanswered.
// Answers through Mewla (By "mewla") are recorded by whoever gave them, with
// who they were.
func recordAgentResolution(ledger *answered.Ledger, workerID string, approval agentevents.Approval, e agentevents.Event) {
	var by answered.Answerer
	switch e.By {
	case "agent":
		by = answered.Answerer{Kind: answered.ByTerminal}
	case "timeout":
		by = answered.Answerer{Kind: answered.ByTimeout}
	default:
		return
	}
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}
	record := server.ApprovalAnswerRecord(workerID, approvalWire(approval), e.OptionID, by, at)
	if e.By == "timeout" {
		record.Answer, record.Outcome = "", answered.OutcomeNotAnswered
	} else if strings.TrimSpace(e.OptionID) == "" {
		record.Answer, record.Outcome = "", answered.OutcomeAnswered
	}
	recordAnswered(ledger, record)
}

// operatorAnswerer is who answers through `mewla worker answer`: Brain for
// its delegated Workers (with the sentence of its brief it relied on), an
// operator for a Session forced from outside.
func operatorAnswerer(worker *classifier.Worker, basis string) answered.Answerer {
	if worker != nil && (worker.Delegated || worker.Hidden) {
		return answered.Answerer{Kind: answered.ByBrain, Basis: strings.TrimSpace(basis)}
	}
	return answered.Answerer{Kind: answered.ByOperator, Basis: strings.TrimSpace(basis)}
}
