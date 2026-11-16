#!/usr/bin/env python3
"""What's New notes: check release-notes/<version>.json and write the copies the app and the daemon ship.

See scripts/lib/release_notes.py. With no flags it writes client/ui/whatsnew/notes.json and
daemon/whatsnew/notes.json; --check fails when either is not current; --require VERSION fails
when that version has no notes.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
import release_notes  # noqa: E402

if __name__ == "__main__":
    raise SystemExit(release_notes.main())
