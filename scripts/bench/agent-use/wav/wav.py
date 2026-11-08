#!/usr/bin/env python3
"""WebArena-Verified for the agent-use benchmark: sites, sampling and judging. Run it with the Python of the venv that
has `webarena-verified` installed (run.sh makes it); everything else of the benchmark uses the standard library only.

  wav.py site up|down|reset|status NAME ...     a site container, published only on a private port (proxy.py is in front)
  wav.py sample --out FILE [--n N] [--seed S] [--sites a,b] [--subset hard]
                                                a seeded sample, stratified by site and by task type
  wav.py judge --task ID --response FILE --har FILE [--config FILE]
                                                the WebArena-Verified checker (type-aware, no LLM): {score, status, ...}

What the agent is given is built from `agent-input-get` (the intent and the start URLs), never from the dataset file
that also holds the expected answers. See README.md for how the shortcut of reading the answers is closed and audited.
"""
import argparse, json, os, random, subprocess, sys, time, urllib.request

# name: public port (what the agent's browser opens, the site's own base URL), private port (the container),
# port inside the container, readiness path, memory cap, login the agent is told about (None: none needed).
SITES = {
    "shopping": dict(public=7770, private=17770, inner=80, ready="/customer/account/login", mem="4g", login=None),
    "shopping_admin": dict(public=7780, private=17780, inner=80, ready="/", mem="4g", login="admin:admin1234", path="/admin"),
    "reddit": dict(public=9999, private=19999, inner=80, ready="/", mem="3g", login=None),
    "gitlab": dict(public=8023, private=18023, inner=8023, ready="/users/sign_in", mem="8g", login=None),
}
# The accounts the WebArena sites are built with (the Verified runner's example config uses the same ones).
ACCOUNTS = {
    "shopping": "Sign in on the store with email emma.lopez@gmail.com and password Password.123 when a task needs an account.",
    "shopping_admin": "",  # signed in by the proxy (Magento's auto-login header), as the Verified runner does
    "reddit": "Sign in on the forum with username MarvelsGrantMan136 and password test1234 when a task needs an account.",
    "gitlab": "Sign in on GitLab with username byteblaze and password hello1234 when a task needs an account.",
}


def cname(site):
    return "aub-wav-" + site.replace("_", "-")


def sh(*a, check=True, **k):
    return subprocess.run(a, check=check, text=True, capture_output=True, **k)


def site_up(site, timeout=600):
    s = SITES[site]
    sh("docker", "rm", "-f", cname(site), check=False)
    sh("docker", "run", "-d", "--name", cname(site), "--memory", s["mem"], "--memory-swap", s["mem"],
       "-p", "127.0.0.1:%d:%d" % (s["private"], s["inner"]),
       "-e", "WA_ENV_CTRL_EXTERNAL_SITE_URL=http://localhost:%d" % s["public"],
       "am1n3e/webarena-verified-%s:latest" % site)
    t0 = time.time()
    while time.time() - t0 < timeout:
        try:
            req = urllib.request.Request("http://127.0.0.1:%d%s" % (s["private"], s["ready"]), headers={"Host": "localhost:%d" % s["public"]})
            # Not following redirects (Postmill's login bounces through a cookie check): any answer below 400 means it is up.
            class NoRedirect(urllib.request.HTTPRedirectHandler):
                def redirect_request(self, *a, **k):
                    return None
            try:
                status = urllib.request.build_opener(NoRedirect).open(req, timeout=10).status
            except urllib.error.HTTPError as e:
                status = e.code
            if status < 400:
                time.sleep(3)
                return round(time.time() - t0, 1)
        except Exception:
            pass
        time.sleep(3)
    raise SystemExit("site %s not ready after %ds" % (site, timeout))


def site_down(site):
    sh("docker", "rm", "-f", cname(site), check=False)


def cli(*args):
    """The webarena-verified CLI next to this interpreter; its log lines go to stdout, so results are written to files."""
    return subprocess.run([os.path.join(os.path.dirname(sys.executable), "webarena-verified"), *args], capture_output=True, text=True, check=True)


