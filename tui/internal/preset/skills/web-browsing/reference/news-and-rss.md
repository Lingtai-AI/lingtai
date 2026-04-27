# News and RSS Feeds

> Part of the [web-browsing](../SKILL.md) skill.
> Google News RSS, Reddit JSON news feeds, RSS feed discovery and parsing, paywall boundary handling, news archival via Wayback Machine.

# News & RSS — 新闻获取与 RSS 订阅处理

本技能是 `web-browsing-manual` 的子技能，专注于新闻类内容的获取、RSS 订阅源的发现与解析、付费墙边界的处理、以及新闻归档。当你需要获取新闻、监控信息流、或解析 RSS/Atom feed 时，使用本技能。

---

## 1. 新闻获取决策树

面对"我需要获取新闻"这个需求时，按下图选择最佳路径：

```
新闻需求
  │
  ├─ 特定主题新闻? ──────────────→ Google News RSS（§2）
  │   （如 "AI breakthroughs"）
  │
  ├─ 社区讨论与热点? ────────────→ Reddit JSON API（§3）
  │   （如 r/worldnews, r/technology）
                          → Hacker News（见 social-media-extraction）
  │
  ├─ 特定网站新闻? ──────────────→ RSS Feed 发现 + feedparser（§4）
  │   （如 CNN, BBC, NYT）
  │
  ├─ 已有 URL 但遇到付费墙? ─→ 付费墙绕行边界（§5）
  │   尝试链: trafilatura → Jina Reader → Wayback Machine
  │
  └─ 需要归档或查历史? ────────→ Wayback Machine API（§6）
      （如页面可能被删除、需要保存证据）
```

**快速选择指南：**

| 需求 | 推荐方法 | 耗时 | 精度 |
|------|----------|------|------|
| 搜某个话题的最新新闻 | Google News RSS | <2s | 高 |
| 了解社区在讨论什么 | Reddit JSON API | <3s | 中 |
| 持续监控某个网站 | RSS Feed 发现 + 解析 | 首次<5s | 高 |
| 读到一半被付费墙挡住 | 付费墙绕行链 | 3-10s | 不确定 |
| 确保新闻链接不过期 | Wayback Machine 归档 | 5-60s | 高 |

---

## 2. Google News RSS

Google News 提供免费的 RSS 接口，无需 API Key，无速率限制，适合按主题搜索最新新闻。

### 2.1 完整代码

```python
import requests
import feedparser
from bs4 import BeautifulSoup


def google_news_rss(query, hl="en", gl="US", ceid="US:en"):
    """Search Google News via RSS feed.

    Args:
        query: Search query string (e.g. "artificial intelligence").
        hl:    Language hint (e.g. "en", "zh-CN", "ja").
        gl:    Country/region code (e.g. "US", "CN", "JP").
        ceid:  Country:language edition (e.g. "US:en", "CN:zh-Hans").

    Returns:
        List of dicts with keys: title, url, published, source.
    """
    url = "https://news.google.com/rss/search"
    params = {
        "q": query,
        "hl": hl,
        "gl": gl,
        "ceid": ceid,
    }
    headers = {"User-Agent": "Mozilla/5.0 (compatible; LingTai/1.0)"}

    r = requests.get(url, params=params, timeout=15, headers=headers)
    r.raise_for_status()

    feed = feedparser.parse(r.text)
    articles = []

    for entry in feed.entries[:20]:
        # Google News titles often contain HTML <source> tags
        title = BeautifulSoup(entry.title, "lxml").get_text()

        source_name = None
        source_tag = entry.get("source")
        if isinstance(source_tag, dict):
            source_name = source_tag.get("title")
        elif isinstance(source_tag, str):
            source_name = source_tag

        articles.append({
            "title": title,
            "url": entry.link,
            "published": entry.get("published"),
            "source": source_name,
        })

    return articles
```

### 2.2 参数详解

| 参数 | 说明 | 示例 |
|------|------|------|
| `q` | 搜索关键词，支持引号精确匹配 | `"OpenAI GPT-5"` |
| `hl` | 界面语言 | `"en"`, `"zh-CN"`, `"ja"`, `"de"` |
| `gl` | 国家/地区 | `"US"`, `"CN"`, `"JP"`, `"DE"` |
| `ceid` | 版本标识，格式 `{国家}:{语言}` | `"US:en"`, `"CN:zh-Hans"` |

**常用语言/地区组合：**

```
英语美国:   hl=en  gl=US  ceid=US:en
中文中国:   hl=zh-CN gl=CN ceid=CN:zh-Hans
日语日本:   hl=ja  gl=JP  ceid=JP:ja
德语德国:   hl=de  gl=DE  ceid=DE:de
法语法国:   hl=fr  gl=FR  ceid=FR:fr
```

