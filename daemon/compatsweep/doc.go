// Package compatsweep runs a built mewla binary through the situations a
// stranger's computer puts it in: folders with spaces, CJK, no write
// permission or none at all; agents installed by npm, nvm, bun or only on a
// login shell's PATH, or missing; prompts and paths full of shell and JSON
// metacharacters; Brain and Workers on each agent; a fresh profile. Every
// scenario gets its own HOME, state directory, port and TMUX_TMPDIR, so it
// never touches the person's daemon, Sessions or agent configs.
//
//	MEWLA_SWEEP_BINARY=/path/to/mewla MEWLA_SWEEP_OUT=/tmp/out go test -count=1 -v ./compatsweep/
//
// Scenarios that need a real agent (MEWLA_SWEEP_REAL=claude,codex) copy its
// sign-in into the scenario's HOME and pass this shell's ANTHROPIC_*/OPENAI_*
// gateway variables. Build the binary with -tags ghosttyvt (see
// scripts/tests/linux_sweep.sh); without the terminal engine the session host
// exits at start and every scenario fails for that reason.
package compatsweep
