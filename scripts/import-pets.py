#!/usr/bin/env python3
"""Import pet packs into the app and the landing site.

A pack is a directory of animated WebPs with a meta.json (see
docs/third-party-assets.md). This re-encodes every action to one canvas size
so the app bundle stays small, writes app/assets/pets/<id>/, regenerates
app/components/pets/petPacks.ts with static requires, and copies what the
landing needs into site/pets/: sprite strips of every pet's clips (state and
play clips, from the pack's registered frames/, the hops from frames-wide/
where the pack has them), and a small loop and a still of every pet for the
strip and the hero without scripts.

Usage:
  python3 scripts/import-pets.py [--site] [PETS_DIR]

--site rewrites only site/pets/ (from the app assets already imported).

PETS_DIR defaults to ~/workspace/mewla-cat-lab/imagegen/pets-codex and must hold
index.json plus one directory per pet. Requires Pillow.
"""

import json
import shutil
import sys
from pathlib import Path

import numpy as np
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
APP_ASSETS = ROOT / "app" / "assets" / "pets"
CATALOG = ROOT / "app" / "components" / "pets" / "petPacks.ts"
SITE = ROOT / "site" / "pets"
DEFAULT_SOURCE = Path.home() / "workspace" / "mewla-cat-lab" / "imagegen" / "pets-codex"

# One clip per Brain state (app/components/mewla/brainCatState.ts), plus the
# hop back into the seal. waking is the hop out.
CLIPS = ("homeless", "offline", "waking", "idle", "working", "delegating",
         "attention", "delivered", "going_back")
# The app canvas: the default pet is tall enough for a 120 pt pet on a 2x
# screen; the others are smaller so all ten fit the bundle budget.
APP_SIDE = 256
OTHER_SIDE = 192
APP_QUALITY = 78
PORTRAIT_SIDE = 256
# The landing's strip pets are small; its hero plays the default at full size.
SITE_STRIP_SIDE = 160
# The landing draws the default pet itself, frame by frame, from one sprite
# strip per clip (state clips and the play clips for its games).
SITE_SPRITE_SIDE = 256
SITE_SPRITE_QUALITY = 80
# Clips the landing moves along the floor: their frames advance with distance.
GAITS = ("walk", "run", "stalk")
GROUND = 232


def frames_of(path):
    """Every frame of an animated WebP with its own hold in ms."""
    frames, durations = [], []
    with Image.open(path) as image:
        loop = image.info.get("loop", 0)
        for index in range(getattr(image, "n_frames", 1)):
            image.seek(index)
            image.load()
            frames.append(image.convert("RGBA").copy())
            durations.append(int(image.info.get("duration") or 100))
    return frames, durations, loop


def encode(frames, durations, loop, side, path, quality=APP_QUALITY):
    sized = [frame if frame.size == (side, side) else frame.resize((side, side), Image.Resampling.LANCZOS)
             for frame in frames]
    path.parent.mkdir(parents=True, exist_ok=True)
    sized[0].save(path, save_all=True, append_images=sized[1:], duration=durations,
                  loop=0 if loop else 1, quality=quality, method=6)


