# CORE API Reference

CORE 是一个免费的学术资源聚合平台，索引全球 2 亿+ 篇开放获取论文，提供 PDF 直链下载。

## API 概述

| 项目 | 说明 |
|---|---|
| 基础 URL | `https://api.core.ac.uk/v3` |
| 认证 | 无 API Key 可用基本功能；有 Key 可解锁更多 |
| 速率限制 | ~100 请求/秒 |
| 响应格式 | JSON |
| 最佳用途 | 查找免费全文论文、机构库内容、PDF 下载 |

核心端点：

| 端点 | 用途 |
|---|---|
| `POST /search/works` | 搜索论文 |
| `GET /works/{id}` | 获取单篇论文详情（含 PDF 下载 URL） |

---

## 端点与参数

### 搜索论文

**端点**: `POST https://api.core.ac.uk/v3/search/works`

请求体为 JSON：

| 参数 | 类型 | 说明 | 示例 |
|---|---|---|---|
| `q` | string | 搜索查询 | `"machine learning"` |
| `limit` | int | 最大结果数 | `10` |
| `offset` | int | 分页偏移 | `0` |

### 获取论文详情

**端点**: `GET https://api.core.ac.uk/v3/works/{workId}`

返回完整元数据，包括 `downloadUrl`（PDF 直链）。

---

## 代码示例

### 搜索论文

```python
import requests

def search_core(query, limit=10, offset=0):
    """搜索 CORE 学术论文。

    Args:
        query: 搜索查询字符串
        limit: 最大返回结果数（默认 10）
        offset: 分页偏移（默认 0）

    Returns:
        dict: 包含 totalHits 和 results 列表
    """
    url = "https://api.core.ac.uk/v3/search/works"
    payload = {"q": query, "limit": limit, "offset": offset}
    r = requests.post(url, json=payload, timeout=10)
    r.raise_for_status()
    return r.json()

# 示例
results = search_core("transformer architecture", limit=5)
print(f"Total hits: {results['totalHits']}")

for paper in results['results']:
    print(f"\nTitle: {paper['title']}")
    print(f"Authors: {[a['name'] for a in paper.get('authors', [])]}")
    print(f"Year: {paper.get('yearPublished', 'N/A')}")
    print(f"DOI: {paper.get('doi', 'N/A')}")
    if paper.get('downloadUrl'):
        print(f"PDF: {paper['downloadUrl']}")
```

### 获取论文详情并下载 PDF

```python
def get_core_work(work_id):
    """获取单篇论文完整信息。"""
    url = f"https://api.core.ac.uk/v3/works/{work_id}"
    r = requests.get(url, timeout=10)
    r.raise_for_status()
    return r.json()

def download_core_pdf(work_id, output_path):
    """下载论文 PDF。

    Args:
        work_id: CORE 论文 ID
        output_path: 本地保存路径

    Returns:
        str: 保存路径（成功时）；None（无 PDF 时）
    """
    data = get_core_work(work_id)
    if data.get('downloadUrl'):
        pdf = requests.get(data['downloadUrl'], timeout=30)
        with open(output_path, 'wb') as f:
            f.write(pdf.content)
        return output_path
    return None
```

### 带过滤的搜索

```python
def search_core_advanced(query, limit=10, year_from=None, year_to=None):
    """高级搜索（使用查询语法）。"""
    q = query
    if year_from or year_to:
        # CORE 支持在查询中嵌入年份范围
        q = f"{query} yearPublished>={year_from or 1900} yearPublished<={year_to or 2100}"

    url = "https://api.core.ac.uk/v3/search/works"
    payload = {"q": q, "limit": limit, "offset": 0}
    r = requests.post(url, json=payload, timeout=10)
    r.raise_for_status()
    return r.json()
```

---

## 返回格式

### 搜索响应

```json
{
  "totalHits": 12345,
  "results": [
    {
      "id": 12345678,
      "title": "Attention Is All You Need",
      "authors": [
        {"name": "Ashish Vaswani"},
        {"name": "Noam Shazeer"}
      ],
      "yearPublished": 2017,
      "doi": "10.5555/3295222.3295349",
      "downloadUrl": "https://core.ac.uk/download/pdf/...",
      "abstract": "The dominant sequence transduction models...",
      "publisher": "Curran Associates",
      "citationCount": 50000,
      "sourceFulltextUrls": ["https://arxiv.org/pdf/1706.03762"]
    }
  ]
}
```

### 关键响应字段

| 字段 | 说明 |
|---|---|
| `totalHits` | 总匹配论文数 |
| `results[].id` | CORE 论文 ID |
| `results[].title` | 论文标题 |
| `results[].authors` | 作者列表（`[{name: "..."}]`） |
| `results[].yearPublished` | 发表年份 |
| `results[].doi` | DOI |
| `results[].downloadUrl` | PDF 直链下载 URL |
| `results[].abstract` | 摘要 |
| `results[].publisher` | 出版商 |
| `results[].citationCount` | 引用数 |
| `results[].sourceFulltextUrls` | 其他全文 URL（如 arXiv） |

---

## 速率限制

| 场景 | 限制 |
|---|---|
| 无 API Key | ~100 req/s |
| 有 API Key | 更高配额 |

CORE 的速率限制相对宽松，一般无需特殊处理。

---

## 失败处理

```python
import requests, time

def core_search_safe(query, limit=10, retries=3, delay=2):
    """带重试的 CORE 搜索。"""
    url = "https://api.core.ac.uk/v3/search/works"
    payload = {"q": query, "limit": limit, "offset": 0}
    for attempt in range(retries):
        try:
            r = requests.post(url, json=payload, timeout=15)
            if r.status_code == 200:
                return r.json()
            elif r.status_code == 429:
                time.sleep(delay * (attempt + 1))
            else:
                raise Exception(f"CORE error {r.status_code}: {r.text[:200]}")
        except requests.exceptions.Timeout:
            time.sleep(delay)
    raise Exception(f"Max retries exceeded for CORE search: {query}")
```

---

## 相关 API

- → 参见 [api-openalex.md](api-openalex.md) — OpenAlex 论文/概念/机构查询（元数据更丰富）
- → 参见 [api-semantic-scholar.md](api-semantic-scholar.md) — Semantic Scholar 论文/作者查询（引用网络更强）
- → 参见 [api-crossref.md](api-crossref.md) — CrossRef DOI 元数据查询
