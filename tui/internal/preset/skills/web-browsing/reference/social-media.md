# Social Media Extraction

> Part of the [web-browsing](../SKILL.md) skill.
> Reddit, Hacker News, Mastodon, X/Twitter, GitHub data extraction.


本技能是 `web-browsing-manual` 的子技能，专注于从社交媒体平台提取公开数据。涵盖 Reddit、Hacker News、Mastodon、X/Twitter 和 GitHub 的公开 API 与页面提取方法，包括速率限制、认证说明、完整代码片段、失败模式与伦理边界。

---

## 1. 社交媒体决策树

面对"我需要从社交媒体获取数据"这个需求时，按下图选择路径：

```
社交媒体数据
  │
  ├─ Reddit? ────────────→ JSON API（§2）
  │   （帖子、评论、搜索）
  │   方法: URL 追加 .json
  │
  ├─ Hacker News? ───────→ Firebase API（§3）
  │   （技术讨论、Show HN）
  │   方法: firebaseio.com/v0/
  │
  ├─ Mastodon? ──────────→ 公开 API（§4）
  │   （联邦宇宙帖子、搜索）
  │   方法: /api/v1/ 或 /api/v2/
  │
  ├─ X/Twitter? ─────────→ 公开页面提取（§5，受限）
  │   （推文、用户信息）
  │   方法: Nitter 实例（不稳定）
  │   边界: 不绕过登录
  │
  └─ GitHub? ────────────→ REST/GraphQL API（§6）
      （仓库、用户、代码搜索）
      方法: api.github.com
```

**快速选择指南：**

| 平台 | 免费额度 | 认证需求 | 稳定性 | 数据丰富度 |
|------|----------|----------|--------|-----------|
| Reddit | 60 req/min | 无需（设 User-Agent 即可） | 高 | 高 |
| Hacker News | 无限制 | 无需 | 极高 | 中 |
| Mastodon | 因实例而异 | 无需（公开端点） | 高 | 高 |
| X/Twitter | 极有限 | 官方 API 需付费 | 低 | 低 |
| GitHub | 60/hr (无 token) | 无需（有 token 则 5000/hr） | 极高 | 极高 |

---

## 2. Reddit JSON API 详细用法

Reddit 提供的 JSON API 是最易用的社交媒体接口之一——只需在标准 URL 后追加 `.json`。

### 2.1 子版块帖子

```python
import requests
import time

# Rate limiting
_last_request_time = 0
_MIN_INTERVAL = 2.0


def _reddit_get(url, params=None):
    """Rate-limited GET for Reddit API."""
    global _last_request_time
    elapsed = time.time() - _last_request_time
    if elapsed < _MIN_INTERVAL:
        time.sleep(_MIN_INTERVAL - elapsed)
    _last_request_time = time.time()
    headers = {"User-Agent": "LingTai/1.0 (Social Media Extractor)"}
    return requests.get(url, headers=headers, params=params, timeout=15)


def reddit_subreddit(subreddit, sort="hot", limit=25, timeframe="day"):
    """Get posts from a subreddit.

    Args:
        subreddit: Name without r/ prefix (e.g. "python").
        sort: "hot", "new", "top", "rising", "controversial".
        limit: Max 100 posts per request.
        timeframe: For "top" sort — "hour", "day", "week", "month", "year", "all".

    Returns:
        List of post dicts.
    """
    url = f"https://www.reddit.com/r/{subreddit}/{sort}.json"
    params = {"limit": limit}
    if sort in ("top", "controversial"):
        params["t"] = timeframe

    r = _reddit_get(url, params=params)
    r.raise_for_status()

    posts = []
    for child in r.json()["data"]["children"]:
        d = child["data"]
        posts.append({
            "id": d.get("name"),        # fullname: t3_{id}
            "title": d.get("title"),
            "url": d.get("url"),
            "score": d.get("score"),
            "upvote_ratio": d.get("upvote_ratio"),
            "num_comments": d.get("num_comments"),
            "author": d.get("author"),
            "created_utc": d.get("created_utc"),
            "selftext": d.get("selftext", "")[:1000],
            "is_self": d.get("is_self", False),
            "link_flair_text": d.get("link_flair_text"),
            "over_18": d.get("over_18", False),
            "subreddit": d.get("subreddit"),
            "permalink": d.get("permalink"),
        })
    return posts


def reddit_subreddit_top_week(subreddit, limit=25):
    """Shortcut: top posts this week."""
    return reddit_subreddit(subreddit, sort="top", limit=limit, timeframe="week")
```

### 2.2 帖子评论

