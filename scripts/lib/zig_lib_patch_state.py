#!/usr/bin/env python3
"""Say whether a unified-diff patch is applied to a tree, by reading the text.

Usage: zig_lib_patch_state.py LIB_DIR PATCH_FILE

Prints one word:
  unpatched  every hunk's old text is in the file and its new text is not
  applied    every hunk's new text is in the file and its old text is not
  mismatch   anything else: the file changed under the patch, or is missing

This is what the Zig/Xcode compat scripts use instead of `patch --reverse
--dry-run`: macOS ships Apple's patch 2.0-12u11, which answers "Unreversed (or
previously applied) patch detected! Ignore -R?" with yes on a tree that is not
patched, so it calls a stock header patched.
"""

import pathlib
import sys


def hunks(patch_text):
    """Yield (path, old_text, new_text) for each hunk of a unified diff."""
    path = None
    old = new = None
    for line in patch_text.splitlines():
        if line.startswith("+++ "):
            name = line[4:].split("\t")[0]
            path = name[2:] if name.startswith("b/") else name
        elif line.startswith("--- "):
            continue
        elif line.startswith("@@"):
            if old is not None:
                yield path, old, new
            old, new = [], []
        elif old is not None and line[:1] in (" ", "-", "+"):
            if line[0] != "+":
                old.append(line[1:])
            if line[0] != "-":
                new.append(line[1:])
    if old is not None:
        yield path, old, new


def block_in(lines, text):
    block = "\n".join(lines) + "\n"
    return block in text


def state(lib_dir, patch_file):
    results = set()
    for path, old, new in hunks(pathlib.Path(patch_file).read_text()):
        target = pathlib.Path(lib_dir) / path
        try:
            text = target.read_text()
        except OSError:
            return "mismatch"
        has_old, has_new = block_in(old, text), block_in(new, text)
        if has_old and not has_new:
            results.add("unpatched")
        elif has_new and not has_old:
            results.add("applied")
        else:
            return "mismatch"
    return results.pop() if len(results) == 1 else "mismatch"


if __name__ == "__main__":
    print(state(sys.argv[1], sys.argv[2]))
