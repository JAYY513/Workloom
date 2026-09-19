#!/usr/bin/env python3
"""devsys -> Contrabass-board export (M8.4, 方案 §10.3).

Reads work items through the devsys CLI (JSON envelopes, fail-closed) and
writes board cards under <board>/issues/<id>.json. The board is a delivery
copy, never a second source of truth: status flows devsys -> board here,
and board -> devsys only through import.py's CLI-only path.

Card shape is the documented intermediate form (see README.md); native
`.contrabass/board/` field names are [UNVERIFIED] assumptions until a live
Contrabass re-check (README §复验) lands them.

Usage (Windows): py -3 scripts/contrabass/export.py --root <proj> --out <board> [--devsys ...] [--id WLM-1,...]
Usage (POSIX):   ./scripts/contrabass/export.py --root <proj> --out <board> [...]
"""

import argparse
import json
import subprocess
import sys
from pathlib import Path

# devsys nine states -> board four states (+label for the queued retry).
BOARD_STATUS = {
    "draft": "open",
    "backlog": "open",
    "ready": "open",
    "retry_queued": "open",
    "in_progress": "in_progress",
    "blocked": "blocked",
    "review": "review",
    "verification": "review",
    "done": "done",
    "cancelled": "closed",
}


def run_devsys(devsys, root, *args):
    cmd = [devsys, "--json"] + list(args)
    try:
        proc = subprocess.run(cmd, cwd=root, capture_output=True, text=True, timeout=60)
    except FileNotFoundError:
        sys.stderr.write(f"devsys binary not found: {devsys}\n")
        sys.exit(1)
    if proc.returncode != 0:
        sys.stderr.write(f"devsys {' '.join(args)} failed: {proc.stderr.strip()}\n")
        sys.exit(1)
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        sys.stderr.write(f"devsys output is not JSON: {exc}\n")
        sys.exit(1)


def board_card(item, version):
    status = item.get("status", "")
    board = BOARD_STATUS.get(status)
    if board is None:
        sys.stderr.write(f"unknown devsys status {status!r} on {item.get('id')}\n")
        sys.exit(1)
    labels = []
    if status == "retry_queued":
        labels.append("retry-queued")
    return {
        "key": item["id"],
        "title": item.get("title", ""),
        "description": item.get("description", ""),
        "devsys_status": status,
        "board_status": board,
        "labels": labels,
        "priority": item.get("priority", 0),
        "acceptance_criteria": item.get("acceptance_criteria", []),
        "source": {"project": item.get("project_id", ""), "workitem_version": version},
        "devsys_updated_at": item.get("updated_at", ""),
        # Execution outcome, filled on the board side (simulation or live).
        "result": None,
    }


def main():
    ap = argparse.ArgumentParser(description="export devsys work items to a board directory")
    ap.add_argument("--root", required=True, help="project root containing .devsys/")
    ap.add_argument("--out", required=True, help="board directory to write")
    ap.add_argument("--id", default="", help="comma-separated work item ids (default: all)")
    ap.add_argument("--devsys", default="devsys", help="devsys binary")
    ap.add_argument("--init-fixture", action="store_true", help="create an empty board layout and exit")
    args = ap.parse_args()

    root = str(Path(args.root).resolve())
    out = Path(args.out)
    (out / "issues").mkdir(parents=True, exist_ok=True)
    if args.init_fixture:
        print(f"board fixture ready at {out}")
        return 0

    if args.id.strip():
        ids = [i.strip() for i in args.id.split(",") if i.strip()]
        items = []
        for wid in ids:
            doc = run_devsys(args.devsys, root, "workitem", "get", wid)
            if not doc.get("ok") or "item" not in doc:
                sys.stderr.write(f"workitem get {wid}: unexpected envelope\n")
                return 1
            items.append((doc["item"], doc.get("version", "")))
    else:
        doc = run_devsys(args.devsys, root, "workitem", "list")
        if not doc.get("ok") or "items" not in doc:
            sys.stderr.write("workitem list: unexpected envelope\n")
            return 1
        items = []
        for item in doc["items"]:
            g = run_devsys(args.devsys, root, "workitem", "get", item["id"])
            items.append((g["item"], g.get("version", "")))

    for item, version in items:
        card = board_card(item, version)
        path = out / "issues" / (item["id"] + ".json")
        text = json.dumps(card, indent=2, sort_keys=True, ensure_ascii=False) + "\n"
        if path.exists() and path.read_text(encoding="utf-8") == text:
            print(f"unchanged {path.name}")
        else:
            path.write_text(text, encoding="utf-8")
            print(f"exported {path.name} [{card['devsys_status']} -> {card['board_status']}]")
    return 0


if __name__ == "__main__":
    sys.exit(main())