### 2.3 进阶用法

**按时间范围搜索：** 在查询中加入 `when:` 修饰符：

```python
# 过去一天的新闻
articles = google_news_rss("AI when:1d")

# 过去一周的新闻
articles = google_news_rss("climate change when:7d")
```

**排除特定来源：** 使用 `-source:` 修饰符：

```python
articles = google_news_rss("technology -source:foxnews.com")
```

**获取特定新闻主题的 RSS：** 不用搜索，直接订阅主题 RSS：

```python
def google_news_topic(topic="WORLD", hl="en", gl="US", ceid="US:en"):
    """Get Google News topic feed.

    Valid topics: WORLD, NATION, BUSINESS, TECHNOLOGY,
                  ENTERTAINMENT, SPORTS, SCIENCE, HEALTH
    """
    url = f"https://news.google.com/rss/topics/CAAqJggKIiBDQkFTRWdvSUwyMHZNRGx1YlY4U0FtVnVHZ0pWVXlnQVAB"
    # Note: topic tokens change; use search-based approach for reliability.
    # For topic feeds, it's easier to use the web interface and copy the RSS URL.
    ...
```

> **提示：** topic token 不稳定，推荐始终使用 `search` 方式。

### 2.4 注意事项

- **无需 API Key**：Google News RSS 是免费公开接口。
- **无速率限制**：但仍应遵守合理使用，避免过于频繁请求。
- **返回 RSS XML**：用 `feedparser` 解析为结构化数据。
- **标题含 HTML**：Google News 的标题字段中可能包含 `<source>` 等 HTML 标签，需要用 BeautifulSoup 清理。
- **URL 重定向**：返回的链接是 Google 的重定向 URL，最终会跳转到原始文章。

---

## 3. Reddit JSON API 新闻流

Reddit 提供免费的 JSON API，只需在 URL 后追加 `.json` 即可获取结构化数据。适合获取社区驱动的新闻与讨论。

### 3.1 完整代码

```python
import requests
import time


# Simple rate limiter — track last request time
_last_request_time = 0
_MIN_INTERVAL = 2.0  # seconds between requests


def _rate_limited_get(url, **kwargs):
    """Enforce minimum interval between Reddit API requests."""
    global _last_request_time
    elapsed = time.time() - _last_request_time
    if elapsed < _MIN_INTERVAL:
        time.sleep(_MIN_INTERVAL - elapsed)
    _last_request_time = time.time()
    return requests.get(url, **kwargs)


def reddit_news(subreddit="worldnews", sort="hot", limit=25, timeframe="day"):
    """Get news posts from a Reddit subreddit.

    Args:
        subreddit: Subreddit name (without r/ prefix).
        sort:      Sort order — "hot", "new", "top", "rising".
        limit:     Number of posts to return (max 100).
        timeframe: Time range for "top" sort — "hour", "day", "week",
                   "month", "year", "all".

    Returns:
        List of dicts with post metadata.
    """
    url = f"https://www.reddit.com/r/{subreddit}/{sort}.json"
    headers = {"User-Agent": "LingTai/1.0 (News Aggregator)"}
    params = {"limit": limit}
    if sort == "top":
        params["t"] = timeframe

    r = _rate_limited_get(url, headers=headers, params=params, timeout=15)
    r.raise_for_status()

    data = r.json()
    posts = []

    for child in data["data"]["children"]:
        post = child["data"]
        posts.append({
            "title": post["title"],
            "url": post["url"],
            "score": post["score"],
            "comments": post["num_comments"],
            "author": post.get("author"),
            "created_utc": post.get("created_utc"),
            "selftext": post.get("selftext", "")[:500],
            "is_self": post.get("is_self", False),
            "subreddit": post.get("subreddit"),
            "link_flair_text": post.get("link_flair_text"),
        })

    return posts


def reddit_search(query, sort="new", limit=25, subreddit=None):
    """Search Reddit posts.

    Args:
        query:    Search query string.
        sort:     Sort order — "relevance", "new", "top", "comments".
        limit:    Number of results (max 100).
        subreddit: Restrict to a specific subreddit (optional).
    """
    if subreddit:
        url = f"https://www.reddit.com/r/{subreddit}/search.json"
    else:
        url = "https://www.reddit.com/search.json"
    headers = {"User-Agent": "LingTai/1.0 (News Aggregator)"}
    params = {"q": query, "sort": sort, "limit": limit, "restrict_sr": "on" if subreddit else "off"}

    r = _rate_limited_get(url, headers=headers, params=params, timeout=15)
    r.raise_for_status()

    data = r.json()
    return [
        {
            "title": child["data"]["title"],
            "url": child["data"]["url"],
            "score": child["data"]["score"],
            "subreddit": child["data"].get("subreddit"),
            "selftext": child["data"].get("selftext", "")[:500],
        }
        for child in data["data"]["children"]
    ]
```

