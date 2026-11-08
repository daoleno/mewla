#!/usr/bin/env python3
"""Merge the per-site, per-invocation parts of a WebArena-Verified run (wav/run.sh --no-convert --keep) into one aub/1
result file per tool layer.

  assemble.py --stamp 20261108 --note "..." LABEL=DIR [LABEL=DIR ...]

DIR is a kept work dir of run.sh (it holds <site>-<side>.json); LABEL names its trial archive (the trial directories live in
<LABEL>/ next to the results, and each row's extra.dir says which). The parts of one side are merged in the order given;
the first one's meta (transport, tool version) is the run's.
"""
import argparse, glob, json, os, shutil, subprocess, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
AUB = os.path.join(HERE, "..", "aub.py")
RUN_ID = {"mewla": "wav-hard-mewla_chrome-bg", "vscreen": "wav-hard-mewla_chrome-virtual_screen", "bu": "wav-hard-browser-use-fg"}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--stamp", required=True)
    ap.add_argument("--note", default="")
    ap.add_argument("--config", help="config.json of the run (default: the part's own)")
    ap.add_argument("--only-side", help="write only these sides (comma list: mewla,bu,vscreen)")
    ap.add_argument("--id-suffix", default="", help="added to every run id (e.g. -cold)")
    ap.add_argument("parts", nargs="+")
    a = ap.parse_args()
    by_side = {}
    for p in a.parts:
        label, d = p.split("=", 1)
        opts = d.split(",")
        d = opts[0]
        want = {k: set(v.split("+")) for k, v in (o.split(":", 1) for o in opts[1:])}  # e.g. main3=DIR,sides:bu+vscreen,sites:shopping
        for f in sorted(glob.glob(os.path.join(d, "*-*.json"))):
            name = os.path.basename(f)[:-5]
            if name.split("-")[0] not in ("shopping_admin", "shopping", "reddit", "gitlab") and not name.startswith("shopping_admin"):
                continue
            # Every part is graded again by guard.mjs from its trials' recorded calls, so one rule judges every tool layer.
            tmpf = os.path.join(tempfile.gettempdir(), "aub-regrade-%d-%s" % (os.getpid(), os.path.basename(f)))
            shutil.copy(f, tmpf)
            cfg = a.config or os.path.join(d, "config.json")
            subprocess.run(["node", os.path.join(HERE, "regrade.mjs"), cfg, tmpf], check=True)
            doc = json.load(open(tmpf))
            os.remove(tmpf)
            site = next((s for s in ("shopping_admin", "shopping", "reddit", "gitlab") if name.startswith(s + "-")), None)
            if ("sides" in want and doc["side"] not in want["sides"]) or ("sites" in want and site not in want["sites"]) or (a.only_side and doc["side"] not in a.only_side.split(",")):
                continue
            for r in doc["rows"]:
                if (r.get("extra") or {}).get("dir"):
                    r["extra"]["dir"] = "%s/%s" % (label, os.path.basename(r["extra"]["dir"]))
            by_side.setdefault(doc["side"], []).append(doc)
    tmp = tempfile.mkdtemp(prefix="aub-assemble.")
    for side, docs in by_side.items():
        merged = dict(docs[0], rows=[r for d in docs for r in d["rows"]])
        merged["meta"] = dict(docs[0].get("meta") or {}, notes=a.note)
        path = os.path.join(tmp, side + ".json")
        json.dump(merged, open(path, "w"))
        subprocess.run([sys.executable, "-I", AUB, "convert-web", "--run-id", "%s-%s%s" % (a.stamp, RUN_ID[side], a.id_suffix), path], check=True)
        os.remove(path)
    os.rmdir(tmp)


if __name__ == "__main__":
    main()