```python
def reddit_comments(post_id, sort="best", limit=25, depth=None):
    """Get comments for a Reddit post.

    Args:
        post_id:  Post ID (e.g. "abc123") or fullname (e.g. "t3_abc123").
        sort:     "best", "top", "new", "controversial", "old", "qa".
        limit:    Max comments to return.
        depth:    Maximum comment nesting depth (optional).

    Returns:
        List of comment dicts.
    """
    # Strip fullname prefix if present
    if post_id.startswith("t3_"):
        post_id = post_id[3:]

    url = f"https://www.reddit.com/comments/{post_id}.json"
    params = {"sort": sort, "limit": limit}
    if depth is not None:
        params["depth"] = depth

    r = _reddit_get(url, params=params)
    r.raise_for_status()

    # Response is a 2-element array: [post_listing, comments_listing]
    data = r.json()
    comments = []

    def extract_comments(children, level=0):
        """Recursively extract comments."""
        for child in children:
            if child["kind"] == "t1":  # comment
                d = child["data"]
                comments.append({
                    "id": d.get("id"),
                    "author": d.get("author"),
                    "body": d.get("body", "")[:2000],
                    "score": d.get("score"),
                    "created_utc": d.get("created_utc"),
                    "level": level,
                    "is_submitter": d.get("is_submitter", False),
                })
                # Recurse into replies
                replies = d.get("replies")
                if isinstance(replies, dict):
                    extract_comments(replies["data"]["children"], level + 1)

    if len(data) > 1:
        extract_comments(data[1]["data"]["children"])

    return comments
```

### 2.3 搜索

```python
def reddit_search(query, sort="relevance", limit=25, subreddit=None, timeframe="all"):
    """Search Reddit posts.

    Args:
        query:     Search string. Supports AND/OR/NOT and field search.
        sort:      "relevance", "hot", "top", "new", "comments".
        limit:     Max results (100 per request).
        subreddit: Restrict to subreddit (optional).
        timeframe: "hour", "day", "week", "month", "year", "all".
    """
    if subreddit:
        url = f"https://www.reddit.com/r/{subreddit}/search.json"
        params = {"q": query, "sort": sort, "limit": limit,
                  "t": timeframe, "restrict_sr": "on"}
    else:
        url = "https://www.reddit.com/search.json"
        params = {"q": query, "sort": sort, "limit": limit, "t": timeframe}

    r = _reddit_get(url, params=params)
    r.raise_for_status()

    return [
        {
            "title": c["data"]["title"],
            "url": c["data"]["url"],
            "score": c["data"]["score"],
            "subreddit": c["data"].get("subreddit"),
            "created_utc": c["data"].get("created_utc"),
            "selftext": c["data"].get("selftext", "")[:500],
        }
        for c in r.json()["data"]["children"]
    ]
```

### 2.4 用户信息

```python
def reddit_user(username, limit=25):
    """Get a user's posts and comments.

    Args:
        username: Reddit username (without u/ prefix).
        limit:    Max items to return.
    """
    url = f"https://www.reddit.com/user/{username}.json"
    params = {"limit": limit, "sort": "new"}
    r = _reddit_get(url, params=params)
    r.raise_for_status()

    items = []
    for child in r.json()["data"]["children"]:
        d = child["data"]
        items.append({
            "type": "post" if child["kind"] == "t3" else "comment",
            "subreddit": d.get("subreddit"),
            "title": d.get("title"),
            "body": d.get("selftext") or d.get("body", "")[:500],
            "score": d.get("score"),
            "created_utc": d.get("created_utc"),
        })
    return items
```

### 2.5 Reddit API 规则与边界

| 规则 | 详情 |
|------|------|
| 速率限制 | ~60 req/min（无 OAuth）；如遇 429 则降速 |
| User-Agent | **必须设置**，否则可能被封禁。推荐格式: `App/Version (Purpose)` |
| 请求间隔 | 建议每次 ≥ 2 秒 |
| 分页 | 用 `after` / `before` 参数，值取自上次返回的 `data.after` |
| 登录 | **不要模拟登录或绕过认证**。如需更高限额，使用官方 OAuth 流程。 |
| 数据范围 | 只能获取公开子版块和数据。私有/quarantined 子版块无法访问。 |
| NSFW | `over_18: True` 的帖子在应用中应妥善处理。 |

---

## 3. Hacker News Firebase API

Hacker News (HN) 提供完全免费、无速率限制的 Firebase API。这是获取技术社区讨论的最佳数据源。

### 3.1 完整代码

```python
import requests

HN_API = "https://hacker-news.firebaseio.com/v0"


def hn_get_item(item_id):
    """Get a single item (story, comment, job, poll) by ID."""
    r = requests.get(f"{HN_API}/item/{item_id}.json", timeout=10)
    r.raise_for_status()
    return r.json()


def hn_top_stories(limit=30):
    """Get current top stories."""
    r = requests.get(f"{HN_API}/topstories.json", timeout=15)
    ids = r.json()[:limit]
    return _hn_fetch_items(ids)


def hn_new_stories(limit=30):
    """Get newest stories."""
    r = requests.get(f"{HN_API}/newstories.json", timeout=15)
    ids = r.json()[:limit]
    return _hn_fetch_items(ids)


def hn_best_stories(limit=30):
    """Get best stories (highest scoring)."""
    r = requests.get(f"{HN_API}/beststories.json", timeout=15)
    ids = r.json()[:limit]
    return _hn_fetch_items(ids)


def hn_ask_stories(limit=30):
    """Get Ask HN stories."""
    r = requests.get(f"{HN_API}/askstories.json", timeout=15)
    ids = r.json()[:limit]
    return _hn_fetch_items(ids)


def hn_show_stories(limit=30):
    """Get Show HN stories."""
    r = requests.get(f"{HN_API}/showstories.json", timeout=15)
    ids = r.json()[:limit]
    return _hn_fetch_items(ids)


def hn_job_stories(limit=30):
    """Get job postings."""
    r = requests.get(f"{HN_API}/jobstories.json", timeout=15)
    ids = r.json()[:limit]
    return _hn_fetch_items(ids)


def _hn_fetch_items(ids):
    """Fetch multiple items by ID list."""
    stories = []
    for id_ in ids:
        item = hn_get_item(id_)
        if item:
            stories.append({
                "id": item.get("id"),
                "type": item.get("type"),     # story, comment, job, poll
                "title": item.get("title"),
                "url": item.get("url"),
                "text": item.get("text"),      # HTML for self-posts
                "score": item.get("score"),
                "by": item.get("by"),
                "time": item.get("time"),      # Unix timestamp
                "descendants": item.get("descendants", 0),  # comment count
                "kids": item.get("kids", []),  # direct comment IDs
            })
    return stories
```