### 3.2 Reddit API 速率限制

| 条件 | 限制 |
|------|------|
| 无 OAuth 认证 | 约 60 请求/分钟 |
| 必须设置 User-Agent | 否则可能被拒绝或限速更严 |
| 建议请求间隔 | ≥ 2 秒 |
| 每次请求最大结果数 | 100 条 |

**关键规则：**

1. **必须设置 User-Agent**：不设或使用默认 User-Agent 会被严厉限速甚至封禁。
2. **间隔请求**：在循环中抓取时，每次请求间隔至少 2 秒。
3. **不要模拟登录**：使用 OAuth 认证提升限额是可以的，但不要破解或绕过认证。
4. **分页**：使用 `after` 参数翻页：
   ```python
   # First page
   params = {"limit": 25}
   # Next page — use the "after" value from previous response
   params = {"limit": 25, "after": data["data"]["after"]}
   ```

### 3.3 常用新闻子板块

| 子板块 | 内容 | 适用场景 |
|--------|------|----------|
| r/worldnews | 国际新闻 | 全球时事 |
| r/news | 美国及国际新闻 | 综合新闻 |
| r/technology | 科技新闻 | 科技动态 |
| r/science | 科学新闻 | 科研进展 |
| r/politics | 美国政治 | 政治分析 |
| r/business | 商业财经 | 经济动态 |

---

## 4. RSS Feed 发现与解析

当你想持续监控某个新闻网站时，RSS 是最稳定的方式。本节提供自动发现和解析 RSS 的完整工具。

### 4.1 Feed 发现

```python
import re
import requests
from bs4 import BeautifulSoup
from urllib.parse import urlparse, urljoin


def discover_rss(url):
    """Find RSS/Atom feed URLs from a webpage.

    Strategy:
      1. Check <link rel="alternate" type="application/rss+xml"> tags.
      2. Check <link rel="alternate" type="application/atom+xml"> tags.
      3. Probe common RSS paths.

    Args:
        url: Webpage URL to scan for feed links.

    Returns:
        List of dicts: {"url": ..., "type": ..., "label": ...}
    """
    headers = {"User-Agent": "Mozilla/5.0 (compatible; LingTai/1.0)"}
    r = requests.get(url, timeout=15, headers=headers)
    soup = BeautifulSoup(r.text, "lxml")

    feeds = []

    # Strategy 1 & 2: <link> tags with RSS/Atom types
    for link in soup.find_all("link", rel=True):
        rel = link.get("rel", [])
        type_ = link.get("type", "")
        if "alternate" in rel and ("rss" in type_ or "atom" in type_):
            href = link.get("href", "")
            if href:
                # Resolve relative URLs
                href = urljoin(url, href)
                feeds.append({
                    "url": href,
                    "type": type_,
                    "label": link.get("title", ""),
                })

    # Strategy 3: Probe common RSS paths
    parsed = urlparse(url)
    base = f"{parsed.scheme}://{parsed.netloc}"
    common_paths = [
        "/feed", "/rss", "/feed.xml", "/rss.xml", "/index.xml",
        "/feeds/all.xml", "/atom.xml", "/feed/", "/rss/",
        "/news/rss", "/news/feed", "/api/rss",
    ]
    for path in common_paths:
        probe_url = base + path
        try:
            probe_r = requests.head(probe_url, timeout=5, headers=headers,
                                     allow_redirects=True)
            content_type = probe_r.headers.get("Content-Type", "")
            if probe_r.status_code == 200 and ("xml" in content_type or "rss" in content_type):
                feeds.append({"url": probe_url, "type": "probed", "label": path})
        except requests.RequestException:
            continue

    # Deduplicate
    seen = set()
    unique = []
    for f in feeds:
        if f["url"] not in seen:
            seen.add(f["url"])
            unique.append(f)

    return unique
```

### 4.2 Feed 解析

