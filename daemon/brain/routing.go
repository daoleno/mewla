package brain

import "path/filepath"

// routing.md is Brain's Worker routing guide. It is seeded once and then owned
// by Brain and the user: product upgrades never rewrite it, and nothing parses
// it. Brain reads it before each spawn and judges the route itself.
const routingGuideName = "routing.md"

func (s *Store) routingGuidePath() string {
	return filepath.Join(s.WorkspacePath(), routingGuideName)
}

const defaultRoutingGuide = `# Worker Routing

Read before every mewla worker spawn and pass -executor, -model and -reasoning. This is judgment guidance, not rules. Brain keeps it current: replace a line when the user states a preference, a new executor or model appears, or a Worker result shows a poor fit.

- Pick the executor and model by the task's nature and the user's preferences; mewla brain executors --json lists executors.
- Set reasoning by difficulty: low for mechanical edits, medium for routine work, high for design, debugging or cross-cutting changes; the top levels only for the hardest problems.
- Prefer the cheapest route that will succeed; a failed cheap attempt costs more than the right model once.
- Use each client's own ids: codex and claude take model names, pi takes provider/model. Reasoning levels: codex none-ultra, claude low-max, pi off-max.

| Task | Executor | Model | Reasoning |
| --- | --- | --- | --- |
| Tiny mechanical edit | pi | a fast, inexpensive model | low |
| Routine implementation | codex | client default | medium |
| Visual design, frontend, product copy | claude | client default | high |
| Hard design or debugging | claude | the strongest available | high |
`
