# Resource telemetry contract (v2)

Zen exposes a read-only machine resource snapshot over the authenticated daemon
WebSocket. Clients send `{ "type": "get_resource_telemetry", "request_id":
"…" }`; the daemon replies with `resource_telemetry` and the same
`request_id`. The endpoint is poll-friendly: clients request the latest cached snapshot every
five seconds; reads never trigger another sample.

The response is versioned and Linux-first. Unsupported platform measurements
are omitted rather than reported as zero.

```json
{
  "type": "resource_telemetry",
  "request_id": "optional-client-id",
  "version": 2,
  "sampled_at": "2026-10-01T06:00:00Z",
  "state": "normal|elevated|critical",
  "cpu": {
    "load1": 1.2, "load5": 0.9, "load15": 0.7,
    "utilization_percent": 18.4,
    "per_core_percent": [12.0, 24.8]
  },
  "memory": {
    "total_bytes": 34359738368,
    "available_bytes": 17179869184,
    "used_bytes": 17179869184,
    "cache_bytes": 4294967296,
    "shared_bytes": 123456,
    "swap_total_bytes": 0,
    "swap_used_bytes": 0
  },
  "psi": {
    "cpu": {"some": {"avg10": 0, "avg60": 0, "avg300": 0}},
    "memory": {"some": {"avg10": 0, "avg60": 0, "avg300": 0}, "full": {"avg10": 0, "avg60": 0, "avg300": 0}},
    "io": {"some": {"avg10": 0, "avg60": 0, "avg300": 0}, "full": {"avg10": 0, "avg60": 0, "avg300": 0}}
  },
  "disks": [{"mount": "/", "total_bytes": 0, "free_bytes": 0, "used_bytes": 0, "read_bytes_per_second": 0, "write_bytes_per_second": 0}],
  "consumers": [{
    "owner": "worker|brain|docker|user|orphaned_worker",
    "worker_id": "optional-session-id",
    "work_id": "optional-work-id",
    "title": "optional Work title",
    "status": "running|idle|done|unknown",
    "executor": "codex",
    "cwd": "/workspace",
    "age_seconds": 120,
    "rss_bytes": 1000000,
    "cpu_percent": 25.0,
    "kinds": ["qemu", "gradle", "chrome", "node"]
  }],
  "history": [{
    "sampled_at": "2026-10-01T05:59:55Z",
    "state": "normal",
    "cpu_percent": 18.4,
    "load15": 0.7,
    "memory_available_bytes": 17179869184,
    "memory_used_bytes": 17179869184,
    "swap_used_bytes": 0,
    "psi_cpu_some_avg10": 0.1,
    "psi_memory_some_avg10": 0.2,
    "psi_io_some_avg10": 0.3,
    "disk_read_bytes_per_second": 1048576,
    "disk_write_bytes_per_second": 524288
  }],
  "endpoint": "get_resource_telemetry"
}
```

PSI values are kernel pressure averages in percent. `history` is a bounded
30 minute ring buffer containing up to 360 samples at a five-second interval,
oldest first. Version 2 adds the compact history fields above so the app can
draw CPU, memory, swap, pressure and disk-throughput charts from one response.
All percentage values are 0–100, except consumer CPU (100% means one core).
`cpu_percent` is total machine utilization. Disk history rates sum each unique
sampled mount device once. Rates and unavailable metrics are omitted until a
valid sample interval exists; measured zero is retained. `memory_used_bytes`
is total minus available, and `swap_used_bytes` is swap total minus swap free.
The ring is in memory and restarts empty. No synthetic backfill is generated. Consumers are
best-effort attribution from process ownership markers, the Brain host, Docker
metadata, and remaining user processes. A consumer with `owner=orphaned_worker`
is a process tree left after a Worker reached `done` or was closed.

## Brain pressure events

When the hysteresis state changes, the daemon sends a compact `resource_pressure`
event through the same Brain event channel used by `zen_work_event`. The event
contains `state`, `previous_state`, `crossed` signal names, `trend`, machine
totals, the top attributed consumers, orphaned consumers, queue/in-flight
counts, `sampled_at`, and `snapshot_endpoint: "get_resource_telemetry"`.
Events are emitted on entry to `elevated` or `critical`, and once on recovery
to `normal`; sustained pressure and cooldowns prevent spam. The daemon never
kills or throttles a process in response to this event. Brain decides whether
to dispatch, ask a Worker to release resources, or close a Worker.

Defaults require 20 seconds of sustained pressure and use a 60 second
notification cooldown. See the threshold configuration below.

## Transport and data details

The implemented transport is cached polling: request `get_resource_telemetry`
on the current server's existing authenticated WebSocket every five seconds.
There is no unconditional telemetry broadcast. `GET /resources` returns the
same flat response with the existing signed device authentication and purpose
`zen-resource-telemetry`; responses use `Cache-Control: no-store`. The local
control socket exposes the same snapshot through `zen resources --json` under
`resource_telemetry`. Before the first sample, the WebSocket returns
`resource_telemetry_unavailable` and HTTP returns 503. Changing the app's current
server must clear its previous history and polling state.

