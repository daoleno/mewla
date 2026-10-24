# Google sign-in exchange (Google Calendar and Gmail)

Google Calendar and Gmail sign in through an exchange that runs inside the account service (`daemon/cmd/mewla-account`, package `daemon/googleauth`) at `https://connect.mewla.ai/google/*`. It exists because Google's Web client has a secret and a daemon must never hold it: users run their own daemons, a phone's browser cannot reach a daemon's loopback, and Google has retired custom-scheme and out-of-band returns. Because the daemon polls the exchange, the same flow works from the phone app and the web UI, with no per-daemon redirect URI.

Why not the alternatives: a **Desktop** client needs a loopback redirect, which the phone's browser cannot reach on a daemon; Android and iOS clients only fit one app and not the web UI; shipping a **Web** client's secret in an open-source binary breaks Google's rule that it stays confidential.

## Configuration

`mewla-account` mounts the exchange only on its public HTTPS listener (`-tls-addr`) and only when every value is set (a partial set stops the service at start):

| Name | Where |
| --- | --- |
| `MEWLA_ACCOUNT_GOOGLE_CLIENT_ID` | `env.clear` in `config/deploy.yml` (public) |
| `MEWLA_ACCOUNT_GOOGLE_CLIENT_SECRET` | Kamal secret, read by `.kamal/secrets` from `~/.config/mewla-remote/google-oauth-client.json` (`jq -r .web.client_secret`) |
| `MEWLA_ACCOUNT_GOOGLE_RECEIPT_KEY` | Kamal secret, from `~/.config/mewla-remote/google-receipt-key`: 32 random bytes, base64url without padding. Losing it forces every Google account to reconnect |
| `MEWLA_ACCOUNT_GOOGLE_GMAIL_CLIENT_ID`, `MEWLA_ACCOUNT_GOOGLE_GMAIL_CLIENT_SECRET` | Optional pair, `config/deploy.yml` and a Kamal secret. Set both only after Gmail moves to its own Cloud project; unset means Gmail signs in with the client above |

**Google verifies a Cloud project, not a scope.** Calendar's scopes are sensitive and Gmail's are restricted (yearly CASA), so keeping them in one project couples Calendar's approval to Gmail's assessment. The exchange therefore takes one Web client per plugin (`googleauth.Client`, keyed by service), which lets Gmail move to a second project without touching Calendar's sign-in. The two may share a project and client; the code never assumes they differ. Both clients register the same redirect URI, `https://connect.mewla.ai/google/callback`; the flow's own state, S256 PKCE and browser cookie bind the callback, and a signed refresh receipt names the client ID and service so a receipt minted in one project cannot drive the other.

Official daemons carry only the public origin: `release/plugin-publishers.json` → `google.exchange_origin` → `connections.GoogleExchangeOrigin` (`scripts/plugin-publisher-flags.py`). No client ID or secret reaches a user daemon.

## Google Cloud project

Project `mewla-50425` (the Firebase project). Enable the Gmail API and the Google Calendar API. Branding: app name Mewla, home page `https://mewla.ai/`, privacy policy `https://mewla.ai/privacy/`, authorized domain `mewla.ai`. One **Web application** client, named `Mewla sign-in`, with no JavaScript origins and exactly one redirect URI: `https://connect.mewla.ai/google/callback`.

Scopes, by plugin (`googleauth.Scopes`): Calendar `calendar.calendarlist.readonly`, `calendar.events.readonly`, `calendar.freebusy`, plus `calendar.events` when changes are allowed; Gmail `gmail.readonly`, plus `gmail.modify` when changes are allowed; both with `openid` and `email`. The sets never overlap, so a Calendar sign-in never asks for mail access and the reverse.

Publishing status is **Production**. Data Access verification was submitted on 2026-10-24 covering both plugins and is under review. Calendar's scopes are sensitive (verification only). Gmail's are restricted: verification plus a yearly third-party CASA security assessment, because the exchange can reach the tokens. `worklog/mewla-gmail-no-casa/DESIGN.md` holds the reasoning and the options.

