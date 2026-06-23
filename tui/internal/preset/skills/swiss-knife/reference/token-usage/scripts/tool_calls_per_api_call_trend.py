#!/usr/bin/env python3
"""Compute LingTai tool calls per LLM API call over recent local-day bins.

The metric is intentionally based on LingTai runtime events rather than token
volume:

- API calls: successful LLM responses (`type == "llm_response"`), deduplicated
  by `(events log path, api_call_id)`.
- Tool calls: top-level model-requested tool calls (`type == "tool_call_received"`),
  deduplicated by `(events log path, api_call_id, tool_call_id)` and assigned to
  the local-day bin of the API response that produced them.
- Tool calls/API call: `tool_calls / api_calls`.
- Tool-using API-call rate: API calls with at least one tool call / API calls.

Pass one or more roots to scan. A root may be a project directory, a `.lingtai`
directory, an agent directory, or a single `events.jsonl` file. Daemon logs are
excluded by default to match parent-agent usage-report workflows.
"""
from __future__ import annotations

import argparse
import csv
import json
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import DefaultDict, Dict, Iterable, List, Optional, Set, Tuple

try:
    from zoneinfo import ZoneInfo
except ImportError:  # pragma: no cover - Python <3.9 fallback
    ZoneInfo = None  # type: ignore[assignment]

EXCLUDE_PARTS = {"daemons", "node_modules", ".git", "__pycache__"}


@dataclass(frozen=True)
class ApiKey:
    log_path: str
    api_call_id: str


@dataclass
class ApiInfo:
    ts: float
    day: str
    source: str
    model: Optional[str] = None
    input_tokens: int = 0
    output_tokens: int = 0
    thinking_tokens: int = 0
    cached_tokens: int = 0


def parse_tz(name: str):
    if name.upper() == "UTC":
        return timezone.utc
    if ZoneInfo is None:
        raise SystemExit("--timezone requires Python 3.9+ zoneinfo unless using UTC")
    try:
        return ZoneInfo(name)
    except Exception as exc:  # pragma: no cover - depends on host tzdata
        raise SystemExit(f"invalid --timezone {name!r}: {exc}") from exc


def norm_ts(value) -> Optional[float]:
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
        except Exception:
            return None
    return None


def iter_event_logs(roots: Iterable[Path], since_ts: float, include_daemons: bool) -> Iterable[Path]:
    seen: Set[Path] = set()
    excludes = EXCLUDE_PARTS if not include_daemons else {p for p in EXCLUDE_PARTS if p != "daemons"}
    for root in roots:
        root = root.expanduser()
        candidates: Iterable[Path]
        if root.is_file() and root.name == "events.jsonl":
            candidates = [root]
        elif root.is_dir():
            candidates = root.rglob("events.jsonl")
        else:
            continue
        for path in candidates:
            if path in seen:
                continue
            seen.add(path)
            if any(part in excludes for part in path.parts):
                continue
            try:
                if path.stat().st_mtime < since_ts - 86400:
                    continue
            except OSError:
                continue
            yield path


def source_label(events_path: Path) -> str:
    parts = events_path.parts
    if ".lingtai" in parts:
        idx = parts.index(".lingtai")
        agent = parts[idx + 1] if idx + 1 < len(parts) else "unknown-agent"
        project = parts[idx - 1] if idx - 1 >= 0 else "unknown-project"
        return f"{project}/{agent}"
    if len(parts) >= 3:
        return "/".join(parts[-4:-2]) or str(events_path.parent)
    return str(events_path.parent)


def new_bucket() -> dict:
    return {
        "api_calls": 0,
        "tool_calls": 0,
        "tool_using_api_calls": 0,
        "input_tokens": 0,
        "cached_tokens": 0,
        "output_tokens": 0,
        "thinking_tokens": 0,
    }


def add_api(bucket: dict, info: ApiInfo, tool_count: int) -> None:
    bucket["api_calls"] += 1
    bucket["tool_calls"] += tool_count
    bucket["tool_using_api_calls"] += 1 if tool_count else 0
    bucket["input_tokens"] += info.input_tokens
    bucket["cached_tokens"] += info.cached_tokens
    bucket["output_tokens"] += info.output_tokens
    bucket["thinking_tokens"] += info.thinking_tokens