Consumers additionally include `id` (stable group key), `process_count`,
`commands` (bounded executable labels), and up to five largest `processes`:
`{ "pid": 123, "start": "1234567", "command": "qemu-system-x86", "rss_bytes": 123 }`.
`start` is an opaque kernel process-generation token. Use it with
`zen worker release -id SESSION -pid PID -start START` to release a selected
owned tool tree. This command refuses stale/unowned processes and the provider's
ancestor chain. Worker close/Work acceptance remain the full cleanup commands.
No command-line arguments or environment variables are exposed by telemetry.
Java is labelled `gradle` as a notable workload family, without claiming all Java
processes are Gradle. Container identifiers come from cgroup paths; no Docker
socket or Docker CLI polling is performed. RSS sums may double-count shared
pages. CPU rates are interval deltas, with a consumer's 100% representing one
CPU core. The first interval has no CPU or IO rates.

Linux reads `/proc/stat`, `/proc/loadavg`, `/proc/meminfo`, PSI, process metadata,
`/proc/diskstats`, and local mount metadata. Relevant mounts are `/` plus local
ext4/xfs/btrfs/zfs filesystems, deduplicated by device; pseudo and network mounts
are excluded. IO rates are per mount's block device when a matching counter
exists, and omitted on overlay filesystems without an exposed block counter.
The sampler never shells out on Linux. Processes are sampled once per interval,
with environment ownership fields cached by PID/start identity. Network,
temperature and GPU metrics are outside this version. macOS currently returns
`unavailable` measurement names with empty disks/consumers and continues to
compile; the app must render these as unavailable, never zero.

`signals` contains active threshold conditions as
`{ "name": "psi.memory.some.avg10", "value": 3, "threshold": 2, "state": "elevated" }`.
A transient crossed signal can appear while the sustained `state` remains normal.

## Threshold configuration

The defaults use the following elevated/critical thresholds. High-water signals
trigger at or above the value; low-water signals trigger at or below it.

| Signal | Elevated | Critical |
| --- | ---: | ---: |
| Available memory (% total, low) | 15 | 7 |
| Total CPU utilization (%) | 90 | 98 |
| CPU PSI some avg10 (%) | 20 | 60 |
| Memory PSI some avg10 (%) | 2 | 10 |
| Memory PSI full avg10 (%) | 1 | 5 |
| IO PSI full avg10 (%) | 5 | 20 |
| Free disk (% capacity, low) | 10 | 3 |

A higher state requires 20 seconds of continuous pressure. Recovery/downgrade
requires 30 seconds and 20% headroom beyond the active threshold. Re-entry into
the same elevated/critical state has a 60-second cooldown; recovery always gets
one event. Continued pressure in the same state produces no repeat events.
Threshold state, cooldown timestamps and pending events persist under the daemon
state directory, so hot reload cannot repeatedly notify the same sustained state.

Override defaults with the `ZEN_RESOURCE_THRESHOLDS` JSON environment variable,
or an optional `resource-telemetry.json` file in the daemon state directory
(normally `~/.zen`). The file is checked every sample and overrides the environment
without restarting the daemon. Removing it restores environment/default values.
Malformed or invalid files retain the last valid configuration. Unknown JSON
fields must be avoided. Example (partial overrides are supported):

```json
{
  "available_memory_percent": { "elevated": 15, "critical": 7 },
  "cpu_busy_percent": { "elevated": 90, "critical": 98 },
  "cpu_some": { "elevated": 20, "critical": 60 },
  "memory_some": { "elevated": 2, "critical": 10 },
  "memory_full": { "elevated": 1, "critical": 5 },
  "io_full": { "elevated": 5, "critical": 20 },
  "disk_free_percent": { "elevated": 10, "critical": 3 },
  "sustain_seconds": 20,
  "recovery_seconds": 30,
  "cooldown_seconds": 60
}
```

Sustain/recovery durations accept 15–600 seconds; cooldown accepts 30–3600 seconds.
Thresholds must be positive and at most 100, with ordered elevated/critical
values. Notifications are `zen_work_event` envelopes with `kind=resource_pressure`
and an embedded `resource_pressure` object. They use the durable Work review
channel and exact delivery receipts, not user-input admission. Each transition
has a deterministic event identity and a separate bounded notification Work;
Brain can record its disposition with the existing Work commands. The compact
payload is bounded to top consumers and a few trend points; `zen resources`
and `get_resource_telemetry` expose the full cached view.

## Sampling cost acceptance

Sampling runs once every five seconds. API polls read the latest in-memory
snapshot; they never trigger a process scan. History is an in-memory ring and
is not rewritten to disk on each tick. Only pressure transitions and their
acknowledgements update the small durable notification state. Ownership leases
are updated only when the exact owned process identities change.

The opt-in Linux acceptance test `TestResourceSamplerLiveCost` measures six
samples at the production cadence, including `/proc` process attribution. It
requires mean CPU below 50 ms/tick (1% of one core at five seconds) and no
logical or physical writes during sampling. Run its compiled test binary
without Go's test-log instrumentation when measuring IO:

```sh
cd daemon
GOMAXPROCS=2 go test -c -o "$ZEN_BUILD_TMPDIR/resource-watcher.test" ./watcher
ZEN_VERIFY_RESOURCE_COST=1 GOMAXPROCS=2 "$ZEN_BUILD_TMPDIR/resource-watcher.test" \
  -test.run '^TestResourceSamplerLiveCost$' -test.v
```

Whole-daemon profiling must distinguish this sampler from provider usage
statistics and channel projections. Usage statistics retain validated sparse
metadata under `~/.cache/zen/usage-v1` across restarts; source identity, size,
modification time and timezone invalidate entries. This disposable cache never
contains transcript bodies. A summary younger than five minutes can serve a
quick restart; normal periodic collection resumes afterwards. Telegram polling
preserves its durable state file when no facts change, and Brain presentation
reuses provider readers for unchanged transcript sources.
