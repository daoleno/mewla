# Brain Executor Policy

Host Executor runs Brain. Delegated Executor selects the client used for Workers and ordinary default Sessions. Worker model/reasoning defaults and autonomous execution apply only to delegated Workers; they do not change Brain host or manual Session configuration.

Routine default Worker configuration is Brain's responsibility. Apply an authorized request directly with `zen worker defaults -executor codex -model gpt-6-astra -reasoning medium`, then query `zen worker defaults --json` to verify the effective selection and resolved command. Do not delegate routine configuration or edit native Codex/Provider files as a substitute. The operation is atomic, durable, and immediately applies to subsequent delegated launches; existing Sessions stay unchanged. Use client names such as `codex`, never model/effort aliases such as `codex-high`.

Single-launch `zen worker spawn -model ... -reasoning ...` overrides defaults. Explicit command selections override defaults too; typed launch options win over command values. Explicit `-command` and Codex resume still pass through the delegated client adapter. Conflicting approval/sandbox flags are rejected with an explanation. Use a manual Session when interactive permission restrictions are intended. Provider credentials and routing remain separate from model selection.

Use configured routing unless the user requests another executor; task type alone does not authorize switching. Use durable Work and current.md across executor changes. Do not imply hidden model-state transfer or capabilities the active harness does not expose. Waiting for authorization, unknown input delivery, and dispatch are not proof of active execution; report the observed state and obtain actual launch/progress evidence.
