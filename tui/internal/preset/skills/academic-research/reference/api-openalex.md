# OpenAlex API Reference

OpenAlex 是微软学术图谱（MAG）的继任者，提供学术论文、概念分类和研究机构的全面元数据。完全免费，无需 API Key。

## API 概述

| 项目 | 说明 |
|---|---|
| 基础 URL | `https://api.openalex.org` |
| 认证 | 无需 API Key（可选 `mailto` 参数提高速率） |
| 速率限制 | ~10 请求/秒，1000 请求/天（无 key）；加 `mailto=you@example.com` 可放宽 |
| 响应格式 | JSON |
| 最佳用途 | 大规模论文发现、机构分析、主题建模、研究趋势映射 |

三个核心端点：

| 端点 | 用途 |
|---|---|
| `/works` | 论文搜索与元数据 |
| `/concepts` | 研究概念/主题分类 |
| `/institutions` | 研究机构查询 |

---

## 端点与参数

### 基础查询 — Works（论文搜索）

**端点**: `GET https://api.openalex.org/works`

| 参数 | 说明 | 示例 |
|---|---|---|
| `search` | 全文搜索 | `search=transformer architecture` |
| `search.title` | 仅搜索标题 | `search.title=attention` |
| `search.author` | 按作者名搜索 | `search.author=vaswani` |
| `filter` | 结构化过滤 | `filter=publication_year:2020` |
| `per-page` | 每页结果数（最大 200） | `per-page=10` |
| `page` | 页码 | `page=2` |
| `select` | 返回字段 | `select=title,authorships,publication_year` |
| `sort` | 排序字段 | `sort=cited_by_count:desc` |

**常用 filter 值**:

```
publication_year:2020                # 单年
publication_year:2017-2020           # 年份范围
authorships.author.id:A5101082644    # 按作者 ID
authorships.institutions.id:I145311948  # 按机构 ID
primary_location.source.id:S2764280280 # 按期刊
topics.id:T10038                     # 按主题
concepts.id:C119857082               # 按概念
is_oa:true                           # 仅开放获取
cited_by_count:1000+                 # 引用数筛选
```

**可选返回字段**: `id`, `title`, `display_name`, `authorships`, `publication_year`, `publication_date`, `type`, `open_access`, `cited_by_count`, `doi`, `primary_location`, `source`, `topics`, `classifications`, `keywords`, `funding`, `institutions`, `related_works`

### 概念分类 — Concepts（主题分类）

**端点**: `GET https://api.openalex.org/concepts`

| 参数 | 说明 | 示例 |
|---|---|---|
| `search` | 搜索概念 | `search=machine learning` |
| `per-page` | 每页结果数 | `per-page=5` |
| `filter` | 结构化过滤 | `filter=level:1` |
| `select` | 返回字段 | `select=display_name,level,works_count` |

**概念层级**（5 级分类体系）:

| 层级 | 含义 | 示例 |
|---|---|---|
| Level 0 | 大领域 | Computer Science |
| Level 1 | 子领域 | Machine learning |
| Level 2 | 更细子领域 | — |
| Level 3 | 具体主题 | — |
| Level 4 | 极具体主题 | — |

每个概念返回：`id`, `display_name`, `level`, `works_count`, `cited_by_count`, `description`, `ancestors`（上级概念链）

### 机构查询 — Institutions（研究机构）

**端点**: `GET https://api.openalex.org/institutions`

| 参数 | 说明 | 示例 |
|---|---|---|
| `search` | 搜索机构 | `search=Stanford` |
| `per-page` | 每页结果数 | `per-page=5` |
| `filter` | 结构化过滤 | `filter=country_code:US` |
| `select` | 返回字段 | `select=display_name,country_code,works_count` |

**机构返回字段**: `id`, `display_name`, `country_code`, `type`, `works_count`, `cited_by_count`, `summary_stats`（含 `h_index`, `2yr_mean_citedness`）

**按国家筛选机构**: `filter=country_code:US`

---

## 代码示例

### 论文搜索

```python
import requests

def search_openalex(query, per_page=10, select='title,authorships,publication_year,cited_by_count'):
    """搜索 OpenAlex 论文。"""
    url = "https://api.openalex.org/works"
    params = {"search": query, "per-page": per_page, "select": select}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

papers = search_openalex("attention is all you need", per_page=5)
for p in papers:
    print(f"Title: {p['title']}")
    print(f"Year: {p['publication_year']}, Cited by: {p['cited_by_count']}")
    for a in p.get('authorships', [])[:3]:
        inst = a['institutions'][0]['display_name'] if a.get('institutions') else 'N/A'
        print(f"  - {a['author']['display_name']} ({inst})")
    print("---")
```

