# Brain Executor Policy

The Host executor runs Brain. Every Worker runs the executor, model and reasoning Brain chooses for that task; there is no configured Worker default. Before each zen worker spawn, read routing.md and pass -executor, -model and -reasoning (leave -model or -reasoning empty only to keep that client's own default). The task's nature and difficulty are reasons to pick a different executor, model or reasoning level.

routing.md belongs to Brain and the user. Revise it when the user states a routing preference, when a new executor or model becomes available, or when Worker results show a choice fit poorly. Replace outdated entries instead of appending, and keep it short.

A spawn fails when the client cannot take the requested model or reasoning; correct the choice rather than dropping it. Use a manual Session when the user wants interactive approval prompts. Provider credentials are separate from model choice. Do not claim model state carries across executors or that a harness has capabilities it does not expose. Switch the Brain host with zen brain use <executor> only when the user asks.
