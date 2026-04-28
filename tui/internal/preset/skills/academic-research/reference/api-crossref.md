# CrossRef API Reference

## API 概述

CrossRef 是最大的学术 DOI 注册机构，其公开 API 提供论文元数据、基金信息与期刊检索。

- **基础端点**: `https://api.crossref.org`
- **认证**: 无需 API Key；附带 `User-Agent` 头可进入 Polite Pool 获得更高速率
- **响应格式**: JSON
- **协议**: HTTPS
- **适用场景**: 论文元数据检索、DOI 查询、基金追踪、出版趋势分析

### Polite Pool 设置

在请求头中加入联系方式即可进入 Polite Pool（速率从 ~10/s 提升至 ~50/s）：

```python
HEADERS = {"User-Agent": "MyApp/1.0 (mailto:your@email.com)"}
```

---

## 一、基础查询（Works 端点）

### 端点

```
GET https://api.crossref.org/works
```

### 查询参数

| 参数 | 说明 | 示例 |
|---|---|---|
| `query` | 全文搜索 | `query=attention is all you need` |
| `query.title` | 标题搜索 | `query.title=transformer` |
| `query.author` | 作者搜索 | `query.author=vaswani` |
| `query.bibliographic` | 书目信息搜索 | `query.bibliographic=deep learning NLP` |
| `rows` | 返回数量（默认 20，最大 100） | `rows=5` |
| `offset` | 分页偏移 | `offset=20` |
| `select` | 返回字段（逗号分隔） | `select=DOI,title,author,published-print` |
| `sort` | 排序字段 | `sort=published-print` |
| `order` | 排序方向：`asc` / `desc` | `order=desc` |
| `filter` | 高级过滤器（逗号分隔多条件） | `filter=from-pub-date:2020-01-01,type:journal-article` |

### 可选返回字段（select）

常用字段：`DOI`、`title`、`author`、`published-print`、`published-online`、`journal`、`publisher`、`type`、`volume`、`issue`、`page`、`abstract`、`citationCount`、`subject`、`ISSN`、`URL`、`link`、`funder`、`award`

### 高级过滤器（filter）

| 过滤器 | 说明 | 示例 |
|---|---|---|
| `from-pub-date` | 起始出版日期 | `from-pub-date:2020-01-01` |
| `until-pub-date` | 截止出版日期 | `until-pub-date:2024-12-31` |
| `type` | 文献类型 | `type:journal-article` |
| `issn` | 期刊 ISSN | `issn:0957-4174` |
| `prefix` | DOI 前缀（出版机构） | `prefix:10.1038` |
| `container-title` | 期刊名 | `container-title:Nature` |
| `funder` | 基金机构 DOI | `funder:10.13039/100000001` |
| `award` | 基金编号 | `award:CBET-1234567` |
| `has-abstract` | 是否有摘要 | `has-abstract:true` |
| `has-funder` | 是否有基金信息 | `has-funder:true` |

### 文献类型（type）

常见值：`journal-article`、`book-chapter`、`book`、`proceedings-article`、`dissertation`、`report`、`dataset`、`preprint`

### 代码示例

```python
import requests

HEADERS = {"User-Agent": "MyApp/1.0 (mailto:your@email.com)"}
BASE = "https://api.crossref.org"

def search_works(query, rows=5, select="DOI,title,author,published-print", **filters):
    """搜索 CrossRef 论文。

    Args:
        query: 搜索词
        rows: 返回数量 (1-100)
        select: 返回字段（逗号分隔）
        **filters: 额外过滤条件，如 type='journal-article', from_pub_date='2020-01-01'

    Returns:
        list[dict]: 论文列表
    """
    params = {"query": query, "rows": rows, "select": select}
    if filters:
        filter_parts = []
        for k, v in filters.items():
            key = k.replace("_", "-")
            filter_parts.append(f"{key}:{v}")
        params["filter"] = ",".join(filter_parts)

    r = requests.get(f"{BASE}/works", params=params, headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]["items"]

# 基础搜索
papers = search_works("transformer architecture", rows=3)
for p in papers:
    title = p.get("title", ["N/A"])[0]
    authors = ", ".join(a["family"] for a in p.get("author", []))
    doi = p.get("DOI", "N/A")
    print(f"DOI: {doi}")
    print(f"Title: {title}")
    print(f"Authors: {authors}")
    print()
```

