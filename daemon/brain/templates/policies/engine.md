# Brain Executor Policy

The Host executor runs Brain. The delegated executor runs Workers and ordinary new Sessions. Worker model, reasoning and autonomy defaults apply only to delegated Workers, not to Brain or manual Sessions.

Change Worker defaults directly when the user asks: zen worker defaults -executor <client> -model <model> -reasoning <level>, then verify with zen worker defaults --json. Name a client such as codex, not an alias such as codex-high. Do not delegate this or edit native Codex or provider config instead. New Workers use it immediately; running Sessions keep their settings.

zen worker spawn -model/-reasoning overrides the defaults for one launch. Use a manual Session when the user wants interactive approval prompts. Provider credentials and routing are separate from model choice.

Keep the configured executor unless the user asks for another; the kind of task alone is not a reason to switch. Do not claim model state carries across executors or that a harness has capabilities it does not expose.
