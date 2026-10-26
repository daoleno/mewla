# Friends (Labs): what exists

Friends is a Labs feature, off by default: a person's Brain writes short posts about finished Work, and the person reads, edits and posts them. Only slices 0 and 1 are built. Nothing is deployed, nothing posts, and there is no timeline. Public docs and the privacy policy do not mention it yet; they change when the service ships.

## Pieces

- `daemon/reqsign`: the signed-request code shared by hosted services (payload, `Authorization` header, nonce cache, rate limiter). `AccountScheme` keeps the bytes installed daemons already sign with; a test pins them.
- `daemon/friendsvc`: the hosted service, mounted by `mewla-account` under `/friends` with its own SQLite file (`-friends-db`). Off unless configured; a half-set configuration stops the service.
  - Two credentials that never overlap: a **human key** (generated on the phone, signs `MewlaFriendsHuman`) and an **agent key** (the daemon identity, signs `MewlaFriendsAgent`). A key registers as one class only.
  - Every route is in one table with a credential class; an untagged route cannot be built, and the matrix test sends every route every kind of request (own key, other class, unsigned, replay, expired, unknown key).
  - Sign-in is GitHub's web flow with no scope (id, login, created_at; the token is dropped). Accounts under 30 days are refused. The handle defaults to the login and can change once. A second phone on the same GitHub account is refused until device approval is built.
  - A link code (ten minutes, once, hashed at rest) lets a computer become the agent; linking a new computer replaces the old one on the service. Not built: the old computer keeps its local link and drafts until its next request is refused (401) and it clears itself; the design wants the old link replaced and its drafts discarded.
- `daemon/friends`: the scrubber, the private vocabulary of this computer, the service client and the local link and draft files (`<state>/friends`).
- `mewla friends status | link | draft | drafts`: there is no command to read, like, follow or comment. `draft` keeps a scrubbed post for the owner and posts nothing.
- `playbooks/friends.md`: seeded into the Brain workspace, and gated on `mewla friends status --json` saying `linked`.
- App: Settings, Labs, Friends. The phone's key lives in the keychain or keystore. The browser's return link (`mewla://friends/enroll`) is handled by a Linking listener (also at cold start), because Android may restart the app while the browser is in front. The emulator run completed sign-in and link, but it was not checked whether the listener or the in-memory browser result finished it, so the restart path itself is unverified.

## The scrubber

Deterministic and refuses rather than rewrites. Format rules: IPs, internal host names, domains, URLs, emails, phones, key and token shapes, JWTs, key blocks, credential assignments, UUIDs, hashes, random-looking strings, paths, credential files, @mentions, hidden characters. Known terms: host, user, home, Work project folders, project directories (usual roots and every project Watch has recorded) with their git remotes and GitHub owner and repository, connected accounts, paired computers, saved addresses. Matching ignores case and separators (`FooBar`, `foo-bar`, `foo bar`).

It cannot catch people's names that are not in the vocabulary, clients that never appear on the computer, anything private by meaning, identifiers written to dodge patterns, and terms under four letters or generic words (`app`, `daemon`). A person reads every draft unless they turned auto-post on.

## Running it isolated

The proof used an isolated `mewla-account` (`-friends-db`, `-friends-dev-fake-github`, a dummy `-relay`, `-dev-fake-login`, `-dns-provider challtestsrv`) and an isolated daemon (own HOME, `-state-dir`, `TMUX_TMPDIR`, port, and `MEWLA_FRIENDS_URL=http://127.0.0.1:<port>/friends`). The app takes the service address from `EXPO_PUBLIC_MEWLA_FRIENDS_URL` at bundle time. `-friends-dev-fake-github` trusts the client's claimed login: development only.
