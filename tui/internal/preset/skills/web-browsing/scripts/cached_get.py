#!/usr/bin/env python3
"""
cached_get.py — Shared caching utility for web-browsing skill.

Provides a simple file-based HTTP cache with TTL support.
Use this to avoid hammering APIs and to speed up repeated requests.

Usage:
    from cached_get import cached_get

    # Simple GET with 1-hour cache
    response = cached_get("https://api.example.com/data")

    # Custom TTL (seconds)
    response = cached_get("https://api.example.com/data", ttl=3600)

    # Force refresh
    response = cached_get("https://api.example.com/data", refresh=True)

    # With custom headers
    response = cached_get("https://api.example.com/data",
                          headers={"Accept": "application/json"})

    # POST request (not cached by default)
    response = cached_get("https://api.example.com/submit",
                          method="POST", json={"key": "value"})

Cache location: /tmp/web-browsing-cache/
"""

import hashlib
import json
import os
import time
from pathlib import Path

import requests

# --- Configuration ---
CACHE_DIR = Path(os.environ.get("WEB_BROWSING_CACHE_DIR", "/tmp/web-browsing-cache"))
DEFAULT_TTL = int(os.environ.get("WEB_BROWSING_CACHE_TTL", "3600"))  # 1 hour
USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)


def _cache_key(url: str, method: str = "GET") -> str:
    """Generate a filesystem-safe cache key from URL and method."""
    h = hashlib.sha256(f"{method.upper()}:{url}".encode()).hexdigest()[:16]
    return f"{method.lower()}_{h}.json"


def _is_expired(cache_path: Path, ttl: int) -> bool:
    """Check if a cached response has expired."""
    if not cache_path.exists():
        return True
    mtime = cache_path.stat().st_mtime
    return (time.time() - mtime) > ttl


def cached_get(
    url: str,
    *,
    method: str = "GET",
    headers: dict | None = None,
    params: dict | None = None,
    json: dict | None = None,
    data: dict | None = None,
    ttl: int = DEFAULT_TTL,
    refresh: bool = False,
    timeout: int = 30,
    allow_codes: tuple[int, ...] = (200,),
    **kwargs,
) -> requests.Response:
    """
    HTTP GET/POST with file-based caching.

    Only GET requests with successful status codes are cached.
    POST/PUT/DELETE requests are never cached.

    Args:
        url: Target URL.
        method: HTTP method (default GET).
        headers: Custom headers (User-Agent added if not present).
        params: Query parameters.
        json: JSON body (for POST/PUT).
        data: Form data body.
        ttl: Cache TTL in seconds (default 1 hour).
        refresh: Force cache bypass.
        timeout: Request timeout in seconds.
        allow_codes: Status codes considered cacheable.
        **kwargs: Additional arguments passed to requests.request().

    Returns:
        requests.Response object.

    Raises:
        requests.HTTPError: If the response status code is not in allow_codes
            and not cached.
    """
    # Only cache GET requests
    cacheable = method.upper() == "GET"

    if cacheable and not refresh:
        CACHE_DIR.mkdir(parents=True, exist_ok=True)
        cache_path = CACHE_DIR / _cache_key(url, method)

        if not _is_expired(cache_path, ttl):
            try:
                with open(cache_path) as f:
                    cached = json.load(f)
                # Reconstruct a Response-like object
                resp = requests.Response()
                resp.status_code = cached["status_code"]
                resp.headers.update(cached["headers"])
                resp._content = cached["body"].encode("utf-8")
                resp.encoding = "utf-8"
                resp.url = url
                resp.reason = "OK" if resp.status_code == 200 else "Cached"
                return resp
            except (json.JSONDecodeError, KeyError, OSError):
                pass  # Cache corrupt, re-fetch

    # Build headers
    req_headers = {"User-Agent": USER_AGENT}
    if headers:
        req_headers.update(headers)

    # Make the request
    response = requests.request(
        method=method.upper(),
        url=url,
        headers=req_headers,
        params=params,
        json=json,
        data=data,
        timeout=timeout,
        **kwargs,
    )

    # Cache successful GET responses
    if cacheable and response.status_code in allow_codes:
        try:
            CACHE_DIR.mkdir(parents=True, exist_ok=True)
            cache_path = CACHE_DIR / _cache_key(url, method)
            with open(cache_path, "w") as f:
                json.dump(
                    {
                        "status_code": response.status_code,
                        "headers": dict(response.headers),
                        "body": response.text,
                        "url": url,
                        "cached_at": time.time(),
                    },
                    f,
                    ensure_ascii=False,
                )
        except OSError:
            pass  # Cache write failure is non-fatal

    return response


def clear_cache(url: str | None = None) -> int:
    """
    Clear the cache. If url is given, clear only that entry.
    Returns the number of entries removed.
    """
    if not CACHE_DIR.exists():
        return 0

    if url:
        cache_path = CACHE_DIR / _cache_key(url)
        if cache_path.exists():
            cache_path.unlink()
            return 1
        return 0

    # Clear all
    count = 0
    for f in CACHE_DIR.glob("*.json"):
        f.unlink()
        count += 1
    return count


if __name__ == "__main__":
    import sys

    if len(sys.argv) < 2:
        print("Usage: cached_get.py <url> [--refresh] [--ttl SECONDS]")
        sys.exit(1)

    url = sys.argv[1]
    opts = sys.argv[2:]

    refresh = "--refresh" in opts
    ttl = DEFAULT_TTL
    if "--ttl" in opts:
        idx = opts.index("--ttl")
        ttl = int(opts[idx + 1])

    resp = cached_get(url, refresh=refresh, ttl=ttl)
    print(f"Status: {resp.status_code}")
    print(f"Cached: {'no' if refresh else 'check /tmp/web-browsing-cache/'}")
    print(f"Content length: {len(resp.text)} chars")
    print("---")
    print(resp.text[:2000])