### 3.2 评论递归提取

```python
def hn_comments(story_id, max_depth=3, max_comments=50):
    """Recursively fetch comments for a story.

    Args:
        story_id:     The story/item ID.
        max_depth:    Maximum nesting depth to follow.
        max_comments: Total comments to collect (safety limit).

    Returns:
        List of comment dicts with "level" field.
    """
    collected = []

    def _recurse(item_ids, depth=0):
        if depth > max_depth or len(collected) >= max_comments:
            return
        for kid_id in item_ids:
            if len(collected) >= max_comments:
                return
            item = hn_get_item(kid_id)
            if not item or item.get("deleted") or item.get("dead"):
                continue
            if item.get("type") == "comment":
                collected.append({
                    "id": item["id"],
                    "by": item.get("by"),
                    "text": item.get("text", ""),  # HTML
                    "time": item.get("time"),
                    "level": depth,
                    "parent": item.get("parent"),
                })
                if item.get("kids"):
                    _recurse(item["kids"], depth + 1)

    story = hn_get_item(story_id)
    if story and story.get("kids"):
        _recurse(story["kids"])

    return collected
```

### 3.3 用户信息

```python
def hn_user(username):
    """Get Hacker News user profile."""
    r = requests.get(f"{HN_API}/user/{username}.json", timeout=10)
    r.raise_for_status()
    data = r.json()
    if not data:
        return None
    return {
        "id": data.get("id"),
        "karma": data.get("karma"),
        "about": data.get("about"),         # HTML
        "created": data.get("created"),     # Unix timestamp
        "submitted": data.get("submitted", [])[:100],  # item IDs
    }
```

### 3.4 HN API 特性

| 特性 | 详情 |
|------|------|
| 完全免费 | 无需 API Key |
| 无速率限制 | 但应合理使用，避免并发大量请求 |
| 数据格式 | JSON |
| 时间格式 | Unix 时间戳 |
| 评论 HTML | `text` 字段包含 HTML，需要自行清理 |
| 故事类型 | `story`, `comment`, `job`, `poll`, `pollopt` |
| 删除/死亡 | `deleted: true` 或 `dead: true` 表示已被删除/屏蔽 |

---

## 4. Mastodon 公开 API

Mastodon 是去中心化的社交网络，每个实例（instance）都有独立的 API。公开端点无需认证。

### 4.1 完整代码

```python
import requests


def mastodon_trends(instance="mastodon.social", limit=20):
    """Get trending statuses from a Mastodon instance.

    Args:
        instance: Domain of the Mastodon instance.
        limit:    Number of trending posts.

    Returns:
        List of status dicts.
    """
    url = f"https://{instance}/api/v1/trends/statuses"
    params = {"limit": limit}
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()

    statuses = []
    for s in r.json():
        statuses.append(_parse_mastodon_status(s))
    return statuses


def mastodon_trends_links(instance="mastodon.social", limit=20):
    """Get trending links from a Mastodon instance."""
    url = f"https://{instance}/api/v1/trends/links"
    params = {"limit": limit}
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()


def mastodon_search(instance, query, search_type="statuses", limit=20):
    """Search public content on a Mastodon instance.

    Args:
        instance:    Domain (e.g. "mastodon.social").
        query:       Search query string.
        search_type: "statuses", "accounts", or "hashtags".
        limit:       Max results.
    """
    url = f"https://{instance}/api/v2/search"
    params = {"q": query, "type": search_type, "limit": limit}
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()

    data = r.json()
    results = data.get(search_type, [])
    if search_type == "statuses":
        return [_parse_mastodon_status(s) for s in results]
    return results


def mastodon_account_statuses(instance, account_id, limit=20):
    """Get public statuses from a specific account.

    Args:
        instance:   Domain.
        account_id: Numeric account ID (not username).
        limit:      Max statuses.
    """
    url = f"https://{instance}/api/v1/accounts/{account_id}/statuses"
    params = {"limit": limit, "exclude_replies": "true"}
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return [_parse_mastodon_status(s) for s in r.json()]


def mastodon_hashtag(instance, tag, limit=20):
    """Get recent statuses with a hashtag."""
    url = f"https://{instance}/api/v1/timelines/tag/{tag}"
    params = {"limit": limit}
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return [_parse_mastodon_status(s) for s in r.json()]


def mastodon_instance_info(instance):
    """Get information about a Mastodon instance."""
    url = f"https://{instance}/api/v1/instance"
    r = requests.get(url, timeout=15)
    r.raise_for_status()
    return r.json()


def _parse_mastodon_status(s):
    """Parse a Mastodon status object into a cleaner dict."""
    account = s.get("account", {})
    return {
        "id": s.get("id"),
        "url": s.get("url"),
        "content": s.get("content"),             # HTML
        "created_at": s.get("created_at"),
        "reblogs_count": s.get("reblogs_count"),
        "favourites_count": s.get("favourites_count"),
        "replies_count": s.get("replies_count"),
        "account": {
            "username": account.get("username"),
            "display_name": account.get("display_name"),
            "url": account.get("url"),
            "followers_count": account.get("followers_count"),
        },
        "tags": [t.get("name") for t in s.get("tags", [])],
        "sensitive": s.get("sensitive", False),
        "media_attachments": [
            {"type": m.get("type"), "url": m.get("url")}
            for m in s.get("media_attachments", [])
        ],
    }
```