### 返回格式

```json
{
  "status": "ok",
  "message-type": "work-list",
  "message": {
    "total-results": 1326968,
    "items-per-page": 5,
    "query": { "search-terms": "transformer", "start-index": 0 },
    "items": [
      {
        "DOI": "10.1007/978-3-031-84300-6_13",
        "title": ["Is Attention All You Need?"],
        "author": [
          { "given": "Patrick", "family": "Mineault", "sequence": "first" }
        ],
        "published-print": { "date-parts": [[2025, 6, 15]] },
        "type": "journal-article",
        "container-title": ["Nature Neuroscience"],
        "publisher": "Springer"
      }
    ]
  }
}
```

---

## 二、基金查询（Funders 端点）

### 端点

```
GET https://api.crossref.org/funders
```

### 参数

| 参数 | 说明 | 示例 |
|---|---|---|
| `query` | 搜索基金机构 | `query=NSF` |
| `rows` | 返回数量 | `rows=5` |

### 常见基金机构 DOI

| 基金机构 | DOI 标识 |
|---|---|
| NIH (美国国立卫生研究院) | `10.13039/100000002` |
| NSF (美国国家科学基金会) | `10.13039/100000001` |
| DOE (美国能源部) | `10.13039/100000015` |
| EU (欧盟) | `10.13039/501100000780` |
| Wellcome Trust | `10.13039/100004440` |
| DFG (德国研究联合会) | `10.13039/501100001659` |
| JSPS (日本学术振兴会) | `10.13039/501100001691` |
| NSFC (中国国家自然科学基金) | `10.13039/501100001809` |

### 代码示例

```python
def search_funders(query, rows=5):
    """搜索基金机构。"""
    params = {"query": query, "rows": rows}
    r = requests.get(f"{BASE}/funders", params=params, headers=HEADERS, timeout=10)
    r.raise_for_status()
    return r.json()["message"]["items"]

def get_funder_works(funder_doi, rows=5):
    """获取某基金机构资助的论文。

    Args:
        funder_doi: 基金机构 DOI（如 '10.13039/100000001' 为 NSF）
        rows: 返回数量
    """
    params = {
        "filter": f"funder:{funder_doi}",
        "rows": rows,
        "select": "DOI,title,author,published-print,funder,award",
        "sort": "published-print",
        "order": "desc",
    }
    r = requests.get(f"{BASE}/works", params=params, headers=HEADERS, timeout=10)
    r.raise_for_status()
    return r.json()["message"]["items"]

# 搜索基金机构
funders = search_funders("National Science Foundation")
for f in funders:
    print(f"{f['name']} (ID: {f['id']})")
    print(f"  Location: {f.get('location', 'N/A')}")

# 获取 NSF 资助的最新论文
nsf_works = get_funder_works("10.13039/100000001", rows=5)
for w in nsf_works:
    title = w.get("title", ["N/A"])[0]
    awards = [a.get("award", []) for a in w.get("funder", [])]
    flat_awards = [str(a) for sub in awards for a in sub]
    print(f"[NSF] {title}")
    if flat_awards:
        print(f"  Award: {', '.join(flat_awards[:3])}")
```

### 返回格式（Funders）

```json
{
  "message": {
    "items": [
      {
        "id": "100000001",
        "location": "United States",
        "name": "National Science Foundation",
        "alt-names": ["NSF"],
        "uri": "http://dx.doi.org/10.13039/100000001",
        "tokens": ["national", "science", "foundation"]
      }
    ]
  }
}
```

---

## 三、今日新增（日期过滤查询）

### 原理

通过 `from-pub-date` / `until-pub-date` 过滤器结合 `sort=published-print` 和 `order=desc`，实现最新论文追踪。

### 代码示例

