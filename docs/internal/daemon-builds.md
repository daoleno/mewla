# Daemon builds and boot service internals

Maintainer notes for source builds, release binaries, staging, the installer
trust boundary and `zen boot`. The user guide is [Install](../install-daemon.md).

## Installer selection and trust boundary

On every fresh bootstrap run without `ZEN_VERSION`, the installer queries GitHub at run time and selects the SemVer-highest public nondraft Release whose tag matches supported `vX.Y.Z` or `vX.Y.Z-beta.N` syntax. GitHub's `prerelease` flag does not affect eligibility: strict SemVer ordering makes a stable tag outrank a beta at the same core version, while a beta with a higher core version outranks a stable release with a lower core. The default follows the highest published version without an embedded version that needs routine README or script updates. `ZEN_VERSION` is optional pinning only.

### Install location and PATH

With no `ZEN_INSTALL_DIR`, the installer preserves a safe existing user-owned Zen location. Otherwise, it selects an existing writable user-owned `bin` directory on `PATH` only when there is exactly one unambiguous choice; the fallback is `~/.local/bin`. It refuses root/system directories and refuses to run as root.

When the fallback is not already on `PATH`, the installer can append one marked, idempotent entry to `.zshrc`, `.bashrc`, or `.config/fish/config.fish`, according to `SHELL`. It will not edit a symlinked or foreign-owned profile. A piped installer cannot modify its parent shell, so the final output prints the exact `export PATH=...` or `fish_add_path ...` command to run immediately and the full installed path to use in the meantime. Set `ZEN_NO_PATH_UPDATE=1` to suppress all profile changes; the immediate command is still printed.

### Bootstrap trust boundary

The first `install.sh`, release archive, and `SHA256SUMS` arrive over GitHub HTTPS. The checksum detects a damaged or mismatched archive, but because the checksum is delivered through the same HTTPS trust boundary, the bootstrap does **not** claim Ed25519 authentication. Review the repository-root [`install.sh`](../../install.sh), pin its commit in the raw URL if your policy requires reviewed immutable bootstrap code, or use the manual path below.

As optional transport hardening on curl versions that support these flags, require HTTPS for the initial script request:

```sh
curl --proto '=https' --proto-redir '=https' -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | sh
```

## Build from source

Startup prints the listening address, available private-network addresses, and one pairing command. Saved model routes are reclaimed when the selected tmux server confirms their sessions are absent. Live or unobservable sessions retain their routes; a model-settings warning for a retained route does not mean the HTTP server failed to start.

Source builds require the Go toolchain declared in `daemon/go.mod`:

```bash
git clone https://github.com/daoleno/zen.git
cd zen
bun run daemon:build
./bin/zen --help
./bin/zen doctor
```

`bun run daemon:build` and `cd daemon && go run ./cmd/zen-dev` build with
`CGO_ENABLED=0`. The development watcher rebuilds Go source changes and restarts
the daemon with its existing arguments. No display libraries are required.

Product version for banners and release staging comes from `app/app.base.json` (`expo.version`). The daemon default is `daemon/cmd/zen/version.go` and can be overridden at link time (`-X main.Version=…`).

## Release binaries (Linux and Apple Silicon macOS)

`scripts/build-daemon-linux.sh` builds Linux amd64, Linux arm64 and Darwin
arm64 binaries with `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false` and stripped
ldflags. All targets use the same daemon feature set.

```bash
./scripts/build-daemon-linux.sh
# → dist-download/staging/bin/zen-linux-amd64
# → dist-download/staging/bin/zen-linux-arm64
# → dist-download/staging/bin/zen-darwin-arm64
```

Full local stage (clean directory each run; **no** GitHub Release):

```bash
./scripts/stage-release.sh
# → dist-download/vVERSION/
#    zen-linux-amd64.tar.gz
#    zen-linux-arm64.tar.gz
#    zen-darwin-arm64.tar.gz
#    SHA256SUMS  release-manifest.json  release-manifest.json.sig
```

Each archive contains the `zen` binary plus `LICENSE`, `NOTICE`, and `TRADEMARKS.md`. The command prints the exact stage path; tracked release notes remain on the GitHub Release page instead of becoming duplicate download assets.

