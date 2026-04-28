# Semantic Scholar API Reference

Semantic Scholar Graph API 提供学术论文搜索、引用网络、作者画像等功能。免费层可用，API Key 可大幅提升配额。

## API 概述

| 项目 | 说明 |
|---|---|
| 基础 URL | `https://api.semanticscholar.org/graph/v1` |
| 认证 | 无 Key 可用（100 req/day/IP）；加 API Key → 1000 req/day（免费） |
| 速率限制 | 无 Key: ~5 成功请求/分钟/IP；有 Key: 显著放宽 |
| 响应格式 | JSON |
| Python SDK | `pip install semanticscholar` |
| 最佳用途 | 引用网络分析、作者画像、论文元数据检索 |

核心端点：

| 端点 | 用途 |
|---|---|
| `GET /paper/search` | 论文搜索 |
| `GET /paper/{paperId}` | 论文详情 |
| `GET /paper/{paperId}/citations` | 获取引用该论文的论文 |
| `GET /paper/{paperId}/references` | 获取该论文引用的论文 |
| `GET /author/search` | 搜索作者 |
| `GET /author/{authorId}` | 作者详情 + 论文列表 |

---

## 端点与参数

### 论文查询

#### 搜索论文

**端点**: `GET /paper/search`

| 参数 | 说明 | 示例 |
|---|---|---|
| `query` | 搜索查询字符串 | `query=attention is all you need` |
| `limit` | 最大结果数（默认 100） | `limit=10` |
| `offset` | 分页偏移 | `offset=10` |
| `fields` | 返回字段（逗号分隔） | `fields=title,authors,year` |
| `year` | 年份筛选 | `year=2020` 或 `year=2018-2022` |
| `publicationTypes` | 发表类型 | `publicationTypes=JournalArticle` |
| `openAccessPdf` | 仅返回有 OA PDF 的 | `openAccessPdf=true` |
| `venue` | 发表场所 | `venue=NeurIPS` |
| `fieldsOfStudy` | 研究领域 | `fieldsOfStudy=Computer Science` |
| `minCitationCount` | 最低引用数 | `minCitationCount=100` |
| `sort` | 排序 | `sort=citationCount:desc` |

**常用 fields 值**: `title`, `authors`, `year`, `abstract`, `venue`, `citationCount`, `referenceCount`, `url`, `paperId`, `externalIds`, `openAccessPdf`, `fieldsOfStudy`, `publicationTypes`, `journal`

嵌套字段: `authors.authorId`, `authors.name`, `authors.url`

#### 论文详情

**端点**: `GET /paper/{paperId}`

`paperId` 可以是：
- Semantic Scholar ID（40 字符 hash）
- DOI: `DOI:10.1234/...`
- ArXiv: `ArXiv:2106.15928`
- PMID, ACL, URL 等

#### 引用与参考文献

| 端点 | 说明 |
|---|---|
| `GET /paper/{paperId}/citations` | 获取引用了此论文的论文列表 |
| `GET /paper/{paperId}/references` | 获取此论文引用的论文列表 |

两者均支持 `limit`, `offset`, `fields` 参数。返回格式中论文嵌套在 `citingPaper` 或 `citedPaper` 键下。

### 作者查询

#### 搜索作者

**端点**: `GET /author/search`

| 参数 | 说明 | 示例 |
|---|---|---|
| `query` | 作者姓名 | `query=yoshua bengio` |
| `limit` | 最大结果数 | `limit=5` |
| `fields` | 返回字段 | `fields=name,hIndex,citationCount` |

#### 作者详情与论文

**端点**: `GET /author/{authorId}`

| 参数 | 说明 | 示例 |
|---|---|---|
| `fields` | 返回字段 | `fields=name,hIndex,citationCount,papers` |

返回 `papers` 数组包含该作者的论文列表（每条含 `paperId`, `title` 等）。

---

## 代码示例

### 论文搜索（直接 HTTP）

```python
import requests, time

def search_papers(query, limit=10, fields=None):
    """搜索 Semantic Scholar 论文。"""
    url = "https://api.semanticscholar.org/graph/v1/paper/search"
    params = {"query": query, "limit": limit}
    if fields:
        params["fields"] = ",".join(fields)
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()

results = search_papers("deep learning", limit=5,
                        fields=["title", "authors", "year", "abstract"])
for p in results.get("data", []):
    print(f"Title: {p['title']}")
    print(f"Year: {p.get('year')}, Authors: {[a['name'] for a in p.get('authors', [])[:3]]}")
    print("---")
```

### 论文搜索（Python SDK）

```python
from semanticscholar import SemanticScholar

# 无 key = 有限配额
ss = SemanticScholar()
# 有 key:
# ss = SemanticScholar(api_key='your-key')

results = ss.search_paper(
    'attention is all you need',
    limit=5,
    fields=['title', 'authors', 'year', 'abstract']
)

for paper in results[:5]:
    print(f"Title: {paper.title}")
    print(f"Year: {paper.year}")
    print(f"Authors: {[a.name for a in paper.authors]}")
    print("---")
```