def square(image):
    """Pad to a square canvas, bottom-aligned, so every action shares a ground line."""
    side = max(image.size)
    if image.size == (side, side):
        return image
    canvas = Image.new("RGBA", (side, side))
    canvas.alpha_composite(image, ((side - image.width) // 2, side - image.height))
    return canvas


def import_pet(source, pet_id):
    meta = json.loads((source / pet_id / "meta.json").read_text())
    side = APP_SIDE if meta.get("default") else OTHER_SIDE
    actions = meta.get("actions", {})
    missing = [name for name in CLIPS if name not in actions]
    if missing:
        raise SystemExit(f"{pet_id}: meta.json lacks actions {missing}; has {sorted(actions)}")
    out = APP_ASSETS / pet_id
    shutil.rmtree(out, ignore_errors=True)
    entries = {}
    for name in CLIPS:
        spec = actions[name]
        frames, durations, _ = frames_of(source / pet_id / spec["file"])
        frames = [square(frame) for frame in frames]
        loop = bool(spec.get("loop", True))
        encode(frames, durations, loop, side, out / f"{name}.webp")
        entries[name] = {"loop": loop, "durationMs": sum(durations)}
    with Image.open(source / pet_id / "portrait.png") as portrait:
        still = square(portrait.convert("RGBA")).resize((PORTRAIT_SIDE, PORTRAIT_SIDE), Image.Resampling.LANCZOS)
        still.save(out / "portrait.webp", quality=85, method=6)
    return {
        "id": pet_id,
        "name": meta["name"],
        "default": bool(meta.get("default")),
        "actions": entries,
    }


def feet(mask):
    """The paws on the ground line: (centre x, width) of each run of contact."""
    band = mask[GROUND - 7:GROUND + 3].any(0)
    edges = np.diff(np.concatenate([[0], band.astype(int), [0]]))
    return [((a + b) / 2, b - a) for a, b in zip(np.nonzero(edges == 1)[0], np.nonzero(edges == -1)[0]) if b - a >= 6]


def gait(frames):
    """How a moving clip's drawings meet the floor, measured on the 256 canvas.

    contact: each frame's paw pixels on the ground line, 0-1 of the most.
    travel: canvas px the body moves forward per frame: how far a planted paw
      slides back from one drawing to the next (each paw is paired with the
      nearest one in the next drawing; a planted one slides back, a swinging
      one comes forward). The drawings hold a paw only roughly still, so it is
      the cycle's mean, the same for every frame. None when too few steps show
      a planted paw (an in-place gallop): the landing then spreads its own
      stride by contact instead.
    rise: how far each frame's top sits above the cycle's lowest, in px (the
      bob the drawings already have).
    """
    masks = [np.asarray(frame.getchannel("A")) > 60 for frame in frames]
    n = len(frames)
    slides = []
    for i in range(n):
        now, then = feet(masks[i]), feet(masks[(i + 1) % n])
        back = []
        for centre, width in now:
            if not then:
                break
            centre2, width2 = min(then, key=lambda paw: abs(paw[0] - centre))
            if -30 <= centre2 - centre <= 2 and 0.5 < width2 / width < 2:
                back.append(centre - centre2)
        if back:
            slides.append(max(back))
    mean = sum(slides) / len(slides) if slides else 0
    travel = [round(mean, 2)] * n if len(slides) >= n / 3 and mean >= 1.5 else None
    counts = [int(mask[GROUND - 7:GROUND + 3].any(0).sum()) for mask in masks]
    most = max(counts) or 1
    tops = [int(np.nonzero(mask.any(1))[0][0]) for mask in masks]
    return {"travel": travel, "contact": [round(c / most, 2) for c in counts],
            "rise": [max(tops) - t for t in tops]}


def write_catalog(pets):
    lines = [
        "// Generated by scripts/import-pets.py from the pet packs. Do not edit.",
        'import type { PetPack } from "./petModel";',
        "",
        "export const PET_PACKS: readonly PetPack[] = [",
    ]
    for pet in pets:
        base = f"../../assets/pets/{pet['id']}"
        lines += [
            "  {",
            f"    id: {json.dumps(pet['id'])},",
            f"    name: {{ en: {json.dumps(pet['name']['en'])}, zh: {json.dumps(pet['name']['zh'], ensure_ascii=False)} }},",
            f"    portrait: require(\"{base}/portrait.webp\"),",
            "    clips: {",
        ]
        for name, entry in pet["actions"].items():
            lines.append(
                f"      {name}: {{ source: require(\"{base}/{name}.webp\"), loop: {str(entry['loop']).lower()}, "
                f"durationMs: {entry['durationMs']} }},"
            )
        lines += ["    },", "  },"]
    default = next(pet["id"] for pet in pets if pet["default"])
    lines += ["];", "", f"export const DEFAULT_PET_ID = {json.dumps(default)};", ""]
    CATALOG.write_text("\n".join(lines))


def site_clips(source, pet_id):
    """Sprite strips of one pet's clips; returns their timing (and gait) by name. A hop the
    pack also wrote on a padded canvas (frames-wide/) keeps that room: its cells are
    side + 2 * pad wide, the ground line pad lower."""
    meta = json.loads((source / pet_id / "meta.json").read_text())
    clips = {}
    for name, spec in meta["actions"].items():
        pad = spec.get("wide", 0)
        folder = source / pet_id / ("frames-wide" if pad else "frames") / name
        frames = [Image.open(path).convert("RGBA") for path in sorted(folder.glob("*.png"))]
        if not frames:
            raise SystemExit(f"{pet_id}/{name}: no {folder.parent.name}/ in the pack; rebuild it")
        cell = SITE_SPRITE_SIDE + 2 * pad
        strip = Image.new("RGBA", (cell * len(frames), cell))
        for i, frame in enumerate(frames):
            strip.alpha_composite(frame if frame.size == (cell, cell) else frame.resize((cell, cell), Image.Resampling.LANCZOS),
                                  (cell * i, 0))
        target = SITE / pet_id / "sprites" / f"{name}.webp"
        target.parent.mkdir(parents=True, exist_ok=True)
        strip.save(target, quality=SITE_SPRITE_QUALITY, method=6, exact=True)
        clips[name] = {"frames": len(frames), "durations": spec.get("durations") or [100] * len(frames),
                       "loop": bool(spec.get("loop", True))}
        if pad:
            clips[name]["pad"] = pad
        if name in GAITS:
            clips[name].update(gait(frames))
    return clips


def write_site(source, pets):
    """The landing draws any pet from sprite strips and shows every pet in a strip."""
    shutil.rmtree(SITE, ignore_errors=True)
    default = next(pet for pet in pets if pet["default"])
    entries = []
    for pet in pets:
        clips = site_clips(source, pet["id"])
        # Without scripts (or with reduced motion) the hero shows these as plain images.
        for name in ("idle", "delegating"):
            shutil.copyfile(APP_ASSETS / pet["id"] / f"{name}.webp", SITE / pet["id"] / f"{name}.webp")
        frames, durations, _ = frames_of(APP_ASSETS / pet["id"] / "delegating.webp")
        encode(frames, durations, True, SITE_STRIP_SIDE, SITE / pet["id"] / "delegating-small.webp", quality=75)
        shutil.copyfile(APP_ASSETS / pet["id"] / "portrait.webp", SITE / pet["id"] / "portrait.webp")
        entries.append({"id": pet["id"], "name": pet["name"], "clips": clips})
    index = {
        "default": default["id"],
        "side": SITE_SPRITE_SIDE,
        "anchor": [128, 232],
        "pets": entries,
    }
    (SITE / "index.json").write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n")


def main():
    args = sys.argv[1:]
    site_only = "--site" in args
    args = [arg for arg in args if arg != "--site"]
    source = Path(args[0]) if args else DEFAULT_SOURCE
    order = json.loads((source / "index.json").read_text())
    ids = [entry if isinstance(entry, str) else entry["id"] for entry in order.get("pets", order)]
    if site_only:
        pets = [json.loads((source / pet_id / "meta.json").read_text()) for pet_id in ids]
        write_site(source, [{"id": pet["id"], "name": pet["name"], "default": bool(pet.get("default"))} for pet in pets])
        total = sum(f.stat().st_size for f in SITE.rglob("*.webp"))
        print(f"Wrote site/pets for {len(pets)} pets: {total / 1e6:.2f} MB")
        return
    pets = [import_pet(source, pet_id) for pet_id in ids]
    if sum(pet["default"] for pet in pets) != 1:
        raise SystemExit("exactly one pet must be the default")
    for stale in APP_ASSETS.iterdir():
        if stale.name not in ids:
            shutil.rmtree(stale)
    write_catalog(pets)
    write_site(source, pets)
    total = sum(f.stat().st_size for f in APP_ASSETS.rglob("*.webp"))
    print(f"Imported {len(pets)} pets: app/assets/pets {total / 1e6:.2f} MB")


if __name__ == "__main__":
    main()