```python
from datetime import date, timedelta

def get_recent_papers(topic=None, days=7, rows=20, funder=None, journal=None):
    """获取近期论文。

    Args:
        topic: 搜索关键词（可选）
        days: 回溯天数
        rows: 返回数量
        funder: 基金机构 DOI（可选）
        journal: 期刊名（可选）

    Returns:
        list[dict]: 论文列表，按出版日期降序
    """
    today = date.today()
    since = today - timedelta(days=days)
    filters = [f"from-pub-date:{since}"]
    if funder:
        filters.append(f"funder:{funder}")
    if journal:
        filters.append(f"container-title:{journal}")

    params = {
        "rows": rows,
        "filter": ",".join(filters),
        "sort": "published-print",
        "order": "desc",
        "select": "DOI,title,author,published-print,published-online",
    }
    if topic:
        params["query"] = topic

    r = requests.get(f"{BASE}/works", params=params, headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]["items"]

def daily_digest(topic, rows=20):
    """每日摘要：获取今日特定主题的论文。"""
    return get_recent_papers(topic=topic, days=1, rows=rows)

# 使用示例
# 最近 7 天关于 transformer 的论文
papers = get_recent_papers("transformer", days=7)
print(f"Found {len(papers)} recent papers on 'transformer'")
for p in papers[:5]:
    title = p.get("title", ["N/A"])[0]
    pub = p.get("published-print", p.get("published-online", {}))
    dp = pub.get("date-parts", [[None]])[0]
    date_str = "-".join(str(x) for x in dp if x is not None)
    print(f"  [{date_str}] {title[:80]}")

# 特定基金 + 特定期刊
nsf_nature = get_recent_papers(days=30, funder="10.13039/100000001", journal="Nature")

# 每日摘要
today_papers = daily_digest("large language model", rows=10)
```

### 高级过滤组合

```bash
# 特定日期范围 + 期刊类型
curl -s "https://api.crossref.org/works?filter=from-pub-date:2026-04-01,until-pub-date:2026-04-22,type:journal-article&rows=5&select=DOI,title,published-print"

# 有摘要的 Nature 论文
curl -s "https://api.crossref.org/works?filter=container-title:Nature,has-abstract:true&rows=3&select=DOI,title,abstract"
```

---

## 速率限制

| 池类型 | 速率 | 获取方式 |
|---|---|---|
| Public Pool | ~10 请求/秒 | 默认 |
| Polite Pool | ~50 请求/秒 | 请求头含 `User-Agent: AppName/Version (mailto:email)` |
| Plus Pool | ~200 请求/秒 | 需付费 CrossRef Plus 会员 |

**最佳实践**:
- 始终附带 `User-Agent` 头进入 Polite Pool
- 大量请求间加入 0.05–0.1 秒延迟
- 使用 `select` 参数只取所需字段，减小响应体积

## 失败处理

| HTTP 状态码 | 含义 | 处理方式 |
|---|---|---|
| 200 | 成功 | 正常解析 |
| 400 | 参数错误 | 检查 filter 语法与参数值 |
| 404 | DOI 不存在 | 确认 DOI 正确性 |
| 429 | 速率限制 | 退避重试，检查是否进入 Polite Pool |
| 503 | 服务暂不可用 | 指数退避重试 |

```python
import time

def crossref_get(url, params=None, retries=3):
    """带重试与退避的 CrossRef 请求。"""
    for attempt in range(retries):
        r = requests.get(url, params=params, headers=HEADERS, timeout=15)
        if r.status_code == 200:
            return r.json()
        elif r.status_code == 429:
            wait = min(30, 2 ** attempt * 2)
            print(f"Rate limited, waiting {wait}s...")
            time.sleep(wait)
        elif r.status_code >= 500:
            time.sleep(2 ** attempt)
        else:
            r.raise_for_status()
    raise Exception(f"CrossRef request failed after {retries} retries: {url}")
```

## 相关 API

- → 参见 [api-arxiv.md](api-arxiv.md) — 检索预印本论文（arXiv 论文通常先于此处发表）
- → 参见 [api-doi-resolver.md](api-doi-resolver.md) — 将单个 DOI 解析为完整元数据
