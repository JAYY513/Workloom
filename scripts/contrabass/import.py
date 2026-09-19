#!/usr/bin/env python3
"""board -> devsys import (M8.4, 方案 §10.3).

Reads board cards and flows execution outcomes back into the project
EXCLUSIVELY through the devsys CLI (get with --expect, transition on
legal edges only, comment for conclusions). This script never writes
project state files directly: it only spawns `devsys` and writes stdout.

Conservative reverse mapping (board -> devsys): only done / review /
blocked / closed(=cancelled) transition the work item; every other board
state only appends a comment. Illegal edges and stale versions land in
`refused`, never force-written.

Usage (Windows): py -3 scripts/contrabass/import.py --root <proj> --board <dir> [--dry-run] --actor <a> --reason <r> [--devsys ...]
"""

import argparse
import json
import subprocess
import sys
from pathlib import Path

# Board states that may move a devsys work item (target devsys status).
REVERSE = {
    "done": "done",
    "review": "review",
    "blocked": "blocked",
    "closed": "cancelled",
}


def run_devsys(devsys, root, *args):
    cmd = [devsys, "--json"] + list(args)
    try:
        proc = subprocess.run(cmd, cwd=root, capture_output=True, text=True, timeout=60)
    except FileNotFoundError:
        sys.stderr.write(f"devsys binary not found: {devsys}\n")
        sys.exit(1)
    return proc


def must_ok(proc, what):
    if proc.returncode != 0:
        return None, f"{what} failed: {(proc.stderr or proc.stdout).strip()}"
    try:
        return json.loads(proc.stdout), None
    except json.JSONDecodeError as exc:
        return None, f"{what}: output is not JSON: {exc}"


def main():
    ap = argparse.ArgumentParser(description="import board execution outcomes into devsys")
    ap.add_argument("--root", required=True)
    ap.add_argument("--board", required=True)
    ap.add_argument("--actor", required=True)
    ap.add_argument("--reason", required=True)
    ap.add_argument("--devsys", default="devsys")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    root = str(Path(args.root).resolve())
    board = Path(args.board) / "issues"
    if not board.is_dir():
        sys.stderr.write(f"board issues dir missing: {board}\n")
        return 1

    report = {"transitioned": [], "commented": [], "refused": [], "skipped": []}
    for path in sorted(board.glob("*.json")):
        try:
            card = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            report["refused"].append(f"{path.name}: unreadable card: {exc}")
            continue
        wid = card.get("key", path.stem)
        proc = run_devsys(args.devsys, root, "workitem", "get", wid)
        doc, err = must_ok(proc, f"workitem get {wid}")
        if err:
            report["refused"].append(f"{wid}: {err}")
            continue
        if not doc.get("ok") or not isinstance(doc.get("item"), dict) or "status" not in doc["item"]:
            report["refused"].append(f"{wid}: unexpected get envelope")
            continue
        cur, version = doc["item"]["status"], doc.get("version", "")
        target = REVERSE.get(card.get("board_status", ""))
        result = card.get("result") or {}
        summary = (result.get("summary") or "").strip()

        actions = []
        if target and target != cur:
            actions.append(("transition", target))
        if summary:
            actions.append(("comment", summary))
        if not actions:
            report["skipped"].append(f"{wid}: board={card.get('board_status')} cur={cur}, nothing to flow back")
            continue
        if args.dry_run:
            report["skipped"].append(f"{wid}: dry-run would {actions}")
            continue
        for kind, payload in actions:
            if kind == "transition":
                p = run_devsys(args.devsys, root, "workitem", "transition",
                               "--id", wid, "--to", payload,
                               "--actor", args.actor, "--reason", args.reason,
                               "--expect", version)
                if p.returncode != 0:
                    report["refused"].append(f"{wid}: transition to {payload}: {(p.stderr or p.stdout).strip()}")
                    break
                report["transitioned"].append(f"{wid}: {cur} -> {payload}")
                # Refresh the version for the comment that follows.
                g2 = run_devsys(args.devsys, root, "workitem", "get", wid)
                d2, e2 = must_ok(g2, f"workitem get {wid}")
                if not e2 and d2.get("version"):
                    version = d2["version"]
            else:
                p = run_devsys(args.devsys, root, "workitem", "comment",
                               "--id", wid, "--text", f"[board] {payload}",
                               "--actor", args.actor)
                if p.returncode != 0:
                    report["refused"].append(f"{wid}: comment: {(p.stderr or p.stdout).strip()}")
                else:
                    report["commented"].append(wid)
    print(json.dumps(report, indent=2, ensure_ascii=False))
    return 0 if not report["refused"] else 1


if __name__ == "__main__":
    sys.exit(main())
