#!/usr/bin/env python3
"""Wall-clock, model turns, tool calls (with per-call latency) and tokens from an
agent's own session log. Standard library only. Prints one small JSON object;
no prompt, answer or tool-output text is ever included.

  metrics.py claude FILE.jsonl   a Claude Code transcript (~/.claude/projects/**)
  metrics.py codex  FILE.jsonl   a Codex session (~/.codex/sessions/**)
  --since ISO / --until ISO      keep only events in a window (one task out of a long session)
"""
import json, sys, datetime, collections


def ts(s):
    return datetime.datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()


def pct(xs, p):
    xs = sorted(xs)
    return round(xs[min(len(xs) - 1, max(0, int(round(p / 100 * len(xs) + 0.4999)) - 1))], 3) if xs else None


def events(path, since, until):
    for line in open(path):
        try:
            o = json.loads(line)
        except ValueError:
            continue
        t = o.get("timestamp")
        if not t:
            continue
        t = ts(t)
        if (since and t < since) or (until and t > until):
            continue
        yield t, o


def finish(first, last, turns, calls, tokens, extra=None):
    by = collections.defaultdict(list)
    for name, secs in calls:
        by[name].append(secs)
    out = {"wall_s": round(last - first, 2) if first else None, "turns": turns, "tool_calls": len(calls),
           "tools": {k: {"n": len(v), "p50_s": pct(v, 50), "p95_s": pct(v, 95), "sum_s": round(sum(v), 2)}
                     for k, v in sorted(by.items())},
           "tool_time_s": round(sum(s for _, s in calls), 2), "tokens": tokens}
    out["model_time_s"] = round(out["wall_s"] - out["tool_time_s"], 2) if first else None
    out.update(extra or {})
    return out


def claude(path, since, until):
    first = last = None
    seen, pending, calls = set(), {}, []
    tokens = collections.Counter()
    for t, o in events(path, since, until):
        first = first or t
        last = t
        msg = o.get("message") or {}
        if o.get("type") == "assistant":
            mid = msg.get("id") or o.get("uuid")
            if mid not in seen:
                seen.add(mid)
                u = msg.get("usage") or {}
                for k in ("input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"):
                    tokens[k] += u.get(k, 0) or 0
            for c in msg.get("content") or []:
                if isinstance(c, dict) and c.get("type") == "tool_use":
                    pending[c["id"]] = (c["name"], t)
        elif o.get("type") == "user":
            for c in msg.get("content") or []:
                if isinstance(c, dict) and c.get("type") == "tool_result" and c.get("tool_use_id") in pending:
                    name, t0 = pending.pop(c["tool_use_id"])
                    calls.append((name, t - t0))
    tokens["total"] = sum(tokens.values())
    return finish(first, last, len(seen), calls, dict(tokens))


def codex(path, since, until):
    first = last = None
    pending, calls = {}, []
    turns, tokens, started, done = 0, {}, None, None
    meta = {}
    for t, o in events(path, since, until):
        p = o.get("payload") or {}
        k = p.get("type")
        if o.get("type") == "session_meta":
            meta = {"originator": p.get("originator"), "cli_version": p.get("cli_version")}
        if o.get("type") == "turn_context" and p.get("model"):
            meta["model"] = p["model"]
        if k == "task_started":
            started = started or t
        if k == "task_complete":
            done = t
        if o.get("type") == "response_item" and k in ("function_call", "custom_tool_call"):
            pending[p.get("call_id")] = (p.get("name") or k, t)
        elif o.get("type") == "response_item" and k in ("function_call_output", "custom_tool_call_output"):
            if p.get("call_id") in pending:
                name, t0 = pending.pop(p["call_id"])
                calls.append((name, t - t0))
        elif o.get("type") == "event_msg" and k == "mcp_tool_call_end":
            inv = p.get("invocation") or {}
            d = p.get("duration") or {}
            secs = d.get("secs", 0) + d.get("nanos", 0) / 1e9 if isinstance(d, dict) else 0
            calls.append(("mcp:%s.%s" % (inv.get("server"), inv.get("tool")), secs))
            pending.pop(p.get("call_id"), None)
        if o.get("type") == "event_msg" and k == "token_count" and p.get("info"):
            turns += 1
            tokens = (p["info"].get("total_token_usage") or tokens)
        first, last = first or t, t
    return finish(started or first, done or last, turns, calls, tokens, meta)


if __name__ == "__main__":
    a = sys.argv[1:]
    since = until = None
    for flag in ("--since", "--until"):
        if flag in a:
            i = a.index(flag)
            v = ts(a[i + 1])
            since, until = (v, until) if flag == "--since" else (since, v)
            del a[i:i + 2]
    if len(a) != 2 or a[0] not in ("claude", "codex"):
        sys.exit(__doc__)
    print(json.dumps((claude if a[0] == "claude" else codex)(a[1], since, until), separators=(",", ":")))
