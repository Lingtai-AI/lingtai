# Real-Time Data

> Part of the [web-browsing](../SKILL.md) skill.
> Financial data, weather, system status, Stack Exchange, Wikipedia.

# realtime-data

> **Real-time data from free APIs — stocks, weather, Q&A, encyclopedia, and more.**
> Part of the `web-browsing-manual` v3.0 skill family.

---

## Decision Tree

```
Real-time data need ────────┐
                         │
    ┌─────────┬──────────────┬───────────┬────────┐
    │        │           │           │        │
  Finance  Weather   Tech Q&A    Facts/    News/
  /Stock   /Climate             Encyclo    Status
    │        │           │           │        │
    ▼        ▼           ▼           ▼        ▼
 yfinance  Open-Meteo  Stack     Wikipedia  Google
 (free)    (free)      Exchange  REST API   News RSS
                                  + others
```

---

## 1. Financial Data (yfinance)

**When to use:** Stock prices, company info, historical data, financial news.
**Free:** ✅ | **Key:** Not needed | **Speed:** ~1-2s

### Stock Information

```python
import yfinance as yf

def stock_info(ticker_symbol):
    """Get comprehensive stock information.
    
    Args:
        ticker_symbol: e.g. "AAPL", "GOOGL", "TSLA", "^GSPC"
    Returns: dict with name, price, market cap, etc.
    """
    ticker = yf.Ticker(ticker_symbol)
    info = ticker.info
    return {
        "name": info.get("longName"),
        "symbol": ticker_symbol,
        "price": info.get("currentPrice"),
        "previous_close": info.get("previousClose"),
        "change_pct": info.get("regularMarketChangePercent"),
        "market_cap": info.get("marketCap"),
        "pe_ratio": info.get("trailingPE"),
        "forward_pe": info.get("forwardPE"),
        "52week_high": info.get("fiftyTwoWeekHigh"),
        "52week_low": info.get("fiftyTwoWeekLow"),
        "volume": info.get("volume"),
        "avg_volume": info.get("averageVolume"),
        "dividend_yield": info.get("dividendYield"),
        "sector": info.get("sector"),
        "industry": info.get("industry"),
        "description": info.get("longBusinessSummary"),
        "website": info.get("website"),
        "employees": info.get("fullTimeEmployees"),
    }
```

### Historical Data

```python
def stock_history(ticker_symbol, period="1mo", interval="1d"):
    """Get OHLCV historical data.
    
    period: 1d, 5d, 1mo, 3mo, 6mo, 1y, 2y, 5y, ytd, max
    interval: 1m, 2m, 5m, 15m, 30m, 60m, 1h, 1d, 5d, 1wk, 1mo, 3mo
    Note: 1m data only available for last 7 days
    """
    ticker = yf.Ticker(ticker_symbol)
    hist = ticker.history(period=period, interval=interval)
    # Returns DataFrame with columns: Open, High, Low, Close, Volume
    return hist

def stock_history_csv(ticker_symbol, period="1y", filename=None):
    """Get history as CSV-friendly records."""
    hist = stock_history(ticker_symbol, period=period)
    return hist.reset_index().to_dict("records")
```

### Stock News & Recommendations

```python
def stock_news(ticker_symbol, count=10):
    """Get news related to a stock."""
    ticker = yf.Ticker(ticker_symbol)
    return ticker.news[:count]

def stock_recommendations(ticker_symbol):
    """Get analyst recommendations."""
    ticker = yf.Ticker(ticker_symbol)
    recs = ticker.recommendations
    return recs.to_dict("records") if recs is not None else []

def stock_earnings(ticker_symbol):
    """Get earnings data (quarterly)."""
    ticker = yf.Ticker(ticker_symbol)
    dates = ticker.earnings_dates
    return dates.to_dict("records") if dates is not None else []
```

### Multi-Stock Comparison

```python
def compare_stocks(symbols, period="3mo"):
    """Compare multiple stocks' performance over a period."""
    data = {}
    for sym in symbols:
        try:
            ticker = yf.Ticker(sym)
            hist = ticker.history(period=period)
            if len(hist) > 0:
                start_price = hist["Close"].iloc[0]
                end_price = hist["Close"].iloc[-1]
                data[sym] = {
                    "start": round(start_price, 2),
                    "end": round(end_price, 2),
                    "change_pct": round(((end_price / start_price) - 1) * 100, 2),
                    "high": round(hist["High"].max(), 2),
                    "low": round(hist["Low"].min(), 2),
                }
        except Exception:
            data[sym] = {"error": "Failed to fetch"}
    return data
```

