# DOI Resolver API Reference

## API 概述

通过 CrossRef 的 Works 端点，将 DOI（Digital Object Identifier）解析为完整的论文元数据。这是从 DOI 获取引用信息最直接的方式。

- **端点**: `https://api.crossref.org/works/{DOI}`
- **重定向端点**: `https://doi.org/{DOI}` → 出版商页面
- **认证**: 无需 API Key；Polite Pool 同样适用
- **响应格式**: JSON
- **典型响应时间**: < 200ms
- **适用场景**: DOI → 引用信息、批量解析 DOI 列表、获取论文全文链接

## 端点与参数

### 单篇 DOI 解析

| 项目 | 说明 |
|---|---|
| 端点 | `GET https://api.crossref.org/works/{DOI}` |
| 路径参数 | `DOI` — DOI 字符串，如 `10.1038/nature12373` |
| 请求头 | 推荐 `User-Agent: AppName/Version (mailto:email)` |
| 响应 | JSON，`message` 字段含完整元数据 |

### DOI URL 重定向

| 项目 | 说明 |
|---|---|
| 端点 | `https://doi.org/{DOI}` |
| 用途 | 跟随重定向获取出版商页面 URL |
| 方法 | `HEAD` 请求 + `allow_redirects=True` |

### select 参数

解析单篇 DOI 时也可使用 `select` 参数限制返回字段（但通常直接取全部字段即可）。

## 代码示例

### 基础解析

```python
import requests

HEADERS = {"User-Agent": "MyApp/1.0 (mailto:your@email.com)"}

def resolve_doi(doi):
    """将 DOI 解析为完整论文元数据。

    Args:
        doi: DOI 字符串，如 '10.1038/nature12373'

    Returns:
        dict: 包含 title, authors, journal, publisher 等字段的元数据字典
    """
    url = f"https://api.crossref.org/works/{doi}"
    r = requests.get(url, headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]

# 使用示例
paper = resolve_doi("10.1038/nature12373")
print(f"Title: {paper.get('title', ['N/A'])[0]}")
print(f"Journal: {paper.get('container-title', ['N/A'])[0]}")
print(f"Publisher: {paper.get('publisher', 'N/A')}")
print(f"Type: {paper.get('type', 'N/A')}")

year = paper.get("published-print", paper.get("published-online", {}))
year_str = year.get("date-parts", [[None]])[0][0] if year else "N/A"
print(f"Year: {year_str}")

authors = paper.get("author", [])
author_names = [f"{a.get('given', '')} {a.get('family', '')}" for a in authors]
print(f"Authors: {', '.join(author_names[:5])}" + (" et al." if len(authors) > 5 else ""))
```

### 获取出版商 URL

```python
def get_publisher_url(doi):
    """跟随 DOI 重定向获取出版商页面 URL。

    Args:
        doi: DOI 字符串

    Returns:
        str: 出版商页面 URL
    """
    r = requests.head(f"https://doi.org/{doi}", allow_redirects=True, timeout=10)
    return r.url

# 使用示例
url = get_publisher_url("10.1038/nature12373")
print(f"Publisher URL: {url}")
```

### 批量解析 DOI

```python
import time

def resolve_dois(doi_list, delay=0.1):
    """批量解析 DOI 列表。

    Args:
        doi_list: DOI 字符串列表
        delay: 请求间隔（秒），避免触发速率限制

    Returns:
        list[dict]: 每个元素为 {'doi': ..., 'metadata': ...} 或 {'doi': ..., 'error': ...}
    """
    results = []
    for doi in doi_list:
        try:
            meta = resolve_doi(doi)
            results.append({"doi": doi, "metadata": meta})
        except requests.HTTPError as e:
            results.append({"doi": doi, "error": str(e)})
        time.sleep(delay)
    return results

# 使用示例
dois = [
    "10.1038/nature12373",
    "10.1126/science.1248506",
    "10.1016/j.cell.2014.05.010",
]
resolved = resolve_dois(dois)
for item in resolved:
    if "metadata" in item:
        title = item["metadata"].get("title", ["N/A"])[0]
        print(f"✓ {item['doi']}: {title}")
    else:
        print(f"✗ {item['doi']}: {item['error']}")
```

### 提取结构化引用

```python
def format_citation(doi, style="apa"):
    """从 DOI 元数据生成结构化引用字符串。

    Args:
        doi: DOI 字符串
        style: 引用格式 ('apa', 'mla', 'brief')

    Returns:
        str: 格式化引用字符串
    """
    paper = resolve_doi(doi)
    title = paper.get("title", ["N/A"])[0]
    authors = paper.get("author", [])
    journal = paper.get("container-title", ["N/A"])[0]
    year_info = paper.get("published-print", paper.get("published-online", {}))
    year = year_info.get("date-parts", [[None]])[0][0] if year_info else "N/A"
    volume = paper.get("volume", "")
    issue = paper.get("issue", "")
    pages = paper.get("page", "")

    if style == "apa":
        auth_str = ", ".join(
            f"{a.get('family', '')}, {a.get('given', '')[0]}." for a in authors[:6]
        )
        if len(authors) > 6:
            auth_str += " et al."
        vi_str = f"{volume}({issue})" if issue else volume
        return f"{auth_str} ({year}). {title}. {journal}, {vi_str}, {pages}. https://doi.org/{doi}"

    elif style == "brief":
        first_author = authors[0].get("family", "Unknown") if authors else "Unknown"
        return f"{first_author} et al. ({year}). {title}. {journal}."

    return f"{title} ({year}). {journal}. DOI: {doi}"

# 使用示例
print(format_citation("10.1038/nature12373", style="apa"))
# Vaswani, A., ... (2017). Attention Is All You Need. Nature, 498(7453), 376-379.

print(format_citation("10.1038/nature12373", style="brief"))
# Vaswani et al. (2017). Attention Is All You Need. Nature.
```