def finalize_bucket(bucket: dict) -> dict:
    out = dict(bucket)
    api_calls = bucket["api_calls"]
    input_tokens = bucket["input_tokens"]
    out["tool_calls_per_api_call"] = bucket["tool_calls"] / api_calls if api_calls else None
    out["tool_using_api_call_rate"] = bucket["tool_using_api_calls"] / api_calls if api_calls else None
    out["total_tokens"] = bucket["input_tokens"] + bucket["output_tokens"] + bucket["thinking_tokens"]
    out["cache_rate"] = bucket["cached_tokens"] / input_tokens if input_tokens else None
    return out


def day_for(ts: float, tz) -> str:
    return datetime.fromtimestamp(ts, timezone.utc).astimezone(tz).date().isoformat()


def compute(roots: List[Path], days: int, timezone_name: str, models: List[str], include_daemons: bool) -> dict:
    tz = parse_tz(timezone_name)
    now = datetime.now(timezone.utc)
    now_local = now.astimezone(tz)
    start_date = now_local.date() - timedelta(days=days - 1)
    since_local = datetime.combine(start_date, datetime.min.time(), tzinfo=tz)
    since = since_local.astimezone(timezone.utc)
    since_ts = since.timestamp()
    now_ts = now.timestamp()

    model_filters = [m.lower() for m in models]
    api_models: Dict[ApiKey, str] = {}
    api_infos: Dict[ApiKey, ApiInfo] = {}
    tool_counts_by_api: Counter[ApiKey] = Counter()
    seen_tool_events: Set[Tuple[str, str, str]] = set()
    files_scanned = 0
    candidate_lines_seen = 0

    for events_path in iter_event_logs(roots, since_ts, include_daemons):
        files_scanned += 1
        path_s = str(events_path)
        label = source_label(events_path)
        try:
            with events_path.open(encoding="utf-8", errors="ignore") as handle:
                for line in handle:
                    if "api_call_id" not in line:
                        continue
                    if not ("llm_call" in line or "llm_response" in line or "tool_call_received" in line):
                        continue
                    candidate_lines_seen += 1
                    try:
                        rec = json.loads(line)
                    except Exception:
                        continue
                    typ = rec.get("type")
                    ts = norm_ts(rec.get("ts"))
                    api_id = rec.get("api_call_id")
                    if ts is None or not api_id:
                        continue
                    # Keep model/tool records just outside the window in case they bracket a response.
                    if ts < since_ts - 3600 or ts > now_ts + 3600:
                        continue
                    key = ApiKey(path_s, str(api_id))
                    if typ == "llm_call":
                        model = rec.get("model")
                        if model is not None:
                            api_models[key] = str(model)
                    elif typ == "llm_response":
                        if not (since_ts <= ts <= now_ts):
                            continue
                        prior = api_infos.get(key)
                        if prior is None or ts >= prior.ts:
                            api_infos[key] = ApiInfo(
                                ts=ts,
                                day=day_for(ts, tz),
                                source=label,
                                model=api_models.get(key),
                                input_tokens=int(rec.get("input_tokens") or 0),
                                output_tokens=int(rec.get("output_tokens") or 0),
                                thinking_tokens=int(rec.get("thinking_tokens") or 0),
                                cached_tokens=int(rec.get("cached_tokens") or 0),
                            )
                    elif typ == "tool_call_received":
                        tool_id = rec.get("tool_call_id") or rec.get("tool_trace_id") or ""
                        unique = (path_s, str(api_id), str(tool_id))
                        if unique in seen_tool_events:
                            continue
                        seen_tool_events.add(unique)
                        tool_counts_by_api[key] += 1
        except OSError:
            continue

    for key, model in api_models.items():
        if key in api_infos and not api_infos[key].model:
            api_infos[key].model = model

    by_day_all: DefaultDict[str, dict] = defaultdict(new_bucket)
    by_source_all: DefaultDict[str, dict] = defaultdict(new_bucket)
    by_day_by_model: Dict[str, DefaultDict[str, dict]] = {m: defaultdict(new_bucket) for m in model_filters}
    by_source_by_model: Dict[str, DefaultDict[str, dict]] = {m: defaultdict(new_bucket) for m in model_filters}

    for key, info in api_infos.items():
        tool_count = int(tool_counts_by_api.get(key, 0))
        add_api(by_day_all[info.day], info, tool_count)
        add_api(by_source_all[info.source], info, tool_count)
        model = (info.model or "").lower()
        for wanted in model_filters:
            if model == wanted:
                add_api(by_day_by_model[wanted][info.day], info, tool_count)
                add_api(by_source_by_model[wanted][info.source], info, tool_count)

    days_list = [(start_date + timedelta(days=i)).isoformat() for i in range(days)]
    result = {
        "generated_at_utc": now.isoformat(),
        "generated_at_local": now_local.isoformat(),
        "timezone": timezone_name,
        "since_utc": since.isoformat(),
        "since_local": since_local.isoformat(),
        "days": days,
        "roots": [str(r.expanduser()) for r in roots],
        "exclude_daemons": not include_daemons,
        "models": models,
        "files_scanned": files_scanned,
        "candidate_event_lines_seen": candidate_lines_seen,
        "definition": {
            "api_calls": "unique llm_response events by (events log path, api_call_id)",
            "tool_calls": "unique tool_call_received events by (events log path, api_call_id, tool_call_id), assigned to the producing API response day",
            "tool_calls_per_api_call": "tool_calls / api_calls",
            "tool_using_api_call_rate": "API calls with at least one tool_call_received / API calls",
        },
        "by_day_all_models": {day: finalize_bucket(by_day_all[day]) for day in days_list},
        "by_source_all_models": {k: finalize_bucket(v) for k, v in sorted(by_source_all.items(), key=lambda kv: kv[1]["tool_calls"], reverse=True)},
        "by_day_by_model": {
            wanted: {day: finalize_bucket(by_day_by_model[wanted][day]) for day in days_list}
            for wanted in model_filters
        },
        "by_source_by_model": {
            wanted: {k: finalize_bucket(v) for k, v in sorted(by_source_by_model[wanted].items(), key=lambda kv: kv[1]["tool_calls"], reverse=True)}
            for wanted in model_filters
        },
    }
    return result


