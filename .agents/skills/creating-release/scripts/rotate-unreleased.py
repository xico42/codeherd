#!/usr/bin/env python3
"""Rotate CHANGELOG.md's [Unreleased] block into a versioned heading.

Usage:
    rotate-unreleased.py <new_version> <date> <section_file> [<prev_tag>]

Arguments:
    new_version   semver, no leading 'v'                e.g. 0.2.0
    date          YYYY-MM-DD                            e.g. 2026-05-28
    section_file  path to rendered Keep a Changelog section for the new
                  version (without the '## [version] - date' heading)
    prev_tag      previous tag with 'v' prefix; empty for first release

Behavior:
    - If CHANGELOG.md is missing, write it from scratch with header,
      empty [Unreleased] (placeholder), the new section, and footer
      compare-links.
    - Otherwise rotate: replace the [Unreleased] body with a placeholder,
      insert the new '## [<v>] - <date>' block immediately below it, and
      rewrite the footer link references for [Unreleased] and [<v>].

Idempotent guard:
    If [<v>] already exists in CHANGELOG.md, exit non-zero with a clear
    message — the maintainer is re-running on an already-rotated tree.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

REPO = "xico42/codeherd"
HEADER = """\
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
"""
UNRELEASED_PLACEHOLDER = "<!-- Add Unreleased entries here. -->"


def die(msg: str, code: int = 2) -> None:
    print(msg, file=sys.stderr)
    sys.exit(code)


def footer_for(version: str, prev_tag: str) -> str:
    tag = f"v{version}"
    if prev_tag:
        version_link = (
            f"[{version}]: https://github.com/{REPO}/compare/{prev_tag}...{tag}"
        )
    else:
        version_link = (
            f"[{version}]: https://github.com/{REPO}/releases/tag/{tag}"
        )
    unreleased = f"[Unreleased]: https://github.com/{REPO}/compare/{tag}...HEAD"
    return unreleased, version_link


def write_fresh(path: Path, version: str, date: str, section: str, prev_tag: str) -> None:
    unreleased_link, version_link = footer_for(version, prev_tag)
    section = section.rstrip() + "\n"
    body = (
        f"{HEADER}\n"
        f"## [Unreleased]\n\n"
        f"{UNRELEASED_PLACEHOLDER}\n\n"
        f"## [{version}] - {date}\n\n"
        f"{section}\n"
        f"{unreleased_link}\n"
        f"{version_link}\n"
    )
    path.write_text(body, encoding="utf-8")


def rotate(path: Path, version: str, date: str, section: str, prev_tag: str) -> None:
    text = path.read_text(encoding="utf-8")

    if re.search(rf"^## \[{re.escape(version)}\] - ", text, re.MULTILINE):
        die(f"CHANGELOG.md already has [{version}] — refusing to rotate twice.")

    unreleased_re = re.compile(r"^## \[Unreleased\]\n", re.MULTILINE)
    m = unreleased_re.search(text)
    if not m:
        die("CHANGELOG.md has no [Unreleased] heading — refusing to guess where to insert.")

    # Find end of the Unreleased body: next "## [" heading, or footer link line.
    body_start = m.end()
    next_heading = re.search(r"^## \[", text[body_start:], re.MULTILINE)
    footer_start = re.search(r"^\[Unreleased\]:\s", text[body_start:], re.MULTILINE)

    if next_heading is None and footer_start is None:
        unreleased_body_end = len(text)
    else:
        candidates = [c.start() for c in (next_heading, footer_start) if c is not None]
        unreleased_body_end = body_start + min(candidates)

    head = text[: m.end()]
    tail = text[unreleased_body_end:]

    # Build the new section block.
    section = section.rstrip() + "\n"
    new_section_block = f"\n{UNRELEASED_PLACEHOLDER}\n\n## [{version}] - {date}\n\n{section}\n"

    # Rewrite footer link references.
    unreleased_link, version_link = footer_for(version, prev_tag)
    new_unreleased_re = re.compile(r"^\[Unreleased\]:\s.*$", re.MULTILINE)
    if new_unreleased_re.search(tail):
        tail = new_unreleased_re.sub(unreleased_link, tail, count=1)
        # Prepend the new version link immediately after the updated Unreleased link line.
        tail = re.sub(
            re.escape(unreleased_link) + r"\n",
            unreleased_link + "\n" + version_link + "\n",
            tail,
            count=1,
        )
    else:
        # No footer in tail — append one.
        tail = tail.rstrip() + "\n\n" + unreleased_link + "\n" + version_link + "\n"

    path.write_text(head + new_section_block + tail, encoding="utf-8")


def main(argv: list[str]) -> None:
    if len(argv) < 4 or len(argv) > 5:
        die(__doc__ or "bad args")

    version, date, section_path = argv[1], argv[2], argv[3]
    prev_tag = argv[4] if len(argv) == 5 else ""

    if not re.match(r"^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$", version):
        die(f"not a valid semver: {version}")
    if not re.match(r"^\d{4}-\d{2}-\d{2}$", date):
        die(f"date must be YYYY-MM-DD: {date}")
    if prev_tag and not re.match(r"^v\d+\.\d+\.\d+", prev_tag):
        die(f"prev_tag must look like vX.Y.Z: {prev_tag}")

    section = Path(section_path).read_text(encoding="utf-8")
    changelog = Path("CHANGELOG.md")

    if not changelog.exists():
        write_fresh(changelog, version, date, section, prev_tag)
        return

    rotate(changelog, version, date, section, prev_tag)


if __name__ == "__main__":
    main(sys.argv)
