#!/usr/bin/env python3
"""Render the star trend as an ASCII sparkline (always) and a PNG (if matplotlib is installed).

Reads docs/stars/stars.csv, writes docs/stars/trend.png and prints a markdown-
embeddable summary to stdout.
"""
from __future__ import annotations

import csv
import datetime as dt
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
CSV_PATH = HERE / "stars.csv"
PNG_PATH = HERE / "trend.png"

SPARK = "▁▂▃▄▅▆▇█"


def load() -> list[tuple[dt.date, int]]:
    if not CSV_PATH.exists():
        raise SystemExit(f"missing {CSV_PATH} — run scripts/star_tracker.py first")
    rows: list[tuple[dt.date, int]] = []
    with CSV_PATH.open(newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append((dt.date.fromisoformat(row["date"]), int(row["stars"])))
    rows.sort()
    return rows


def sparkline(values: list[int]) -> str:
    if not values:
        return ""
    lo, hi = min(values), max(values)
    if lo == hi:
        return SPARK[0] * len(values)
    span = hi - lo
    return "".join(SPARK[min(len(SPARK) - 1, int((v - lo) / span * (len(SPARK) - 1)))] for v in values)


def render_png(rows: list[tuple[dt.date, int]]) -> bool:
    try:
        import matplotlib

        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
        import matplotlib.dates as mdates
    except ImportError:
        return False
    dates = [r[0] for r in rows]
    stars = [r[1] for r in rows]
    fig, ax = plt.subplots(figsize=(9, 3.2), dpi=140)
    ax.plot(dates, stars, color="#d4824a", linewidth=2.0)
    ax.fill_between(dates, stars, alpha=0.18, color="#d4824a")
    ax.set_title(f"LingTai — GitHub stars over time (last {len(rows)} days)", fontsize=11, loc="left")
    ax.set_ylabel("stars")
    ax.grid(True, alpha=0.25, linestyle="--")
    ax.xaxis.set_major_locator(mdates.AutoDateLocator())
    ax.xaxis.set_major_formatter(mdates.DateFormatter("%Y-%m-%d"))
    for spine in ("top", "right"):
        ax.spines[spine].set_visible(False)
    fig.autofmt_xdate()
    fig.tight_layout()
    fig.savefig(PNG_PATH)
    plt.close(fig)
    return True


def main() -> int:
    rows = load()
    if not rows:
        print("(no data)")
        return 1
    first_d, first_s = rows[0]
    last_d, last_s = rows[-1]
    spark_window = rows[-60:]
    spark = sparkline([s for _, s in spark_window])
    week_ago = [s for d, s in rows if d >= last_d - dt.timedelta(days=7)]
    delta_7d = last_s - week_ago[0] if len(week_ago) >= 2 else 0
    delta_total = last_s - first_s
    png_ok = render_png(rows)

    print(f"# LingTai star trend")
    print()
    print(f"- Latest: **{last_s}** stars ({last_d.isoformat()})")
    print(f"- Last 7 days: **{delta_7d:+d}**")
    print(f"- Since tracking began ({first_d.isoformat()}): **{delta_total:+d}**")
    print(f"- Sparkline (last {len(spark_window)} days): `{spark}`")
    if png_ok:
        print(f"- Chart: `trend.png`")
    else:
        print(f"- Chart: (matplotlib not installed — PNG skipped)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