def fmt_ratio(value) -> str:
    return "n/a" if value is None else f"{value:.3f}"


def fmt_pct(value) -> str:
    return "n/a" if value is None else f"{value * 100:.1f}%"


def write_outputs(result: dict, prefix: Path) -> None:
    prefix.parent.mkdir(parents=True, exist_ok=True)
    prefix.with_suffix(".json").write_text(json.dumps(result, indent=2, ensure_ascii=False), encoding="utf-8")
    with prefix.with_suffix(".csv").open("w", encoding="utf-8", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow([
            "scope",
            "name",
            "api_calls",
            "tool_calls",
            "tool_calls_per_api_call",
            "tool_using_api_call_rate",
            "input_tokens",
            "cached_tokens",
            "cache_rate",
            "output_tokens",
            "thinking_tokens",
            "total_tokens",
        ])
        tables = [("day_all_models", result["by_day_all_models"]), ("source_all_models", result["by_source_all_models"])]
        for model, rows in result["by_day_by_model"].items():
            tables.append((f"day_model:{model}", rows))
        for model, rows in result["by_source_by_model"].items():
            tables.append((f"source_model:{model}", rows))
        for scope, rows in tables:
            for name, bucket in rows.items():
                writer.writerow([
                    scope,
                    name,
                    bucket["api_calls"],
                    bucket["tool_calls"],
                    bucket["tool_calls_per_api_call"],
                    bucket["tool_using_api_call_rate"],
                    bucket["input_tokens"],
                    bucket["cached_tokens"],
                    bucket["cache_rate"],
                    bucket["output_tokens"],
                    bucket["thinking_tokens"],
                    bucket["total_tokens"],
                ])

    md: List[str] = [
        "# Tool calls per API call trend",
        "",
        f"- Generated: `{result['generated_at_local']}` local / `{result['generated_at_utc']}` UTC",
        f"- Window start: `{result['since_local']}` local / `{result['since_utc']}` UTC",
        f"- Roots: `{', '.join(result['roots'])}`",
        f"- Excludes daemon logs: `{result['exclude_daemons']}`",
        f"- Event logs scanned: `{result['files_scanned']}`",
        "",
        "Metric: `tool_calls_per_api_call = unique tool_call_received events / unique llm_response API calls`, binned by local calendar day. `tool_using_api_call_rate` is the fraction of API calls that produced at least one tool call.",
        "",
        "## Daily trend — all models",
        "",
        "| date | API calls | tool calls | tools/API call | tool-using API calls | total tokens | cache rate |",
        "|---|---:|---:|---:|---:|---:|---:|",
    ]
    for day, bucket in result["by_day_all_models"].items():
        md.append(
            f"| {day} | {bucket['api_calls']:,} | {bucket['tool_calls']:,} | "
            f"{fmt_ratio(bucket['tool_calls_per_api_call'])} | {fmt_pct(bucket['tool_using_api_call_rate'])} | "
            f"{bucket['total_tokens']:,} | {fmt_pct(bucket['cache_rate'])} |"
        )
    for model, rows in result["by_day_by_model"].items():
        md.extend([
            "",
            f"## Daily trend — model `{model}`",
            "",
            "| date | API calls | tool calls | tools/API call | tool-using API calls | total tokens | cache rate |",
            "|---|---:|---:|---:|---:|---:|---:|",
        ])
        for day, bucket in rows.items():
            md.append(
                f"| {day} | {bucket['api_calls']:,} | {bucket['tool_calls']:,} | "
                f"{fmt_ratio(bucket['tool_calls_per_api_call'])} | {fmt_pct(bucket['tool_using_api_call_rate'])} | "
                f"{bucket['total_tokens']:,} | {fmt_pct(bucket['cache_rate'])} |"
            )
    md.extend([
        "",
        "## Top sources by tool calls — all models",
        "",
        "| source | API calls | tool calls | tools/API call | tool-using API calls |",
        "|---|---:|---:|---:|---:|",
    ])
    for source, bucket in list(result["by_source_all_models"].items())[:20]:
        md.append(
            f"| {source} | {bucket['api_calls']:,} | {bucket['tool_calls']:,} | "
            f"{fmt_ratio(bucket['tool_calls_per_api_call'])} | {fmt_pct(bucket['tool_using_api_call_rate'])} |"
        )
    md.extend(["", "## Artifacts", f"- JSON: `{prefix.with_suffix('.json')}`", f"- CSV: `{prefix.with_suffix('.csv')}`"])
    prefix.with_suffix(".md").write_text("\n".join(md) + "\n", encoding="utf-8")


def default_prefix(days: int) -> Path:
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    return Path.cwd() / f"tool_calls_per_api_call_{days}d_{stamp}"


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(description="Compute LingTai tool calls per API call over local-day bins.")
    parser.add_argument("roots", nargs="+", help="Roots to scan: project dir, .lingtai dir, agent dir, or events.jsonl file.")
    parser.add_argument("--days", type=int, default=5, help="Number of local calendar days to include, including today (default: 5).")
    parser.add_argument("--timezone", default="UTC", help="IANA timezone for day bins (default: UTC; e.g. America/Los_Angeles).")
    parser.add_argument("--model", action="append", default=[], help="Also emit a model-specific table for an exact model name; repeatable.")
    parser.add_argument("--include-daemons", action="store_true", help="Include daemon logs instead of excluding them.")
    parser.add_argument("--out-prefix", default="", help="Output path prefix for .md/.json/.csv artifacts (default: ./tool_calls_per_api_call_<days>d_<timestamp>).")
    args = parser.parse_args(argv)
    if args.days < 1:
        parser.error("--days must be >= 1")
    roots = [Path(root) for root in args.roots]
    result = compute(roots, args.days, args.timezone, args.model, args.include_daemons)
    prefix = Path(args.out_prefix).expanduser() if args.out_prefix else default_prefix(args.days)
    write_outputs(result, prefix)
    print(json.dumps({
        "prefix": str(prefix),
        "artifacts": [str(prefix.with_suffix(ext)) for ext in (".md", ".json", ".csv")],
        "generated_at_local": result["generated_at_local"],
        "by_day_all_models": result["by_day_all_models"],
        "by_day_by_model": result["by_day_by_model"],
    }, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