**When NOT to use:** Need real-time tick data, options chains, or crypto (use `yf.Ticker("BTC-USD")` for crypto).
**Gotcha:** yfinance scrapes Yahoo Finance — may break if Yahoo changes their page structure.

---

## 2. Weather (Open-Meteo)

**When to use:** Weather forecasts, historical weather, climate data.
**Free:** ✅ | **Key:** Not needed | **Speed:** ~0.5s

### Current Weather + Forecast

```python
import requests

def get_weather(latitude, longitude, days=7):
    """Get weather forecast from Open-Meteo.
    
    Free, no API key, no registration.
    Returns current weather + daily forecast.
    """
    url = "https://api.open-meteo.com/v1/forecast"
    params = {
        "latitude": latitude,
        "longitude": longitude,
        "current_weather": True,
        "daily": "temperature_2m_max,temperature_2m_min,precipitation_sum,"
                 "windspeed_10m_max,weathercode",
        "timezone": "auto",
        "forecast_days": days,
    }
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    data = r.json()
    return {
        "current": data.get("current_weather"),
        "daily": data.get("daily"),
        "timezone": data.get("timezone"),
    }

def get_weather_by_city(city_name, days=7):
    """Convenience: city name → weather forecast."""
    coords = geocode(city_name)
    if not coords:
        return None
    return get_weather(coords["lat"], coords["lon"], days=days)
```

### Geocoding (City → Coordinates)

```python
def geocode(city_name):
    """Convert city name to lat/lon via Open-Meteo geocoding."""
    url = "https://geocoding-api.open-meteo.com/v1/search"
    params = {"name": city_name, "count": 1, "language": "en", "format": "json"}
    r = requests.get(url, params=params, timeout=10)
    results = r.json().get("results")
    if results:
        res = results[0]
        return {
            "lat": res["latitude"],
            "lon": res["longitude"],
            "name": res["name"],
            "country": res.get("country"),
            "admin1": res.get("admin1"),  # State/province
        }
    return None
```

### Historical Weather

```python
def historical_weather(latitude, longitude, start_date, end_date):
    """Get historical weather data.
    
    Dates in YYYY-MM-DD format. Max range: past 80 years.
    """
    url = "https://archive-api.open-meteo.com/v1/archive"
    params = {
        "latitude": latitude,
        "longitude": longitude,
        "start_date": start_date,
        "end_date": end_date,
        "daily": "temperature_2m_max,temperature_2m_min,precipitation_sum,"
                 "windspeed_10m_max",
        "timezone": "auto",
    }
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()
```

**When NOT to use:** Need severe weather alerts, radar data, or very specific hourly forecasts beyond what Open-Meteo provides.

---

## 3. Stack Exchange API

**When to use:** Technical Q&A, programming help, domain-specific knowledge.
**Free:** ✅ | **Key:** Not needed (300/day with key: 10000/day) | **Speed:** ~0.5s

```python
def stackexchange_search(query, site="stackoverflow", limit=10, sort="relevance"):
    """Search Stack Exchange sites.
    
    site: stackoverflow, serverfault, askubuntu, superuser, math, etc.
          Full list: https://stackexchange.com/sites
    sort: relevance, votes, newest, activity
    """
    url = "https://api.stackexchange.com/2.3/search"
    params = {
        "intitle": query,
        "site": site,
        "pagesize": limit,
        "sort": sort,
        "order": "desc",
    }
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    items = r.json().get("items", [])
    return [{
        "title": i.get("title"),
        "url": i.get("link"),
        "score": i.get("score"),
        "answers": i.get("answer_count"),
        "is_answered": i.get("is_answered"),
        "tags": i.get("tags", []),
        "views": i.get("view_count"),
        "created": i.get("creation_date"),
    } for i in items]

def stackexchange_answers(question_id, site="stackoverflow"):
    """Get answers for a specific question (with body content)."""
    url = f"https://api.stackexchange.com/2.3/questions/{question_id}/answers"
    params = {
        "site": site,
        "order": "desc",
        "sort": "votes",
        "filter": "withbody",  # Include full HTML body
    }
    r = requests.get(url, params=params, timeout=15)
    items = r.json().get("items", [])
    return [{
        "body_html": i.get("body"),
        "score": i.get("score"),
        "is_accepted": i.get("is_accepted"),
        "author": i.get("owner", {}).get("display_name"),
    } for i in items]

def stackexchange_advanced_search(query, site="stackoverflow", tagged=None,
                                   accepted=True, limit=10):
    """Advanced search with filters.
    
    tagged: comma-separated tags, e.g. "python,pandas"
    accepted: only questions with accepted answers
    """
    url = "https://api.stackexchange.com/2.3/search/advanced"
    params = {
        "q": query,
        "site": site,
        "pagesize": limit,
        "order": "desc",
        "sort": "relevance",
        "answers": 1 if accepted else 0,
    }
    if tagged:
        params["tagged"] = tagged
    r = requests.get(url, params=params, timeout=15)
    return r.json().get("items", [])
```