Release staging requires the Ed25519 manifest key through `ZEN_UPDATE_SIGNING_KEY` (a local PEM path) or `ZEN_UPDATE_SIGNING_KEY_BASE64` (CI). The committed public key is the trust root embedded in the daemon; private key material is never stored in Git.

Verify identity sources and stage checksums:

```bash
./scripts/verify-release-identity.sh
VERSION="$(python3 -c "import json; print(json.load(open('app/app.base.json'))['expo']['version'])")"
./scripts/verify-release-identity.sh --stage "dist-download/v$VERSION"
(cd "dist-download/v$VERSION" && sha256sum -c SHA256SUMS)

## Runtime equivalence

`zen` is the whole runtime. Running it in a persistent shell or tmux session
and running it under systemd are equivalent deployment choices: the same ELF,
the same arguments, the same `-state-dir`, identity, port and authority. The
development command `zen-dev` exists only to rebuild and restart that same
runtime while you edit source; it is not a different daemon and adds no
separate trust, enrollment, scope or state database. A build failure leaves the
last healthy daemon running.

## `zen boot` contract

The unit carries the explicit runtime contract: an absolute `ExecStart` with
`-state-dir` and `-addr` (or `-lan`), `WorkingDirectory`, and the non-secret
`HOME`/`PATH` environment. Relative paths and `~` are resolved at install time
against the invoking directory, so a relative `-state-dir` never becomes a
second state, identity or loopback bind. Pointing `-binary` at `zen-dev`
requires `-work-dir` (by default the current directory) to be the source
module root because the DEV runner rebuilds there.

Install validates the contract before writing anything: it refuses to run as
root, refuses a binary or working directory this user cannot execute, refuses a
foreign `zen.service`, and refuses when the state directory is already locked
by a process outside the installed unit's own systemd cgroup, or when the
listen address is unavailable. It never kills those processes; stop them
first. After `enable --now` (or `restart` for a changed contract) it verifies
that the unit is active, its main process is the installed binary, a process
inside the unit's own cgroup holds the installed state's lifecycle lock, and
`/health` on the installed address serves the installed state's `daemon_id`.
Only then does it report success. Re-running `install` with the same contract
leaves a healthy daemon running; any changed runtime context (binary, state,
address, working directory, `HOME`/`PATH`) updates the unit and restarts it
explicitly.

The unit is enabled into `default.target` without `After=default.target`: a
target already orders itself after the units it wants, so that line would form
an ordering cycle at boot and drop the implicit ordering. Ownership is bound
to the unit, not to a same-binary guess: an active unit that owns a different
state does not satisfy the cgroup-lock and identity checks above.
`KillMode=process` is intentional: the daemon reuses the user's ordinary tmux
server, a shared per-user resource, so stopping or restarting `zen.service`
terminates only the daemon and never tears down tmux or Worker sessions; tmux,
Worker sessions and the pairing/state files are outside the unit's lifecycle.

Known dependency: the DEV runner (`zen-dev`) binds its daemon child to the
watcher lifetime on Linux (`PR_SET_PDEATHSIG`), so an abrupt watcher death
stops the child and releases the state lock. Even with that binding, `zen boot
install` and `uninstall` treat a process inside the unit cgroup that still
holds the state lock as an own leftover and refuse or retain the configuration
instead of deleting it; real guest acceptance of the DEV crash path is still
owned by the runtime worker.

Lingering starts the unit before an interactive login (`zen boot install`
reports `sudo loginctl enable-linger <user>` when it cannot enable it itself).
`zen boot status` always reads the installed unit configuration rather than the
invocation defaults, shows the installed binary hash, and attributes `/health`
to the installed state identity. `zen boot uninstall` requires readable
installed metadata, stops the unit, confirms it is no longer active and that
no process inside (or unattributable to) the unit still holds the state lock,
then disables and removes only its own unit and metadata; on any metadata,
stop, disable or ownership failure — including a still-held lifecycle lock
whose holder cannot be attributed — it retains the unit and configuration for
a retry and never deletes a running owner. Daemon state and pairing are never
touched.

## Docker (advanced)

See `daemon/Dockerfile`. Prefer the host binary so tmux and local agent CLIs share the same environment. Container use requires mounting state, publishing or proxying port 9876, and arranging access to host agent tools yourself.