```python
import feedparser


def parse_rss(feed_url, max_entries=20):
    """Parse RSS/Atom feed and return structured entries.

    Args:
        feed_url:    URL of the RSS/Atom feed.
        max_entries: Maximum number of entries to return.

    Returns:
        Dict with "title" (feed title), "entries" (list of article dicts).
    """
    feed = feedparser.parse(feed_url)

    if not feed.entries:
        # feedparser might have failed — check bozo
        if feed.bozo and feed.bozo_exception:
            return {"title": None, "entries": [], "error": str(feed.bozo_exception)}
        return {"title": feed.feed.get("title"), "entries": []}

    entries = []
    for entry in feed.entries[:max_entries]:
        # Extract best available content
        content = None
        if hasattr(entry, "content") and entry.content:
            content = entry.content[0].get("value")
        elif hasattr(entry, "summary"):
            content = entry.summary
        elif hasattr(entry, "description"):
            content = entry.description

        # Clean HTML from content
        if content:
            content = BeautifulSoup(content, "lxml").get_text()

        entries.append({
            "title": entry.get("title"),
            "url": entry.get("link"),
            "published": entry.get("published"),
            "updated": entry.get("updated"),
            "summary": content[:1000] if content else None,
            "author": entry.get("author"),
            "tags": [t.get("term") for t in entry.get("tags", [])] if entry.get("tags") else [],
        })

    return {
        "title": feed.feed.get("title"),
        "link": feed.feed.get("link"),
        "entries": entries,
    }
```

### 4.3 使用示例

```python
# Example 1: Discover and parse CNN RSS
feeds = discover_rss("https://www.cnn.com")
if feeds:
    articles = parse_rss(feeds[0]["url"])
    for article in articles["entries"][:5]:
        print(f"[{article['published']}] {article['title']}")
        print(f"  {article['url']}")

# Example 2: Direct known RSS feed
articles = parse_rss("https://feeds.bbci.co.uk/news/world/rss.xml")
print(f"Feed: {articles['title']}")
for a in articles["entries"][:10]:
    print(f"  - {a['title']} ({a['published']})")

# Example 3: Batch monitor multiple feeds
FEEDS = [
    "https://feeds.bbci.co.uk/news/world/rss.xml",
    "https://rss.nytimes.com/services/xml/rss/nyt/World.xml",
    "https://feeds.reuters.com/reuters/topNews",
]
for feed_url in FEEDS:
    result = parse_rss(feed_url, max_entries=5)
    print(f"\n=== {result.get('title', feed_url)} ===")
    for entry in result["entries"]:
        print(f"  {entry['title']}")
```

### 4.4 feedparser 诊断

`feedparser` 有内置的诊断机制：

```python
feed = feedparser.parse("https://example.com/feed.xml")

# Check if feed was parsed successfully
if feed.bozo:
    print(f"Parse warning: {feed.bozo_exception}")
    # Common: XML encoding issues, malformed dates
    # feedparser still tries to extract data despite bozo errors

# Check feed metadata
print(f"Feed title: {feed.feed.get('title')}")
print(f"Feed link: {feed.feed.get('link')}")
print(f"Entries found: {len(feed.entries)}")

# Check entry fields available
if feed.entries:
    print(f"Available fields: {[k for k in feed.entries[0].keys()]}")
```

---

## 5. 付费墙绕行边界

### 5.1 伦理边界

**明确什么能做，什么不能做：**

| 能做 ✅ | 不能做 ❌ |
|---------|----------|
| 使用 RSS 获取公开摘要 | 破解登录验证系统 |
| 使用 Google 缓存 | 绕过服务器端付费墙 |
| 使用 Wayback Machine 查看归档 | 盗用付费订阅内容 |
| trafilatura 提取（对客户端付费墙有效） | 模拟已付费用户身份 |
| Jina Reader 转换页面 | 自动化破解 CAPTCHA |
| 提取 meta 标签和 OG 数据 | 共享付费内容的完整副本 |

**判断原则：如果内容需要用户登录才能看到，应该停止尝试。** 客户端付费墙（内容在 HTML 中但被 JS 隐藏）与服务器端付费墙（内容根本不在 HTML 中）有本质区别。前者是技术策略，后者是明确的付费意图。

### 5.2 绕行策略链

