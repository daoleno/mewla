# Services

The **Services** sheet in the app lists the network services your agents
started: development servers, previews, APIs. It shows them by project and can
give one a temporary public URL.

## What appears

| Source | Shown when |
| --- | --- |
| **Session** | A process inside a live Zen Session is listening on a port. The row can open that Session's terminal. |
| **Persistent** | A registered user `systemd` service is running and listening. It has no terminal, because the Session that created it is gone. |

A service bound only to `127.0.0.1` shows its bind address and is marked local;
Zen does not invent a network URL for it. A registered service that stopped
shows as **Inactive**, and one whose state cannot be read shows **Error** with
the reason.

The CLI reads the same list:

```sh
zen service list --json
```

## Keep a service after its Worker ends

A service started outside `tmux`, for example as a user `systemd` unit, is
invisible until it is registered. Registering never starts, stops or changes
the unit:

```sh
zen service register -unit my-preview.service -name "Docs preview" \
  -project docs -port 3080 -cwd ~/projects/docs
zen service unregister -unit my-preview.service   # removes the registration only
```

Only plain user unit names are accepted. The registration survives daemon
restarts and Worker cleanup, and Zen rechecks on every refresh that the unit
is really running and owns the port. Registration needs Linux with a user
`systemd`; elsewhere it reports that it is unsupported.

## Temporary public URLs

A service row can start a temporary Cloudflare **Quick Tunnel**, which gives
that one service a random public `trycloudflare.com` URL. You need
`cloudflared` installed on the computer; no Cloudflare account is required.

- Starting a tunnel is always your explicit action, and exposes only the
  selected service. Zen first checks that the service answers HTTP.
- **Anyone with the URL can reach the service.** Do not tunnel anything that
  holds secrets or has no login of its own.
- The tunnel follows the exact process that owns the port. If that process
  exits, the tunnel stops; stopping a tunnel never stops the service.
- URLs are temporary and kept in memory only. After a daemon restart there is
  no tunnel until you start one again.
- Cloudflare limits Quick Tunnels to 200 concurrent requests and does not
  support Server-Sent Events.

Zen uses an empty `cloudflared` configuration for these tunnels and does not
touch your own named tunnels or `~/.cloudflared`. On non-Linux daemons, Quick
Tunnels report that they are unsupported.

From the computer, use the service ID and generation shown by
`zen service list --json`:

```sh
zen service tunnel start  -id SERVICE_ID -generation PROCESS_GENERATION
zen service tunnel status -id SERVICE_ID -generation PROCESS_GENERATION
zen service tunnel stop   -id SERVICE_ID -generation PROCESS_GENERATION
```