SDK `search_paper` 完整签名：

```python
search_paper(
    query: str,
    year: str = None,                # '2020' 或 '2018-2022'
    publication_types: list = None,
    open_access_pdf: bool = None,
    venue: list = None,
    fields_of_study: list = None,
    fields: list = None,
    publication_date_or_year: str = None,
    min_citation_count: int = None,
    limit: int = 100,
    bulk: bool = False,
    sort: str = None,                # 'citationCount:desc'
    match_title: bool = False
)
```

### 获取引用与参考文献

```python
def get_citations(paper_id, limit=5, fields=None):
    """获取引用了指定论文的论文。"""
    url = f"https://api.semanticscholar.org/graph/v1/paper/{paper_id}/citations"
    params = {"limit": limit}
    if fields:
        params["fields"] = ",".join(fields)
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()

def get_references(paper_id, limit=5, fields=None):
    """获取指定论文引用的参考文献。"""
    url = f"https://api.semanticscholar.org/graph/v1/paper/{paper_id}/references"
    params = {"limit": limit}
    if fields:
        params["fields"] = ",".join(fields)
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()
```

### 作者搜索与画像

```python
def search_authors(query, limit=5):
    """搜索作者。"""
    url = "https://api.semanticscholar.org/graph/v1/author/search"
    params = {"query": query, "limit": limit, "fields": "name,hIndex,citationCount"}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["data"]

def get_author_profile(author_id):
    """获取作者详情及论文列表。"""
    url = f"https://api.semanticscholar.org/graph/v1/author/{author_id}"
    params = {"fields": "name,hIndex,citationCount,papers"}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()

# 示例
authors = search_authors("yoshua bengio")
for a in authors:
    print(f"{a['name']} — h-index: {a.get('hIndex', 'N/A')}, citations: {a.get('citationCount', 'N/A')}")

if authors:
    profile = get_author_profile(authors[0]["authorId"])
    print(f"\nTop papers by {profile['name']}:")
    for p in profile.get('papers', [])[:5]:
        print(f"  {p.get('title', 'N/A')[:70]}")
```

---

## 返回格式

### 论文搜索响应

```json
{
  "total": 8013996,
  "offset": 0,
  "next": 10,
  "data": [
    {
      "paperId": "3c8a4565...",
      "title": "PyTorch: An Imperative Style, High-Performance Deep Learning Library",
      "year": 2019,
      "authors": [
        {"authorId": "3407277", "name": "Adam Paszke"}
      ],
      "abstract": "...",
      "citationCount": 12000
    }
  ]
}
```

### 引用响应

```json
{
  "data": [
    {
      "citingPaper": {
        "paperId": "...",
        "title": "Bridging local and global representations...",
        "year": 2026,
        "authors": [...]
      }
    }
  ]
}
```

### 作者搜索响应

```json
{
  "total": 5,
  "offset": 0,
  "data": [
    {
      "authorId": "1751762",
      "name": "Yoshua Bengio",
      "hIndex": 187,
      "citationCount": 523456
    }
  ]
}
```

---

## 速率限制

| 场景 | 配额 | 说明 |
|---|---|---|
| 无 API Key | ~100 req/day/IP | 实际约 5 成功请求/分钟 |
| 有免费 API Key | 1000 req/day | 更稳定的速率 |
| 付费层级 | 更高 | 按需购买 |

**超限响应**: HTTP 429 Too Many Requests

**最佳实践**: 请求间至少等待 12 秒（无 key）；有 key 可连续请求。

---

## 失败处理

```python
import requests, time

def safe_semantic_scholar_get(url, params, retries=3, delay=12):
    """带重试的 Semantic Scholar 请求（无 key 模式）。"""
    for attempt in range(retries):
        try:
            r = requests.get(url, params=params, timeout=15)
            if r.status_code == 200:
                return r.json()
            elif r.status_code == 429:
                wait = delay * (attempt + 1)
                print(f"Rate limited (429), waiting {wait}s... (attempt {attempt+1}/{retries})")
                time.sleep(wait)
            else:
                raise Exception(f"S2 error {r.status_code}: {r.text[:200]}")
        except requests.exceptions.Timeout:
            print(f"Timeout, retrying... (attempt {attempt+1}/{retries})")
            time.sleep(delay)
    raise Exception(f"Max retries exceeded for {url}")
```

---

## 相关 API

- → 参见 [api-openalex.md](api-openalex.md) — OpenAlex 论文/概念/机构查询（无 key 限制更宽）
- → 参见 [api-core.md](api-core.md) — CORE 开放获取论文全文下载
- → 参见 [api-crossref.md](api-crossref.md) — CrossRef DOI 元数据查询