```python
import requests
import trafilatura


def try_behind_paywall(url):
    """Try to extract content from a potentially paywalled page.

    Tries strategies in order of respectfulness:
      1. trafilatura — handles client-side paywalls
      2. Jina Reader — lightweight conversion
      3. Wayback Machine — check archived version

    Args:
        url: The article URL that may be behind a paywall.

    Returns:
        Dict with "method" and content, or None if all strategies fail.
    """
    headers = {"User-Agent": "Mozilla/5.0 (compatible; LingTai/1.0)"}

    # ── Strategy 1: trafilatura ────────────────────────────────────
    # Works against client-side paywalls where content is in HTML
    # but hidden by JavaScript.
    try:
        html = trafilatura.fetch_url(url)
        if html:
            text = trafilatura.extract(html)
            if text and len(text) > 200:
                return {"method": "trafilatura", "content": text}
    except Exception:
        pass

    # ── Strategy 2: Jina Reader API ────────────────────────────────
    # Converts pages to clean markdown. Often bypasses simple paywalls.
    try:
        jina_url = f"https://r.jina.ai/{url}"
        r = requests.get(jina_url, timeout=30)
        if r.status_code == 200 and len(r.text) > 200:
            return {"method": "jina", "content": r.text}
    except Exception:
        pass

    # ── Strategy 3: Wayback Machine ────────────────────────────────
    # Check if an archived version exists. Most respectful approach.
    try:
        wb_url = f"https://archive.org/wayback/available?url={url}"
        wb = requests.get(wb_url, timeout=15)
        if wb.status_code == 200:
            data = wb.json()
            snapshots = data.get("archived_snapshots", {})
            closest = snapshots.get("closest")
            if closest and closest.get("available"):
                archive_url = closest["url"]
                # Fetch the archived page
                ar = requests.get(archive_url, timeout=15)
                if ar.status_code == 200:
                    text = trafilatura.extract(ar.text)
                    if text and len(text) > 200:
                        return {
                            "method": "wayback",
                            "archive_url": archive_url,
                            "timestamp": closest.get("timestamp"),
                            "content": text,
                        }
    except Exception:
        pass

    # ── All strategies failed — respect the paywall ────────────────
    # Try to extract at least the meta description / OG summary
    try:
        from bs4 import BeautifulSoup
        r = requests.get(url, timeout=15, headers=headers)
        soup = BeautifulSoup(r.text, "lxml")
        og_desc = soup.find("meta", property="og:description")
        meta_desc = soup.find("meta", attrs={"name": "description"})
        title = soup.find("title")
        summary = None
        if og_desc:
            summary = og_desc.get("content")
        elif meta_desc:
            summary = meta_desc.get("content")
        if summary:
            return {
                "method": "meta_only",
                "title": title.get_text() if title else None,
                "summary": summary,
                "note": "Full content behind paywall — only summary available.",
            }
    except Exception:
        pass

    return None  # Give up — respect the paywall
```

### 5.3 常见付费墙类型

| 类型 | 特征 | trafilatura | Jina | Wayback |
|------|------|:-----------:|:----:|:-------:|
| 客户端 (JS overlay) | 内容在 HTML 中，被 JS 遮盖 | ✅ 常有效 | ✅ 常有效 | ✅ |
| 计量付费墙 (metered) | 允许 N 篇/月 | ✅ | ✅ | ✅ |
| 服务器端 | HTML 中无正文 | ❌ | ❌ | ✅ |
| 硬付费墙 | 必须登录 | ❌ | ❌ | ❌ |

---

## 6. 新闻归档（Wayback Machine）

Wayback Machine 提供免费的网页归档服务。适合保存新闻链接，防止链接失效或内容被修改。

### 6.1 检查归档

```python
import requests


def wayback_check(url):
    """Check if an archived version of a URL exists.

    Args:
        url: The URL to check.

    Returns:
        Dict with "available", "url", "timestamp" if found.
    """
    api_url = "https://archive.org/wayback/available"
    params = {"url": url}
    r = requests.get(api_url, params=params, timeout=15)

    data = r.json()
    closest = data.get("archived_snapshots", {}).get("closest")

    if closest and closest.get("available"):
        return {
            "available": True,
            "url": closest["url"],
            "timestamp": closest["timestamp"],
            "status": closest.get("status"),
        }

    return {"available": False}


def wayback_get_all_snapshots(url):
    """Get list of all archived snapshots for a URL.

    Returns:
        List of dicts with "timestamp", "status", "url".
    """
    api_url = "https://web.archive.org/cdx/search/cdx"
    params = {
        "url": url,
        "output": "json",
        "fl": "timestamp,statuscode,original",
        "limit": 50,
    }
    r = requests.get(api_url, params=params, timeout=30)
    r.raise_for_status()

    rows = r.json()
    if len(rows) <= 1:
        return []

    # First row is header
    snapshots = []
    for row in rows[1:]:
        snapshots.append({
            "timestamp": row[0],
            "status": row[1],
            "url": f"https://web.archive.org/web/{row[0]}/{row[2]}",
        })

    return snapshots
```

### 6.2 保存页面