**When NOT to use:** Need content from non-SE sites, or need real-time streaming answers.

---

## 4. Wikipedia REST API

**When to use:** Encyclopedia facts, definitions, summaries, images.
**Free:** ✅ | **Key:** Not needed | **Speed:** ~0.3s

```python
def wikipedia_summary(title, lang="en"):
    """Get a summary of a Wikipedia article.
    
    Returns: title, extract, thumbnail, coordinates (if applicable).
    """
    url = f"https://{lang}.wikipedia.org/api/rest_v1/page/summary/{title}"
    r = requests.get(url, timeout=15,
                     headers={"User-Agent": "LingTai/3.0"})
    if r.status_code == 404:
        return None
    r.raise_for_status()
    return r.json()

def wikipedia_full_text(title, lang="en"):
    """Get full plain text of a Wikipedia article."""
    url = f"https://{lang}.wikipedia.org/api/rest_v1/page/plain/{title}"
    r = requests.get(url, timeout=15,
                     headers={"User-Agent": "LingTai/3.0"})
    if r.status_code == 404:
        return None
    r.raise_for_status()
    return r.json()

def wikipedia_search(query, lang="en", limit=10):
    """Search Wikipedia for articles matching a query."""
    url = f"https://{lang}.wikipedia.org/w/api.php"
    params = {
        "action": "query",
        "list": "search",
        "srsearch": query,
        "format": "json",
        "srlimit": limit,
    }
    r = requests.get(url, params=params, timeout=15)
    return r.json()["query"]["search"]

def wikipedia_related(title, lang="en", limit=10):
    """Get related articles using morelike: search.
    
    NOTE: The REST API /page/related endpoint was decommissioned by Wikimedia
    in Feb 2025 (Phabricator T376297). This uses MediaWiki Action API's
    morelike: search as the recommended replacement.
    """
    url = f"https://{lang}.wikipedia.org/w/api.php"
    params = {
        "action": "query",
        "list": "search",
        "srsearch": f"morelike:{title}",
        "srnamespace": "0",
        "srlimit": limit,
        "format": "json",
    }
    r = requests.get(url, params=params, timeout=15,
                     headers={"User-Agent": "LingTai/3.0"})
    r.raise_for_status()
    results = r.json().get("query", {}).get("search", [])
    return [
        {
            "title": item.get("title"),
            "pageid": item.get("pageid"),
            "snippet": item.get("snippet", "").replace(
                '<span class="searchmatch">', "").replace("</span>", ""),
        }
        for item in results
    ]
```

**Multi-language support:** Change `lang` parameter: "en", "zh", "ja", "de", "fr", "es", "ru", etc.

---

## 5. System Status Pages

**When to use:** Check if a service is down, get incident reports.
**Speed:** ~1-5s

```python
def check_statuspage(domain):
    """Check Statuspage.io-powered status pages.
    
    Many SaaS companies use Statuspage.io with a standard JSON API.
    E.g., github.statuspage.io, api.openai.com, etc.
    """
    # Try common patterns
    candidates = [
        f"https://{domain}.statuspage.io/api/v2/status.json",
        f"https://status.{domain}/api/v2/status.json",
    ]
    for url in candidates:
        try:
            r = requests.get(url, timeout=10)
            if r.status_code == 200:
                data = r.json()
                return {
                    "page": data.get("page", {}).get("name"),
                    "status": data.get("status", {}).get("indicator"),
                    "description": data.get("status", {}).get("description"),
                }
        except Exception:
            continue
    return {"status": "unknown", "error": "Could not find status page"}

def check_statuspage_incidents(domain):
    """Get active incidents from a Statuspage.io page."""
    candidates = [
        f"https://{domain}.statuspage.io/api/v2/incidents.json",
        f"https://status.{domain}/api/v2/incidents.json",
    ]
    for url in candidates:
        try:
            r = requests.get(url, timeout=10)
            if r.status_code == 200:
                return r.json().get("incidents", [])
        except Exception:
            continue
    return []
```