def sample(args):
    # The subset's ids, sites and task types come from the subset export; the text the agent gets is read through
    # agent-input-get, which has no answers in it.
    tmp = args.out + ".subset.tmp"
    cli("subset-export", "--name", "webarena-verified-" + args.subset, "--output", tmp)
    subset = json.load(open(tmp))
    os.remove(tmp)
    want = set(args.sites.split(","))
    pool = {}
    for t in subset:
        if len(t["sites"]) != 1 or t["sites"][0] not in want:
            continue
        pool.setdefault((t["sites"][0], t["eval"][0]["expected"]["task_type"]), []).append(t["task_id"])
    rnd = random.Random(args.seed)
    strata = sorted(pool)
    for k in strata:
        pool[k].sort()
        rnd.shuffle(pool[k])
    # Round-robin over the strata so the sample is as even across sites and task types as the pool allows.
    chosen, i = [], 0
    while len(chosen) < args.n and any(pool[k] for k in strata):
        k = strata[i % len(strata)]
        i += 1
        if pool[k]:
            chosen.append((k, pool[k].pop()))
    tmp = args.out + ".inputs.tmp"
    cli("agent-input-get", "--task-ids", ",".join(str(tid) for _, tid in chosen), "--output", tmp)
    inputs = {t["task_id"]: t for t in json.load(open(tmp))}
    os.remove(tmp)
    tasks = [{"id": tid, "site": site, "type": kind, "intent": inputs[tid]["intent"], "start_urls": inputs[tid]["start_urls"]} for (site, kind), tid in chosen]
    json.dump({"subset": args.subset, "seed": args.seed, "sites": sorted(want), "tasks": tasks}, open(args.out, "w"), indent=1)
    print("%d tasks: %s" % (len(tasks), {"/".join(k): sum(1 for kk, _ in chosen if kk == k) for k in strata}))


def config_for(path):
    cfg = {"environments": {("__" + s.upper() + "__"): {"urls": ["http://localhost:%d%s" % (v["public"], v.get("path", ""))],
                                                           **({"credentials": {"username": "admin", "password": "admin1234"}, "use_header_login": True} if s == "shopping_admin" else {})}
                            for s, v in SITES.items()}}
    json.dump(cfg, open(path, "w"), indent=1)


def judge(args):
    from pathlib import Path
    from webarena_verified import WebArenaVerified
    cfg = args.config
    if not cfg:
        raise SystemExit("--config required")
    wa = WebArenaVerified(config=Path(cfg))
    r = wa.evaluate_task(task_id=args.task, agent_response=Path(args.response), network_trace=Path(args.har))
    print(json.dumps({"score": r.score, "status": str(r.status), "error": getattr(r, "error_msg", None)}))


def audit(args):
    """Re-judge every trial directory of a run and print what the checker said about each failure: the assertion names and
    messages, the agent's status and the calls it made. This is where the failure causes of a report come from."""
    from pathlib import Path
    from webarena_verified import WebArenaVerified
    wa = WebArenaVerified(config=Path(args.config))
    out = []
    for d in sorted(Path(args.trials).iterdir()):
        if not (d / "agent_response.json").exists():
            continue
        n, arm, tid = d.name.split("-", 2)
        r = wa.evaluate_task(task_id=int(tid), agent_response=d / "agent_response.json", network_trace=d / "network.har")
        j = json.loads(r.model_dump_json())
        resp = json.load(open(d / "agent_response.json"))
        calls = json.load(open(d / "calls.json"))
        asserts = []
        for ev in j.get("evaluators_results", []):
            for a_ in (ev.get("assertions") or []):
                if a_.get("status") != "success":
                    asserts.append("%s: %s" % (a_.get("assertion_name"), "; ".join(a_.get("assertion_msgs") or [])[:240]))
        out.append({"trial": d.name, "arm": arm, "task": int(tid), "score": r.score, "agent_status": resp.get("status"), "agent_type": resp.get("task_type"),
                    "assertions": asserts, "calls": len(calls), "first_calls": [c["name"].split("__")[-1] + " " + json.dumps(c["input"])[:90] for c in calls[:3]]})
    json.dump(out, open(args.out, "w"), indent=1)
    print(len(out), "trials audited ->", args.out)


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("site"); s.add_argument("action", choices=["up", "down", "reset", "status", "config"]); s.add_argument("names", nargs="*")
    s.add_argument("--out")
    p = sub.add_parser("sample"); p.add_argument("--out", required=True); p.add_argument("--n", type=int, default=24); p.add_argument("--seed", type=int, default=1108)
    p.add_argument("--sites", default="shopping_admin,shopping,reddit,gitlab"); p.add_argument("--subset", default="hard")
    u = sub.add_parser("audit"); u.add_argument("--trials", required=True); u.add_argument("--config", required=True); u.add_argument("--out", required=True)
    j = sub.add_parser("judge"); j.add_argument("--task", type=int, required=True); j.add_argument("--response", required=True); j.add_argument("--har", required=True); j.add_argument("--config")
    a = ap.parse_args()
    if a.cmd == "site":
        if a.action == "config":
            return config_for(a.out)
        for n in a.names:
            if a.action in ("up", "reset"):
                print(n, "ready in", site_up(n), "s")
            elif a.action == "down":
                site_down(n)
            else:
                print(n, sh("docker", "ps", "--filter", "name=" + cname(n), "--format", "{{.Status}}").stdout.strip() or "down")
    elif a.cmd == "sample":
        sample(a)
    elif a.cmd == "audit":
        audit(a)
    else:
        judge(a)


if __name__ == "__main__":
    main()