### 4.2 常用实例

| 实例 | 特色 |
|------|------|
| mastodon.social | 最大的通用实例 |
| fosstodon.org | 开源技术社区 |
| hachyderm.io | 技术专业人士 |
| mas.to | 通用实例 |
| mstdn.jp | 日语社区 |

### 4.3 Mastodon API 特性

| 特性 | 详情 |
|------|------|
| 去中心化 | 每个实例有独立 API，数据不互通 |
| 公开端点 | 无需认证即可访问公开内容 |
| 速率限制 | 因实例而异，通常 300 req/5min |
| 内容格式 | HTML（`content` 字段需自行清理标签） |
| 分页 | 使用 `Link` 响应头中的 `next` / `prev` URL |

### 4.4 HTML 内容清理

```python
from bs4 import BeautifulSoup


def clean_mastodon_html(html_content):
    """Remove HTML tags from Mastodon content."""
    if not html_content:
        return ""
    soup = BeautifulSoup(html_content, "lxml")
    return soup.get_text(separator=" ", strip=True)
```

---

## 5. X/Twitter 公开页面（边界与限制）

### 5.1 现状与限制

X（原 Twitter）的数据获取面临严重限制：

| 方式 | 可行性 | 说明 |
|------|--------|------|
| 官方 API | 需付费 | Free tier 仅能发推，不能读取 |
| 公开页面抓取 | 极不稳定 | 页面严重依赖 JS 渲染，反爬严格 |
| Nitter 实例 | 不稳定 | 2024 年后大量实例关闭 |
| 第三方服务 | 可行但需评估 | 如 SocialData, Twitter API Pro |

### 5.2 伦理边界

**严格遵守以下原则：**

- ✅ 使用官方 API（如有付费订阅）
- ✅ 使用 Nitter 等公开替代前端（如可用）
- ✅ 提取 OG meta 标签（页面提供的公开元数据）
- ❌ 不尝试绕过登录
- ❌ 不使用抓取工具破解 JS 渲染后的登录墙
- ❌ 不使用被盗或泄露的 API 凭证

### 5.3 基本提取（如果页面可访问）

```python
import requests
from bs4 import BeautifulSoup


def twitter_meta_extract(url):
    """Extract metadata from a public Twitter/X page.

    This only works if the page is accessible without login.
    Primarily extracts OpenGraph and Twitter Card meta tags.

    Args:
        url: Twitter/X post URL.

    Returns:
        Dict with available metadata, or None.
    """
    headers = {
        "User-Agent": "Mozilla/5.0 (compatible; LingTai/1.0)",
    }
    try:
        r = requests.get(url, headers=headers, timeout=15)
        if r.status_code != 200:
            return None

        soup = BeautifulSoup(r.text, "lxml")

        # Extract OG meta tags
        og = {}
        for tag in soup.find_all("meta"):
            prop = tag.get("property", "")
            if prop.startswith("og:"):
                og[prop[3:]] = tag.get("content", "")

        # Extract Twitter Card meta tags
        twitter = {}
        for tag in soup.find_all("meta"):
            name = tag.get("name", "")
            if name.startswith("twitter:"):
                twitter[name[8:]] = tag.get("content", "")

        if not og and not twitter:
            return None

        return {
            "title": og.get("title") or twitter.get("title"),
            "description": og.get("description") or twitter.get("description"),
            "image": og.get("image"),
            "url": og.get("url"),
            "site_name": og.get("site_name"),
            "card_type": twitter.get("card"),
        }
    except Exception:
        return None


def nitter_extract(nitter_url):
    """Extract content from a Nitter instance (if available).

    Nitter instances provide a lightweight, JS-free view of tweets.
    However, many instances have shut down since 2024.

    Args:
        nitter_url: Full Nitter URL (e.g. "https://nitter.net/user/status/123").

    Returns:
        Extracted text content, or None.
    """
    headers = {"User-Agent": "Mozilla/5.0 (compatible; LingTai/1.0)"}
    try:
        r = requests.get(nitter_url, headers=headers, timeout=15)
        if r.status_code != 200:
            return None

        soup = BeautifulSoup(r.text, "lxml")
        # Nitter uses specific CSS classes
        tweet_content = soup.find("div", class_="tweet-content")
        if tweet_content:
            return tweet_content.get_text(strip=True)

        # Fallback: try the main content area
        return soup.get_text(strip=True)[:1000]
    except Exception:
        return None
```