```python
def wayback_save(url):
    """Request Wayback Machine to archive a page.

    Note: This is asynchronous. The archive may take minutes to become available.

    Args:
        url: The URL to archive.

    Returns:
        Dict with "saved" status and archive URL if successful.
    """
    save_url = f"https://web.archive.org/save/{url}"
    headers = {"User-Agent": "Mozilla/5.0 (compatible; LingTai/1.0)"}

    try:
        r = requests.get(save_url, timeout=60, headers=headers)
        if r.status_code == 200:
            # The response headers may contain the archive path
            archive_path = r.headers.get("Content-Location", "")
            if archive_path:
                archive_url = f"https://web.archive.org{archive_path}"
            else:
                # Try to parse from response
                archive_url = f"https://web.archive.org/web/*/{url}"
            return {"saved": True, "archive_url": archive_url}
        return {"saved": False, "status_code": r.status_code}
    except requests.RequestException as e:
        return {"saved": False, "error": str(e)}


def wayback_save_and_verify(url, max_wait=120):
    """Save a page and wait for it to become available.

    Args:
        url:      The URL to archive.
        max_wait: Maximum seconds to wait for verification.

    Returns:
        Dict with verified archive URL.
    """
    import time

    result = wayback_save(url)
    if not result["saved"]:
        return result

    # Poll until available
    start = time.time()
    while time.time() - start < max_wait:
        check = wayback_check(url)
        if check["available"]:
            return {"saved": True, "verified": True, "archive_url": check["url"]}
        time.sleep(5)

    return {"saved": True, "verified": False, "archive_url": result.get("archive_url")}
```

### 6.3 使用场景

```python
# Scenario 1: Check if a news article has been archived
result = wayback_check("https://www.example.com/news/article-123")
if result["available"]:
    print(f"Archived at: {result['url']} (snapshot: {result['timestamp']})")
else:
    print("No archive found.")

# Scenario 2: Archive a potentially ephemeral news article
result = wayback_save("https://www.example.com/breaking-news")
print(f"Archive requested: {result}")

# Scenario 3: Get full history of a URL
snapshots = wayback_get_all_snapshots("https://www.example.com/article")
for s in snapshots:
    print(f"  {s['timestamp']} (status {s['status']}) → {s['url']}")
```

---

## 7. 失败模式与排查

### 7.1 Google News RSS

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| 返回空结果 | 查询词编码问题 | 简化关键词，避免特殊字符 |
| 返回空结果 | `ceid` 不匹配 | 确保语言/国家组合正确 |
| 连接超时 | 网络问题或被封锁 | 使用代理，或换用 Reddit |
| feedparser 报错 | 返回非 XML | 检查 HTTP 状态码，可能被重定向 |

### 7.2 Reddit JSON API

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| HTTP 429 | 速率限制 | 降低频率，确保 User-Agent 已设置 |
| HTTP 403 | 被封禁或私有子板块 | 检查子板块是否存在和公开 |
| 返回空 `children` | 子板块无帖子或不存在 | Reddit 对不存在的子板块也返回 200+空列表，需检查 `dist` 或 `children` 是否为空；尝试 `sort="new"` 或换子板块 |
| JSON 解析失败 | 可能返回 HTML 错误页 | 检查 `r.status_code` 和 `Content-Type` |

### 7.3 RSS Feed 发现

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| 发现不到 feed | 网站不提供 RSS | 尝试手动拼接常见路径 |
| 发现到的 feed 解析失败 | URL 格式错误或需要认证 | 用 `feedparser` 的 `bozo` 诊断 |
| feed 内容过时 | feed 不再更新 | 检查 `updated` 字段，寻找替代 feed |
| 中文 feed 乱码 | 编码问题 | feedparser 通常自动处理，检查 `encoding` |

### 7.4 付费墙

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| trafilatura 返回极短文本 | 服务器端付费墙 | 尝试 Jina Reader 或 Wayback |
| Jina Reader 返回付费提示 | 内容确实需要登录 | 停止尝试，仅取摘要 |
| Wayback 无归档 | 页面从未被归档 | 返回 meta 摘要，标记不可用 |
| 所有策略失败 | 硬付费墙 | 返回 `None`，记录链接供后续参考 |

### 7.5 Wayback Machine

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| `wayback_check` 返回 `available: False` | 从未被归档 | 使用 `wayback_save` 主动归档 |
| `wayback_save` 超时 | Wayback 服务繁忙 | 重试，或接受异步结果 |
| 归档页面加载慢 | Wayback CDN 负载高 | 等待或使用 `web_archive.org` 直接链接 |

---

## 8. 重试退避与降级策略

外域接口诡变无常——速率越限、连接超时、令牌见弃皆为常态。本节提供系统性的重试、退避与降级方案。

### 8.1 通用重试装饰器

