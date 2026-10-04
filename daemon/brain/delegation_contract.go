package brain

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

const (
	brainWorkerRoleContractPlaceholder = "{{ZEN_BRAIN_WORKER_ROLE_CONTRACT}}"
	brainWorkerRoleContract            = "Brain owns conversation, planning, lifecycle, review and acceptance. Delegate substantive execution to a visible Zen Worker unless the user explicitly asks Brain to execute it directly. Inspect context as needed to form or review a brief. Brain handles questions and routine configuration directly, including Worker defaults; query the effective result. A delegation failure does not authorize direct execution."
)

func brainHostContractDigest() string {
	// Refresh an existing Host when release guidance changes, not when private
	// overlays change. Hash lazy guidance without embedding it in activation.
	parts := []string{brainHostActivationPrompt(), productWorkspaceInstructions, productDelegationPolicy, productEnginePolicy, productHandoffPolicy, productCalendarPolicy}
	for _, playbook := range seedPlaybooks {
		parts = append(parts, playbook.name, playbook.initial)
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))
}

func projectBrainWorkerRoleContract(template string) string {
	return strings.ReplaceAll(template, brainWorkerRoleContractPlaceholder, brainWorkerRoleContract)
}

// brainHostActivationPrompt is re-sent when a Host process generation or the
// product guidance changes. The role lives once, in AGENTS.md; the activation
// only makes the Host reload it.
func brainHostActivationPrompt() string {
	return strings.Join([]string{
		"Brain Host activation contract:",
		"Read AGENTS.md before continuing, even in a resumed Session; it holds the current Brain role and guidance. Load a policy or playbook only when its workflow applies. Preserve active Work/Event state.",
	}, "\n")
}