### Moving Gmail to its own project

Google's review, user cap and CASA answer all attach to the project, so this is a console change plus one secret pair; nothing else in the code changes.

1. New Cloud project. Enable the Gmail API. Fill Branding exactly as above (same app name, homepage, privacy policy, authorized domain) and publish it. Branding must be published before Data Access can be requested.
2. Create a **Web application** client named `Mewla Gmail sign-in`, no JavaScript origins, one redirect URI: `https://connect.mewla.ai/google/callback`. Download its JSON.
3. Declare only the Gmail scopes in the new project's Data Access, and remove `gmail.readonly`/`gmail.modify` from the old project's Data Access, leaving only the Calendar scopes.
4. Add `MEWLA_ACCOUNT_GOOGLE_GMAIL_CLIENT_ID` to `config/deploy.yml` `env.clear` and `MEWLA_ACCOUNT_GOOGLE_GMAIL_CLIENT_SECRET` to `.kamal/secrets`. Both or neither: a half-set pair stops the account service at start.
5. Deploy. A refresh receipt names the client that minted it and the service, and the exchange honours a receipt only for its own client of that service, so **Gmail accounts connected before the move must reconnect once** ("Reconnect this Google account" on their next refresh). Calendar connections are unaffected.
6. Submit the new project for Gmail's Data Access review separately. Its 100-user cap is its own: the cap counts new users over the project's whole lifetime and cannot be reset. Check usage under APIs & Services → OAuth consent screen → OAuth user cap.



## Protocol

Five fixed routes: start, browser authorize, Google callback, status/cancel, and refresh. No caller supplies a provider, token endpoint, scope string, callback URL or daemon URL; the only choice a daemon makes is the `service` (`calendar` or `gmail`), which selects a fixed scope set. The daemon generates a NaCl box keypair and a random nonce. The exchange receives the public key and nonce over HTTPS. Its browser flow uses its own single-use state, S256 PKCE and a Secure/HttpOnly/SameSite cookie. The callback exchanges the code with the secret and seals the token pair to the daemon's public key. A phone browser is returned to `mewla://plugins`; any other browser gets a page saying the tab can be closed. The page carries no code, token or retrieval capability.

Only the daemon holding the nonce can poll or cancel, and only its private key can open the result. The daemon checks the nonce, the identity and the granted scopes, saves the tokens in its vault, and cancels the pending entry. Unfinished results expire in ten minutes.

Refresh keeps no user-token database. The exchange issues an HMAC receipt bound to the public key, client ID, service, a hash of the refresh token and a hash of a random proof sealed to the daemon. A refresh must present the receipt, the matching refresh token and the proof; the new result is sealed to the same key. A restart keeps working because only the receipt key is durable. The receipt is bearer authority and lives only in the daemon vault.

Requests and bodies are bounded, redirects are denied, `start` and `refresh` are rate limited per peer (behind the relay the PROXY-protocol header gives the real client address), and no CORS allowance exists. Pending flows are in memory and fail on restart. Operations are serialized under one lock, which suits the Testing-mode volume; review capacity before a wide launch. It is not independently audited.

## Deploy and roll back

Run from a checkout that has `.kamal/secrets` and `.kamal/files/` (see the [relay runbook](mewla-link-relay.md#operations-kamal)). Before deploying, record the current behaviour of `connect.mewla.ai`: `/plugins/return` forwards `code`/`state`/`error` to `mewla://plugins`, `/device` redirects to GitHub's device page, and the machine registry answers. After deploying, check the same things and `/google/start` (JSON `POST` only: a `GET` is 400). To roll back, redeploy the previous version: `kamal deploy --version <previous SHA> --skip-push` (the old image is still on the host), which drops the exchange; the secrets may stay.