### 按作者/机构筛选论文

```python
def get_works_by_author(author_id, per_page=10):
    """按 OpenAlex 作者 ID 获取论文列表。"""
    url = "https://api.openalex.org/works"
    params = {
        "filter": f"authorships.author.id:{author_id}",
        "per-page": per_page,
        "select": "title,publication_year,cited_by_count"
    }
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

def get_works_by_institution(inst_id, per_page=10):
    """按机构 ID 获取该机构的高引论文。"""
    url = "https://api.openalex.org/works"
    params = {
        "filter": f"authorships.institutions.id:{inst_id}",
        "per-page": per_page,
        "sort": "cited_by_count:desc",
        "select": "title,publication_year,cited_by_count,authorships"
    }
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]
```

### 概念搜索与论文关联

```python
def search_concepts(query, per_page=5):
    """搜索研究概念/主题。"""
    url = "https://api.openalex.org/concepts"
    params = {"search": query, "per-page": per_page}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

def get_concept_works(concept_id, per_page=10):
    """获取某个概念下的高引论文。"""
    url = "https://api.openalex.org/works"
    params = {
        "filter": f"concepts.id:{concept_id}",
        "per-page": per_page,
        "sort": "cited_by_count:desc",
        "select": "title,publication_year,cited_by_count"
    }
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

# 示例：搜索概念 → 获取其下论文
concepts = search_concepts("transformer architecture")
for c in concepts:
    print(f"[Level {c['level']}] {c['display_name']} — {c['works_count']:,} papers")

ml_id = "C119857082"  # Machine learning
papers = get_concept_works(ml_id)
for p in papers[:5]:
    print(f"{p['title'][:60]}... ({p['publication_year']})")
```

### 机构搜索与统计

```python
def search_institutions(query, per_page=5):
    """搜索研究机构。"""
    url = "https://api.openalex.org/institutions"
    params = {"search": query, "per-page": per_page}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

def get_institution_detail(inst_id):
    """获取机构详细信息。"""
    url = f"https://api.openalex.org/institutions/{inst_id}"
    r = requests.get(url, timeout=10)
    r.raise_for_status()
    return r.json()

# 示例
insts = search_institutions("Stanford University", per_page=3)
for i in insts:
    print(f"{i['display_name']} ({i['country_code']})")
    print(f"  Works: {i['works_count']:,}, Citations: {i['cited_by_count']:,}")
    stats = i.get('summary_stats', {})
    print(f"  h-index: {stats.get('h_index', 'N/A')}")
```

---

## 返回格式

所有端点返回统一的 JSON 结构：

```json
{
  "meta": {
    "count": 394212,
    "per_page": 10,
    "page": 1
  },
  "results": [
    {
      "id": "https://openalex.org/W123456789",
      "title": "...",
      "authorships": [
        {
          "author_position": "first",
          "author": {"id": "A5101082644", "display_name": "..."},
          "institutions": [{"display_name": "...", "country_code": "US"}]
        }
      ],
      "publication_year": 2022,
      "cited_by_count": 892,
      "doi": "https://doi.org/10...."
    }
  ]
}
```

单条记录查询（如 `/institutions/I97018004`）直接返回对象，无 `meta`/`results` 包装。

---

## 速率限制

| 场景 | 限制 |
|---|---|
| 无参数 | ~10 req/s, 1000 req/day |
| 加 `mailto=you@example.com` | 更宽的速率（推荐） |
| 返回状态码 | HTTP 429 = 超限 |

建议在请求参数中加 `mailto`：`?search=...&mailto=you@example.com`

---

## 失败处理

```python
import requests, time

def openalex_get(url, params, retries=3, delay=2):
    """带重试的 OpenAlex 请求。"""
    for attempt in range(retries):
        r = requests.get(url, params=params, timeout=15)
        if r.status_code == 200:
            return r.json()
        elif r.status_code == 429:
            wait = delay * (attempt + 1)
            print(f"Rate limited, waiting {wait}s...")
            time.sleep(wait)
        else:
            raise Exception(f"OpenAlex error {r.status_code}: {r.text[:200]}")
    raise Exception(f"Max retries exceeded for {url}")
```

---

## 相关 API

- → 参见 [api-semantic-scholar.md](api-semantic-scholar.md) — Semantic Scholar 论文/作者查询（引用网络更强）
- → 参见 [api-core.md](api-core.md) — CORE 开放获取论文全文下载
- → 参见 [api-crossref.md](api-crossref.md) — CrossRef DOI 元数据查询
