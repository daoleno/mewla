# Resource telemetry contract (v2)

Zen exposes a read-only machine resource snapshot over the authenticated daemon
WebSocket. Clients send `{ "type": "get_resource_telemetry", "request_id":
"…" }`; the daemon replies with `resource_telemetry` and the same
`request_id`. The daemon may also broadcast `resource_telemetry` messages when a
new sample is available. The stream is intentionally poll-friendly: clients
that do not want broadcasts can request a snapshot every few seconds.

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

Default thresholds are configurable with `ZEN_RESOURCE_*` environment
variables and are documented in the daemon configuration. Defaults require at
least 20 seconds of sustained pressure and use a 60 second notification
cooldown.