### 5.4 实用建议

```python
# Practical approach: use Jina Reader as fallback
def twitter_via_jina(url):
    """Try to get tweet content via Jina Reader."""
    jina_url = f"https://r.jina.ai/{url}"
    try:
        r = requests.get(jina_url, timeout=30)
        if r.status_code == 200 and len(r.text) > 50:
            return r.text
    except Exception:
        pass
    return None
```

> **底线：** 如果以上方法都无法获取内容，说明 X/Twitter 的数据确实不可公开获取。接受这个限制，不要进一步尝试。

---

## 6. GitHub 公开数据

GitHub 提供 REST API v3 和 GraphQL API v4，是获取代码仓库、用户、issue、PR 等数据的最佳途径。

### 6.1 仓库搜索

```python
import requests
import time

# GitHub rate limiter
_gh_last_request = 0
_GH_MIN_INTERVAL = 1.0


def _github_get(url, params=None, token=None):
    """Rate-limited GET for GitHub API."""
    global _gh_last_request
    elapsed = time.time() - _gh_last_request
    if elapsed < _GH_MIN_INTERVAL:
        time.sleep(_GH_MIN_INTERVAL - elapsed)
    _gh_last_request = time.time()

    headers = {
        "Accept": "application/vnd.github.v3+json",
        "User-Agent": "LingTai/1.0",
    }
    if token:
        headers["Authorization"] = f"token {token}"

    r = requests.get(url, headers=headers, params=params, timeout=15)
    r.raise_for_status()
    return r


def github_search_repos(query, sort="stars", order="desc", per_page=10, token=None):
    """Search GitHub repositories.

    Args:
        query:    Search query. Supports qualifiers like "language:python", "stars:>1000".
        sort:     "stars", "forks", "help-wanted-issues", "updated".
        order:    "desc" or "asc".
        per_page: Max 100.
        token:    Optional GitHub personal access token.
    """
    url = "https://api.github.com/search/repositories"
    params = {"q": query, "sort": sort, "order": order, "per_page": per_page}
    r = _github_get(url, params=params, token=token)

    repos = []
    for item in r.json().get("items", []):
        repos.append({
            "full_name": item.get("full_name"),
            "description": item.get("description"),
            "url": item.get("html_url"),
            "stars": item.get("stargazers_count"),
            "forks": item.get("forks_count"),
            "language": item.get("language"),
            "topics": item.get("topics", []),
            "created_at": item.get("created_at"),
            "updated_at": item.get("updated_at"),
            "license": item.get("license", {}).get("spdx_id") if item.get("license") else None,
            "open_issues": item.get("open_issues_count"),
        })
    return repos


def github_search_code(query, per_page=10, token=None):
    """Search code on GitHub.

    Note: Code search requires authentication (token required).
    """
    url = "https://api.github.com/search/code"
    params = {"q": query, "per_page": per_page}
    r = _github_get(url, params=params, token=token)
    return r.json().get("items", [])
```

### 6.2 仓库详情与 README

```python
def github_repo_info(owner, repo, token=None):
    """Get detailed repository information."""
    url = f"https://api.github.com/repos/{owner}/{repo}"
    r = _github_get(url, token=token)
    data = r.json()
    return {
        "full_name": data.get("full_name"),
        "description": data.get("description"),
        "url": data.get("html_url"),
        "stars": data.get("stargazers_count"),
        "forks": data.get("forks_count"),
        "watchers": data.get("watchers_count"),
        "language": data.get("language"),
        "topics": data.get("topics", []),
        "license": data.get("license", {}).get("spdx_id") if data.get("license") else None,
        "default_branch": data.get("default_branch"),
        "created_at": data.get("created_at"),
        "updated_at": data.get("updated_at"),
        "pushed_at": data.get("pushed_at"),
        "size": data.get("size"),  # KB
        "open_issues": data.get("open_issues_count"),
        "network_count": data.get("network_count"),
        "subscribers_count": data.get("subscribers_count"),
    }


def github_readme(owner, repo, token=None):
    """Get repository README content.

    Returns raw text of the README file (README.md, README.rst, etc.)
    """
    url = f"https://api.github.com/repos/{owner}/{repo}/readme"
    headers = {
        "Accept": "application/vnd.github.raw",
        "User-Agent": "LingTai/1.0",
    }
    if token:
        headers["Authorization"] = f"token {token}"

    r = requests.get(url, headers=headers, timeout=15)
    if r.status_code == 200:
        return r.text
    return None


def github_repo_contents(owner, repo, path="", token=None):
    """List repository directory contents."""
    url = f"https://api.github.com/repos/{owner}/{repo}/contents/{path}"
    r = _github_get(url, token=token)
    return [
        {
            "name": item.get("name"),
            "path": item.get("path"),
            "type": item.get("type"),   # "file", "dir", "symlink"
            "size": item.get("size"),
            "url": item.get("html_url"),
        }
        for item in r.json()
    ]
```