---

## 6. News & Social Feeds (Quick Reference)

For detailed usage, see the `news-and-rss` and `social-media-extraction` sub-skills.

```python
# Google News RSS (free, no key)
def quick_news(query):
    import feedparser
    url = f"https://news.google.com/rss/search?q={query}&hl=en"
    r = requests.get(url, timeout=15, headers={"User-Agent": "Mozilla/5.0"})
    feed = feedparser.parse(r.text)
    return [{"title": e.title, "url": e.link, "date": e.get("published")}
            for e in feed.entries[:10]]

# Hacker News top stories
def hn_top(limit=10):
    r = requests.get("https://hacker-news.firebaseio.com/v0/topstories.json", timeout=10)
    ids = r.json()[:limit]
    stories = []
    for id_ in ids:
        item = requests.get(f"https://hacker-news.firebaseio.com/v0/item/{id_}.json",
                           timeout=10).json()
        if item:
            stories.append({"title": item.get("title"), "url": item.get("url"),
                           "score": item.get("score")})
    return stories

# GitHub trending (via search API)
def github_trending(language=None, since="daily"):
    url = "https://api.github.com/search/repositories"
    query = "stars:>100"
    if language:
        query += f" language:{language}"
    query += f" pushed:>{_since_date(since)}"
    params = {"q": query, "sort": "stars", "order": "desc", "per_page": 10}
    r = requests.get(url, params=params, timeout=15)
    return r.json().get("items", [])

def _since_date(since):
    from datetime import datetime, timedelta
    days = {"daily": 1, "weekly": 7, "monthly": 30}
    return (datetime.now() - timedelta(days=days.get(since, 1))).strftime("%Y-%m-%d")
```

---

## Caching Strategy

Real-time data changes frequently but doesn't need to be fetched every second.

```python
import time, functools

# Simple TTL cache
_cache = {}
_cache_ttl = {}

def cached_fetch(key, fetch_fn, ttl_seconds=300):
    """Fetch with TTL-based cache.
    
    ttl_seconds: 300 = 5min (good for stock prices)
                 3600 = 1hr (good for weather)
                 86400 = 1day (good for Wikipedia)
    """
    now = time.time()
    if key in _cache and (now - _cache_ttl.get(key, 0)) < ttl_seconds:
        return _cache[key]
    
    result = fetch_fn()
    _cache[key] = result
    _cache_ttl[key] = now
    return result

# Usage
def get_stock_price_cached(symbol):
    return cached_fetch(f"stock:{symbol}", lambda: stock_info(symbol), ttl_seconds=300)

def get_weather_cached(city):
    return cached_fetch(f"weather:{city}", lambda: get_weather_by_city(city), ttl_seconds=3600)
```

---

## Failure Modes & Fallback Table

| Failure | Cause | Fallback |
|---------|-------|----------|
| yfinance returns empty | Yahoo Finance changed structure | Retry once, check ticker symbol |
| yfinance KeyError on info | Not all fields available for all stocks | Use `.get()` with defaults |
| Open-Meteo timeout | Network issues | Retry once, use cached data |
| Stack Exchange 429 | Rate limit exceeded (300/day without key) | Register for free key (10K/day) |
| Wikipedia 404 | Article doesn't exist | Try `wikipedia_search` to find correct title |
| Status page not found | Not using Statuspage.io | Try scraping the status page HTML |
| All data sources fail | Network outage | Return last cached result if available |

---

## Dependencies

```bash
pip install yfinance       # Financial data
pip install requests       # All HTTP APIs
pip install feedparser     # RSS/Atom feeds
pip install beautifulsoup4 # HTML parsing (status pages)
```

---

*This sub-skill is part of `web-browsing-manual` v3.0. For news-specific workflows, see `news-and-rss`. For social media, see `social-media-extraction`.*