```python
import requests
import time
import logging

logger = logging.getLogger(__name__)


def retry_with_backoff(max_retries=3, base_delay=2.0, backoff_factor=2.0,
                       retryable_statuses=(429, 500, 502, 503, 504)):
    """Decorator: retry a function with exponential backoff.

    Args:
        max_retries:       Maximum retry attempts (0 = no retry).
        base_delay:        Initial delay in seconds.
        backoff_factor:    Multiplier for each subsequent delay.
        retryable_statuses: HTTP status codes that trigger a retry.
    """
    def decorator(func):
        def wrapper(*args, **kwargs):
            last_exception = None
            for attempt in range(max_retries + 1):
                try:
                    result = func(*args, **kwargs)
                    # If result is a requests.Response, check status
                    if isinstance(result, requests.Response):
                        if result.status_code in retryable_statuses:
                            retry_after = result.headers.get("Retry-After")
                            delay = float(retry_after) if retry_after else base_delay * (backoff_factor ** attempt)
                            logger.warning(
                                f"HTTP {result.status_code} on attempt {attempt + 1}/{max_retries + 1}, "
                                f"retrying in {delay:.1f}s"
                            )
                            time.sleep(delay)
                            continue
                        result.raise_for_status()
                    return result
                except (requests.Timeout, requests.ConnectionError) as e:
                    last_exception = e
                    if attempt < max_retries:
                        delay = base_delay * (backoff_factor ** attempt)
                        logger.warning(
                            f"Network error on attempt {attempt + 1}/{max_retries + 1}: {e}, "
                            f"retrying in {delay:.1f}s"
                        )
                        time.sleep(delay)
                    else:
                        raise
            raise last_exception  # Should not reach here
        return wrapper
    return decorator
```

### 8.2 新闻降级链

当一个数据源失败时，自动降级到备选源：

```python
def fetch_news_with_fallback(query, max_results=10):
    """Fetch news with automatic fallback across sources.

    Priority chain:
      1. Google News RSS (broad, topic-based)
      2. Reddit search (community-driven)
      3. RSS feed of major outlet (curated)

    Returns:
        List of article dicts, or empty list if all sources fail.
    """
    articles = []

    # ── Tier 1: Google News RSS ────────────────────────────────────
    try:
        articles = google_news_rss(query, limit=max_results)
        if articles:
            return articles[:max_results]
    except Exception as e:
        logger.info(f"Google News failed, falling back: {e}")

    # ── Tier 2: Reddit Search ──────────────────────────────────────
    try:
        reddit_results = reddit_search(query, sort="new", limit=max_results)
        for r in reddit_results:
            articles.append({
                "title": r["title"],
                "url": r["url"] if not r["is_self"] else f"https://reddit.com{r.get('permalink', '')}",
                "source": f"r/{r['subreddit']}",
                "score": r["score"],
            })
        if articles:
            return articles[:max_results]
    except Exception as e:
        logger.info(f"Reddit search failed, falling back: {e}")

    # ── Tier 3: Major RSS outlets ──────────────────────────────────
    fallback_feeds = [
        "https://feeds.bbci.co.uk/news/world/rss.xml",
        "https://rss.nytimes.com/services/xml/rss/nyt/World.xml",
    ]
    for feed_url in fallback_feeds:
        try:
            parsed = parse_rss(feed_url, max_entries=max_results)
            for entry in parsed["entries"]:
                articles.append({
                    "title": entry.get("title"),
                    "url": entry.get("url"),
                    "source": parsed.get("title", feed_url),
                    "published": entry.get("published"),
                })
            if articles:
                return articles[:max_results]
        except Exception:
            continue

    logger.warning("All news sources failed.")
    return []
```

### 8.3 付费墙降级链

当逐级尝试付费墙绕行时，每层失败应优雅地继续，而非崩溃：