### 6.3 Issue 和 Pull Request

```python
def github_issues(owner, repo, state="open", sort="created", direction="desc",
                  per_page=20, token=None):
    """List repository issues.

    Args:
        state:     "open", "closed", "all".
        sort:      "created", "updated", "comments".
        direction: "desc" or "asc".
    """
    url = f"https://api.github.com/repos/{owner}/{repo}/issues"
    params = {"state": state, "sort": sort, "direction": direction, "per_page": per_page}
    r = _github_get(url, params=params, token=token)

    issues = []
    for item in r.json():
        # GitHub API returns PRs in the issues endpoint too
        is_pr = "pull_request" in item
        issues.append({
            "number": item.get("number"),
            "title": item.get("title"),
            "state": item.get("state"),
            "is_pr": is_pr,
            "user": item.get("user", {}).get("login"),
            "labels": [l.get("name") for l in item.get("labels", [])],
            "created_at": item.get("created_at"),
            "updated_at": item.get("updated_at"),
            "comments": item.get("comments"),
            "body": (item.get("body") or "")[:1000],
            "url": item.get("html_url"),
        })
    return issues


def github_pull_requests(owner, repo, state="open", sort="created", direction="desc",
                         per_page=20, token=None):
    """List repository pull requests."""
    url = f"https://api.github.com/repos/{owner}/{repo}/pulls"
    params = {"state": state, "sort": sort, "direction": direction, "per_page": per_page}
    r = _github_get(url, params=params, token=token)

    return [
        {
            "number": pr.get("number"),
            "title": pr.get("title"),
            "state": pr.get("state"),
            "user": pr.get("user", {}).get("login"),
            "merged": pr.get("merged", False),
            "created_at": pr.get("created_at"),
            "updated_at": pr.get("updated_at"),
            "additions": pr.get("additions"),
            "deletions": pr.get("deletions"),
            "changed_files": pr.get("changed_files"),
            "url": pr.get("html_url"),
        }
        for pr in r.json()
    ]
```

### 6.4 用户信息

```python
def github_user(username, token=None):
    """Get GitHub user profile."""
    url = f"https://api.github.com/users/{username}"
    r = _github_get(url, token=token)
    data = r.json()
    return {
        "login": data.get("login"),
        "name": data.get("name"),
        "company": data.get("company"),
        "blog": data.get("blog"),
        "location": data.get("location"),
        "bio": data.get("bio"),
        "public_repos": data.get("public_repos"),
        "public_gists": data.get("public_gists"),
        "followers": data.get("followers"),
        "following": data.get("following"),
        "created_at": data.get("created_at"),
        "url": data.get("html_url"),
    }
```

### 6.5 GitHub API 速率限制

| 认证状态 | 搜索 API | 普通 API |
|----------|----------|----------|
| 无 Token | 10 req/min | 60 req/hr |
| 有 Token | 30 req/min | 5000 req/hr |

**检查剩余配额：**

```python
def github_rate_limit(token=None):
    """Check current GitHub API rate limit status."""
    url = "https://api.github.com/rate_limit"
    headers = {"User-Agent": "LingTai/1.0"}
    if token:
        headers["Authorization"] = f"token {token}"
    r = requests.get(url, headers=headers, timeout=15)
    data = r.json()
    return {
        "core": data["resources"]["core"],
        "search": data["resources"]["search"],
    }
```

**关键 Accept Header：**

```
application/vnd.github.v3+json          # 标准 JSON 响应
application/vnd.github.raw              # README 原始内容
application/vnd.github.html             # HTML 渲染的 Markdown
application/vnd.github.diff             # diff 格式
application/vnd.github.patch            # patch 格式
```

---

## 7. 失败模式与平台特殊注意

### 7.1 Reddit

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| HTTP 429 | 速率限制 | 等待 > 2s，确认 User-Agent 已设 |
| HTTP 403 | 子版块私有/封禁 | 确认子版块公开存在 |
| 返回 HTML 而非 JSON | URL 格式错误 | 确认 URL 以 `.json` 结尾 |
| 数据不完整 | Reddit 截断长内容 | 使用 `after` 分页获取更多 |
| HTTP 200 但 `children` 为空 | 子版块不存在或无帖子 | Reddit 对不存在的子版块也返回 200+空列表；检查 `data.dist` 为 0 则可能是无效子版块 |
| 评论区为空 | 帖子无评论或被锁 | 检查 `locked` 字段 |

**特殊注意：**
- 子版块名称大小写不敏感，但建议用小写。
- `selftext` 可能包含 Markdown，需要渲染或转义。
- `over_18` 标记的帖子应做适当处理。

### 7.2 Hacker News

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| 返回 `null` | item ID 无效或已删除 | 跳过 `null` 结果 |
| 评论数过多 | 递归过深 | 设 `max_depth` 和 `max_comments` |
| 网络超时 | Firebase 响应慢 | 增大 timeout，重试 |
| 重复获取同一 item | 列表中有重复 ID | 去重处理 |

