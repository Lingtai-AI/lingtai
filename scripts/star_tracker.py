#!/usr/bin/env python3
"""Record the current GitHub star count for this repo into a CSV time series.

Default mode: append (or update) today's row in docs/stars/stars.csv with the
current stargazers_count from the GitHub REST API.

Backfill mode (--backfill): walk the paginated stargazers API with the
`star+json` media type to reconstruct one row per day from the first star
through today. Useful to seed the series. Capped at GitHub's 40k-star window;
unauthenticated callers get 60 req/hr, so for big repos pass --token.

Usage:
    python scripts/star_tracker.py
    python scripts/star_tracker.py --repo Lingtai-AI/lingtai
    python scripts/star_tracker.py --backfill --token "$GH_TOKEN"
"""
from __future__ import annotations

import argparse
import csv
import datetime as dt
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path

DEFAULT_REPO = "Lingtai-AI/lingtai"
DEFAULT_CSV = Path(__file__).resolve().parent.parent / "docs" / "stars" / "stars.csv"
API_ROOT = "https://api.github.com"


def _request(url: str, token: str | None, accept: str = "application/vnd.github+json") -> tuple[dict | list, dict]:
    req = urllib.request.Request(url)
    req.add_header("Accept", accept)
    req.add_header("User-Agent", "lingtai-star-tracker")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(req, timeout=30) as resp:
        body = json.loads(resp.read().decode("utf-8"))
        headers = dict(resp.headers)
    return body, headers


def fetch_current_count(repo: str, token: str | None) -> int:
    data, _ = _request(f"{API_ROOT}/repos/{repo}", token)
    return int(data["stargazers_count"])


def fetch_stargazer_timeline(repo: str, token: str | None) -> list[dt.date]:
    """Return a list of dates (one per star event) ordered oldest→newest."""
    dates: list[dt.date] = []
    page = 1
    while True:
        url = f"{API_ROOT}/repos/{repo}/stargazers?per_page=100&page={page}"
        try:
            body, headers = _request(url, token, accept="application/vnd.github.star+json")
        except urllib.error.HTTPError as exc:
            if exc.code == 403:
                remaining = exc.headers.get("X-RateLimit-Remaining", "?")
                raise SystemExit(
                    f"GitHub rate limit hit on page {page} (remaining={remaining}). "
                    f"Pass --token or set GITHUB_TOKEN."
                ) from exc
            raise
        if not body:
            break
        for entry in body:
            ts = entry.get("starred_at")
            if ts:
                dates.append(dt.datetime.fromisoformat(ts.replace("Z", "+00:00")).date())
        if len(body) < 100:
            break
        page += 1
        if "link" in headers and 'rel="next"' not in headers["link"]:
            break
    dates.sort()
    return dates


def append_today(csv_path: Path, repo: str, count: int) -> tuple[str, bool]:
    """Write today's count. Returns (action, changed) where action ∈ {'created','updated','appended','unchanged'}."""
    today = dt.date.today().isoformat()
    csv_path.parent.mkdir(parents=True, exist_ok=True)
    rows: list[list[str]] = []
    if csv_path.exists():
        with csv_path.open(newline="") as f:
            reader = csv.reader(f)
            rows = list(reader)
    if not rows or rows[0] != ["date", "stars", "repo"]:
        rows = [["date", "stars", "repo"]] + (rows[1:] if rows and rows[0][:2] == ["date", "stars"] else rows)
    body = rows[1:]
    if body and body[-1][0] == today:
        if body[-1][1] == str(count):
            return ("unchanged", False)
        body[-1] = [today, str(count), repo]
        action = "updated"
    else:
        body.append([today, str(count), repo])
        action = "appended" if csv_path.exists() else "created"
    rows = [rows[0]] + body
    with csv_path.open("w", newline="") as f:
        writer = csv.writer(f)
        writer.writerows(rows)
    return (action, True)


def write_backfill(csv_path: Path, repo: str, dates: list[dt.date]) -> int:
    """Write one row per day from first star → today using cumulative count."""
    if not dates:
        raise SystemExit("No star events returned — nothing to backfill.")
    csv_path.parent.mkdir(parents=True, exist_ok=True)
    start = dates[0]
    today = dt.date.today()
    counts_by_day: dict[dt.date, int] = {}
    cumulative = 0
    di = 0
    day = start
    while day <= today:
        while di < len(dates) and dates[di] <= day:
            cumulative += 1
            di += 1
        counts_by_day[day] = cumulative
        day += dt.timedelta(days=1)
    with csv_path.open("w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["date", "stars", "repo"])
        for d in sorted(counts_by_day):
            writer.writerow([d.isoformat(), counts_by_day[d], repo])
    return len(counts_by_day)


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repo", default=DEFAULT_REPO, help=f"owner/name (default: {DEFAULT_REPO})")
    ap.add_argument("--csv", default=str(DEFAULT_CSV), help="output CSV path")
    ap.add_argument("--token", default=os.environ.get("GITHUB_TOKEN"), help="GitHub token (or set GITHUB_TOKEN)")
    ap.add_argument("--backfill", action="store_true", help="reconstruct full daily history from stargazers API")
    args = ap.parse_args(argv)

    csv_path = Path(args.csv)
    if args.backfill:
        print(f"Backfilling {args.repo} → {csv_path}", file=sys.stderr)
        dates = fetch_stargazer_timeline(args.repo, args.token)
        n = write_backfill(csv_path, args.repo, dates)
        print(f"Wrote {n} daily rows ({dates[0]} → today, {len(dates)} stars)", file=sys.stderr)
        return 0

    count = fetch_current_count(args.repo, args.token)
    action, changed = append_today(csv_path, args.repo, count)
    print(f"{args.repo}: {count} stars [{action}]")
    return 0 if changed or action == "unchanged" else 1


if __name__ == "__main__":
    raise SystemExit(main())