## 返回格式

### 完整响应结构

```json
{
  "status": "ok",
  "message-type": "work",
  "message": {
    "DOI": "10.1038/nature12373",
    "title": ["Attention Is All You Need"],
    "author": [
      {"given": "Ashish", "family": "Vaswani", "sequence": "first", "affiliation": []}
    ],
    "published-print": {"date-parts": [[2017, 12, 6]]},
    "published-online": {"date-parts": [[2017, 6, 12]]},
    "container-title": ["Nature"],
    "publisher": "Springer Nature",
    "type": "journal-article",
    "volume": "576",
    "issue": "7467",
    "page": "376-379",
    "abstract": "...",
    "reference-count": 45,
    "references-count": 45,
    "is-referenced-by-count": 95000,
    "subject": ["Computer Science"],
    "ISSN": ["0028-0836", "1476-4687"],
    "URL": "http://dx.doi.org/10.1038/nature12373",
    "link": [
      {"URL": "https://doi.org/10.1038/nature12373", "content-type": "text/html"},
      {"URL": "...pdf", "content-type": "application/pdf"}
    ],
    "license": [
      {"URL": "...", "content-version": "vor", "content-type": "text/html"}
    ],
    "funder": [
      {"DOI": "10.13039/100000001", "name": "National Science Foundation", "award": ["1234567"]}
    ]
  }
}
```

### 关键元数据字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `title[0]` | string | 论文标题 |
| `container-title[0]` | string | 期刊/书籍名称 |
| `author[].given` | string | 作者名 |
| `author[].family` | string | 作者姓 |
| `author[].sequence` | string | 作者排序：`first` / `additional` |
| `published-print.date-parts` | array | 纸质出版日期 [[年, 月, 日]] |
| `published-online.date-parts` | array | 在线出版日期 [[年, 月, 日]] |
| `publisher` | string | 出版机构 |
| `type` | string | 文献类型（journal-article 等） |
| `volume` | string | 卷号 |
| `issue` | string | 期号 |
| `page` | string | 页码范围 |
| `DOI` | string | DOI 标识 |
| `abstract` | string | 摘要（部分论文可能缺失） |
| `reference-count` | number | 参考文献数量 |
| `is-referenced-by-count` | number | 被引用次数（CrossRef 索引内） |
| `subject` | array[string] | 学科分类 |
| `ISSN` | array[string] | 期刊 ISSN |
| `URL` | string | CrossRef 页面链接 |
| `link[]` | array | 全文链接列表 |
| `license[]` | array | 许可证信息 |
| `funder[]` | array | 基金信息，含 `DOI`、`name`、`award` |

## 速率限制

| 池类型 | 速率 | 获取方式 |
|---|---|---|
| Public Pool | ~10 请求/秒 | 默认 |
| Polite Pool | ~50 请求/秒 | 请求头含 `User-Agent` |

**批量解析最佳实践**:
- 请求间隔 ≥ 0.1 秒（Polite Pool）或 ≥ 0.2 秒（Public Pool）
- 使用 `try/except` 捕获单条失败，不中断整体批处理
- 大批量（>100 DOI）考虑分批，每批之间加入更长间隔

## 失败处理

| 场景 | HTTP 状态码 | 处理方式 |
|---|---|---|
| DOI 不存在 | 404 | 确认 DOI 拼写；部分旧 DOI 可能未在 CrossRef 注册 |
| 速率限制 | 429 | 退避重试；检查 User-Agent 头 |
| 服务暂不可用 | 503 | 指数退避重试 |
| DOI 格式错误 | 400 | 检查 DOI 格式（应为 `10.xxxx/yyyy`） |
| 出版商无响应 | 超时 | 增大 timeout 至 15-30 秒 |
| 元数据不完整 | 200 但字段缺失 | 使用 `.get()` 防御性访问，部分字段非必有 |

```python
def resolve_doi_safe(doi):
    """安全解析 DOI，返回统一结构。"""
    try:
        paper = resolve_doi(doi)
        return {
            "doi": doi,
            "title": paper.get("title", [None])[0],
            "journal": paper.get("container-title", [None])[0],
            "year": (paper.get("published-print") or paper.get("published-online") or {}).get("date-parts", [[None]])[0][0],
            "authors": [f"{a.get('given', '')} {a.get('family', '')}" for a in paper.get("author", [])],
            "citations": paper.get("is-referenced-by-count", 0),
            "type": paper.get("type"),
            "success": True,
        }
    except requests.HTTPError as e:
        return {"doi": doi, "error": f"HTTP {e.response.status_code}", "success": False}
    except Exception as e:
        return {"doi": doi, "error": str(e), "success": False}
```

## 相关 API

- → 参见 [api-crossref.md](api-crossref.md) — 搜索论文、基金查询、日期过滤（Works 端点的完整用法）
- → 参见 [api-arxiv.md](api-arxiv.md) — 检索预印本论文（arXiv 论文通常无 DOI）
- DOI 解析是 CrossRef Works 端点的单篇特化；批量搜索需求应使用 Works 搜索端点