**特殊注意：**
- `text` 字段是 HTML 格式，需要清理。
- `url` 为 `None` 表示自帖（Ask HN 等），内容在 `text` 中。
- 时间是 Unix 时间戳，需要转换。
- `deleted` 和 `dead` 字段标记已删除或被屏蔽的内容。

### 7.3 Mastodon

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| HTTP 404 | 实例不存在或 API 版本不同 | 尝试其他实例 |
| HTTP 429 | 实例速率限制 | 降低请求频率，换实例 |
| 返回空列表 | 搜索无结果 | 扩大搜索范围，换关键词 |
| HTML 标签未清理 | `content` 是 HTML | 使用 BeautifulSoup 清理 |
| 跨实例内容不可见 | 去中心化限制 | 尝试目标用户所在的实例 |

**特殊注意：**
- 不同实例的速率限制不同，`/api/v1/instance` 可查看实例规则。
- 帖子 `content` 是 HTML，始终需要清理。
- `account_id` 是数字，不是用户名（格式 `@user@instance`）。
- 搜索端点 `/api/v2/search` 的结果取决于实例已知的联邦内容。

### 7.4 X/Twitter

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| 页面重定向到登录 | 需要登录 | 接受限制，尝试 meta 提取 |
| Nitter 502/503 | 实例已关闭 | 尝试其他实例或放弃 |
| Jina Reader 返回登录页 | 被检测 | 放弃，接受不可获取 |
| OG meta 为空 | 页面未正确渲染 | 无法获取 |

**特殊注意：**
- **X/Twitter 数据获取是目前最困难的。** 官方 API 需付费（最低 $100/月）。
- Nitter 实例大量关闭，不可依赖。
- 唯一可靠的免费方式是通过搜索引擎（Google `site:twitter.com`）获取缓存片段。
- **伦理底线：绝不尝试绕过登录验证。**

### 7.5 GitHub

| 症状 | 原因 | 解决方案 |
|------|------|----------|
| HTTP 403 + rate limit | 配额用尽 | 等待重置或使用 Token |
| HTTP 404 | 仓库不存在或私有 | 确认仓库名拼写 |
| 搜索无结果 | 查询语法错误 | 检查 qualifier 格式 |
| README 返回 404 | 仓库无 README | 返回 None，正常处理 |
| 大仓库内容截断 | API 分页 | 使用 page 参数 |

**特殊注意：**
- 搜索 API 限额（10/min 无 Token）远低于普通 API（60/hr 无 Token）。
- `Accept` header 控制返回格式，获取 README 用 `application/vnd.github.raw`。
- 仓库的 `size` 单位是 KB。
- Issue API 也包含 PR，用 `pull_request` 字段区分。

---

## 8. 重试退避与降级策略

外域接口诡变无常——速率越限、连接超时、令牌见夺皆为常态。本节提供系统性的重试、退避与降级方案。

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
            raise last_exception
        return wrapper
    return decorator
```

### 8.2 社交媒体降级链

当一个平台失败时，自动降级到备选平台获取相似数据：

```python
def fetch_tech_discussion(query, max_results=10):
    """Fetch tech community discussion with fallback across platforms.

    Priority chain:
      1. Reddit (r/technology + search)
      2. Hacker News (search via Algolia)
      3. Mastodon (search on mastodon.social)

    Returns:
        List of dicts with title, url, score, source_platform.
    """
    results = []

    # ── Tier 1: Reddit ───────────────────────────────────────────
    try:
        posts = reddit_search(query, sort="new", limit=max_results)
        for p in posts:
            results.append({
                "title": p["title"],
                "url": p["url"],
                "score": p.get("score"),
                "comments": p.get("num_comments"),
                "source_platform": "reddit",
                "subreddit": p.get("subreddit"),
            })
        if results:
            return results[:max_results]
    except Exception as e:
        logger.info(f"Reddit failed, falling back: {e}")

    # ── Tier 2: Hacker News (via Algolia search API) ─────────────
    try:
        algolia_url = "https://hn.algolia.com/api/v1/search"
        params = {"query": query, "tags": "story", "hitsPerPage": max_results}
        r = requests.get(algolia_url, params=params, timeout=15)
        r.raise_for_status()
        for hit in r.json().get("hits", []):
            results.append({
                "title": hit.get("title"),
                "url": hit.get("url") or f"https://news.ycombinator.com/item?id={hit['objectID']}",
                "score": hit.get("points"),
                "comments": hit.get("num_comments"),
                "source_platform": "hackernews",
            })
        if results:
            return results[:max_results]
    except Exception as e:
        logger.info(f"Hacker News failed, falling back: {e}")

    # ── Tier 3: Mastodon ─────────────────────────────────────────
    try:
        statuses = mastodon_search("mastodon.social", query,
                                    search_type="statuses", limit=max_results)
        for s in statuses:
            from bs4 import BeautifulSoup
            text = BeautifulSoup(s.get("content", ""), "lxml").get_text(strip=True)
            results.append({
                "title": text[:100],
                "url": s.get("url"),
                "score": s.get("favourites_count", 0),
                "source_platform": "mastodon",
            })
        if results:
            return results[:max_results]
    except Exception as e:
        logger.info(f"Mastodon failed, all sources exhausted: {e}")

    return []