```python
def extract_with_graceful_degradation(url):
    """Extract article content with graceful degradation.

    Tries: full text → archive text → summary → meta → None.
    Always returns a dict with "level" indicating what was obtained.
    """
    # Level 0: Direct extraction
    try:
        import trafilatura
        html = trafilatura.fetch_url(url)
        if html:
            text = trafilatura.extract(html)
            if text and len(text) > 200:
                return {"level": "full", "method": "trafilatura", "content": text}
    except Exception:
        pass

    # Level 1: Jina Reader
    try:
        r = requests.get(f"https://r.jina.ai/{url}", timeout=30)
        if r.status_code == 200 and len(r.text) > 200:
            return {"level": "full", "method": "jina", "content": r.text}
    except Exception:
        pass

    # Level 2: Wayback Machine
    try:
        wb = requests.get(
            f"https://archive.org/wayback/available?url={url}", timeout=15
        )
        closest = wb.json().get("archived_snapshots", {}).get("closest")
        if closest and closest.get("available"):
            ar = requests.get(closest["url"], timeout=15)
            text = trafilatura.extract(ar.text)
            if text and len(text) > 200:
                return {
                    "level": "archived",
                    "method": "wayback",
                    "timestamp": closest["timestamp"],
                    "content": text,
                }
    except Exception:
        pass

    # Level 3: Meta summary only
    try:
        from bs4 import BeautifulSoup
        r = requests.get(url, timeout=15, headers={"User-Agent": "Mozilla/5.0"})
        soup = BeautifulSoup(r.text, "lxml")
        og_desc = soup.find("meta", property="og:description")
        title = soup.find("title")
        summary = og_desc.get("content") if og_desc else None
        if summary:
            return {
                "level": "summary",
                "title": title.get_text() if title else None,
                "content": summary,
            }
    except Exception:
        pass

    # Level 4: Nothing available
    return {"level": "unavailable", "url": url}
```

### 8.4 各平台退避参数

| 平台 | 首次失败延迟 | 最大重试 | 退避因子 | 429 特殊处理 |
|------|-------------|----------|---------|-------------|
| Google News RSS | 2s | 3 | 2x | 无 Retry-After header，用指数退避 |
| Reddit | 2s | 3 | 3x | 遵守 `Retry-After` header（秒数） |
| Wayback Machine | 5s | 2 | 2x | 保存操作 timeout 设 60s |
| RSS Feed | 3s | 2 | 2x | 解析失败不重试，换 feed |

---

## 9. 依赖安装

```bash
pip install requests feedparser beautifulsoup4 lxml trafilatura
```

| 包 | 用途 |
|---|------|
| `requests` | HTTP 请求 |
| `feedparser` | RSS/Atom 解析 |
| `beautifulsoup4` | HTML 解析与清理 |
| `lxml` | XML/HTML 解析后端 |
| `trafilatura` | 网页正文提取 |

### 日志配置

**缓存策略：** 新闻与 RSS 数据有天然时效性，但短时间内重复请求同一 URL 纯属浪费且易触发封禁。建议使用共器 `cached_get`：

```python
import sys
sys.path.insert(0, os.path.expanduser("~/.lingtai-tui/utilities/_shared/scripts"))
from cached_get import cached_get

# 用法与 requests.get 一致，多一个 ttl 参数和 cache_name 参数
r = cached_get("https://news.google.com/rss/search",
               params={"q": "AI"}, ttl=300, cache_name="news")
feed = feedparser.parse(r.text)
```

共器位于 `~/.lingtai-tui/utilities/_shared/scripts/cached_get.py`，特性：
- 原子写入（先书临时，成而后正名，断电不生残卷）
- 自动淘汰陈腐（>1 天自动删除，总量上限 200 条）
- 按 `cache_name` 隔离不同技能的缓存目录

**建议 TTL：**

| 数据源 | TTL | 理由 |
|--------|-----|------|
| Google News RSS | 300s (5min) | 新闻更新快，但 5 分钟内不会大变 |
| Reddit 帖子列表 | 120s (2min) | 热帖排序变化较快 |
| RSS Feed | 600s (10min) | 多数站点更新频率较低 |
| Wayback Machine | 86400s (1day) | 归档数据不会变 |
| 付费墙尝试 | 3600s (1hr) | 短时间重复尝试无意义 |

本技能代码中使用 `logging` 模块记录请求成功、降级触发与异常信息。调用前须先配置：

```python
import logging

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%H:%M:%S",
)
# 或为特定模块设更细粒度：
logging.getLogger("news_and_rss").setLevel(logging.DEBUG)
```

**日志级别约定：**

| 级别 | 用途 |
|------|------|
| `DEBUG` | 完整请求 URL、响应状态码、解析字段详情 |
| `INFO` | 降级触发、数据源切换、结果数量 |
| `WARNING` | 重试中、速率限制命中、付费墙检测 |
| `ERROR` | 全部数据源失败、无法恢复的异常 |

---

## 10. 与主技能的关系

本技能是 **web-browsing-manual v3.0** 的子技能（sub-skill）。父技能提供了完整的网页浏览策略（七层渐进策略、学术 API、搜索引擎、反检测技术等），本技能专注于其中的新闻获取领域。

- 需要通用网页抓取 → 参见 `web-browsing-manual`
- 需要社交媒体数据提取 → 参见 `social-media-extraction`
- 需要学术论文搜索 → 参见 `web-browsing-manual` 中的学术 API 章节