```

### 8.3 各平台退避参数

| 平台 | 首次失败延迟 | 最大重试 | 退避因子 | 429 特殊处理 |
|------|-------------|----------|---------|-------------|
| Reddit | 2s | 3 | 3x | 遵守 `Retry-After` header |
| Hacker News | 1s | 3 | 2x | 无速率限制，但 Firebase 偶尔超时 |
| Mastodon | 2s | 2 | 2x | 因实例而异，检查 `X-RateLimit-Remaining` |
| GitHub | 2s | 3 | 2x | 检查 `X-RateLimit-Remaining` header |
| X/Twitter | 5s | 1 | — | 基本不重试，直接降级 |

### 8.4 GitHub 配额耗尽降级

```python
def github_with_fallback(query, per_page=10, token=None):
    """Search GitHub with graceful degradation on rate limit.

    If rate limited on search API, falls back to:
      1. Google site:github.com search
      2. Return cached partial results
    """
    # Check remaining quota first
    limits = github_rate_limit(token)
    search_remaining = limits["search"].get("remaining", 0)

    if search_remaining > 0:
        try:
            return github_search_repos(query, per_page=per_page, token=token)
        except requests.HTTPError:
            pass

    # Fallback: DuckDuckGo site search
    logger.info("GitHub search rate limited, falling back to web search")
    try:
        ddg_url = "https://html.duckduckgo.com/html/"
        params = {"q": f"{query} site:github.com"}
        r = requests.get(ddg_url, params=params, timeout=15,
                         headers={"User-Agent": "Mozilla/5.0"})
        from bs4 import BeautifulSoup
        soup = BeautifulSoup(r.text, "lxml")
        results = []
        for item in soup.find_all("div", class_="result"):
            title_el = item.find("a", class_="result__a")
            if title_el:
                results.append({
                    "title": title_el.get_text(strip=True),
                    "url": title_el.get("href"),
                    "source": "duckduckgo_fallback",
                })
        return results[:per_page]
    except Exception:
        return []
```

---

## 9. 依赖安装

```bash
pip install requests beautifulsoup4 lxml
```

| 包 | 用途 |
|---|------|
| `requests` | HTTP 请求（所有平台共用） |
| `beautifulsoup4` | HTML 解析（Mastodon、Twitter、HN 评论） |
| `lxml` | 解析后端 |

### 日志配置

**缓存策略：** 社交媒体 API 调用频繁极易触发封禁。建议使用共器 `cached_get`：

```python
import sys
sys.path.insert(0, os.path.expanduser("~/.lingtai-tui/utilities/_shared/scripts"))
from cached_get import cached_get

# 用法与 requests.get 一致，多一个 ttl 参数和 cache_name 参数
r = cached_get("https://www.reddit.com/r/python/hot.json",
               headers={"User-Agent": "LingTai/1.0"}, ttl=120, cache_name="social")
posts = r.json()["data"]["children"]
```

共器位于 `~/.lingtai-tui/utilities/_shared/scripts/cached_get.py`，特性：
- 原子写入（先书临时，成而后正名，断电不生残卷）
- 自动淘汰陈腐（>1 天自动删除，总量上限 200 条）
- 按 `cache_name` 隔离不同技能的缓存目录

**建议 TTL：**

| 平台 | TTL | 理由 |
|------|-----|------|
| Reddit 帖子列表 | 120s | 排序变化快 |
| Reddit 搜索 | 300s | 搜索结果相对稳定 |
| Hacker News | 60s | 实时讨论，变化极快 |
| Mastodon | 300s | 趋势和搜索结果较稳定 |
| GitHub 仓库搜索 | 3600s | 仓库数据变化慢 |
| GitHub Rate Limit | 60s | 仅用于检查配额 |

本技能代码中使用 `logging` 模块记录请求成败、降级触发与异常信息。调用前须先配置：

```python
import logging

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    datefmt="%H:%M:%S",
)
# 或为特定模块设更细粒度：
logging.getLogger("social_media").setLevel(logging.DEBUG)
```

**日志级别约定：**

| 级别 | 用途 |
|------|------|
| `DEBUG` | 完整请求 URL、响应状态码、解析字段详情 |
| `INFO` | 降级触发、平台切换、结果数量 |
| `WARNING` | 重试中、速率限制命中（含剩余配额） |
| `ERROR` | 全部平台失败、GitHub 配额耗尽、无法恢复的异常 |

---

## 10. 与主技能的关系

本技能是 **web-browsing-manual v3.0** 的子技能（sub-skill）。父技能提供了完整的网页浏览策略（七层渐进策略、学术 API、搜索引擎、反检测技术等），本技能专注于其中的社交媒体数据提取领域。

- 需要通用网页抓取 → 参见 `web-browsing-manual`
- 需要新闻获取和 RSS 处理 → 参见 `news-and-rss`
- 需要学术论文搜索 → 参见 `web-browsing-manual` 中的学术 API 章节
- 需要反检测和代理策略 → 参见 `web-browsing-manual` 中的反检测章节
