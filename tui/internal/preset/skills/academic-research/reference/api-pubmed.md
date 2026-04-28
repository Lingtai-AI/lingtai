# PubMed API Reference

## API 概述

PubMed 提供 NCBI E-utilities API，用于检索生物医学文献。完全免费、无需 API key，适合生物医学、生命科学、医学研究领域的文献检索。返回 PubMed ID（PMID）作为文献唯一标识。

| 属性 | 说明 |
|------|------|
| 基础 URL | `https://eutils.ncbi.nlm.nih.gov/entrez/eutils/` |
| 认证 | 无需 API key（可选 `tool`/`email` 参数用于追踪） |
| 速率限制 | ~3 请求/秒（无 API key）；有 key 可达 10 次/秒 |
| 返回格式 | JSON 或 XML（`retmode` 参数控制） |
| 文献 ID | PMID（PubMed ID） |

## 端点与参数

### esearch — 搜索文献

| 参数 | 说明 | 示例 |
|------|------|------|
| `db` | 数据库 | `pubmed`、`pmc`、`books` |
| `term` | 搜索查询 | `transformer architecture` |
| `retmax` | 最大返回数（默认 20） | `10` |
| `retmode` | 返回格式 | `json` 或 `xml` |
| `sort` | 排序字段 | `relevance`、`pub_date` |
| `field` | 搜索字段限定 | `tiab`（标题+摘要） |
| `retstart` | 分页偏移 | `0`、`20` |

**搜索字段速查**：

| 代码 | 字段 | 示例查询 |
|------|------|----------|
| `ti` | 标题 | `cancer[ti]` |
| `ab` | 摘要 | `treatment[ab]` |
| `tiab` | 标题+摘要 | `transformer[tiab]` |
| `au` | 作者 | `vaswani[au]` |
| `dp` | 日期 | `2020:2024[dp]` |
| `mh` | MeSH 主题词 | `neural networks[mh]` |
| `mb` | MeSH 主题词（主要） | `genomics[mb]` |

**布尔组合**：使用 `AND`、`OR`、`NOT` 连接，如 `cancer[ti] AND review[pt] AND 2023:2024[dp]`。

### esummary — 文献摘要

| 参数 | 说明 | 示例 |
|------|------|------|
| `db` | 数据库 | `pubmed` |
| `id` | PMID（逗号分隔支持批量） | `42018049` 或 `42018049,42014737` |
| `retmode` | 返回格式 | `json` |

### efetch — 完整记录

| 参数 | 说明 | 示例 |
|------|------|------|
| `db` | 数据库 | `pubmed` |
| `id` | PMID | `42018049` |
| `rettype` | 返回类型 | `abstract`、`full`、`medline`、`uilist` |
| `retmode` | 返回格式 | `text`（`rettype=abstract` 时）、`xml`（`rettype=full` 时） |

## 代码示例

### 搜索并获取文献详情

```python
import requests
import time

BASE = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"

def search_pubmed(query, retmax=5, field=None, sort="relevance"):
    """搜索 PubMed，返回 PMID 列表。"""
    params = {
        "db": "pubmed",
        "term": query,
        "retmax": retmax,
        "retmode": "json",
        "sort": sort,
    }
    if field:
        params["field"] = field
    r = requests.get(f"{BASE}/esearch.fcgi", params=params, timeout=10)
    r.raise_for_status()
    return r.json()["esearchresult"]["idlist"]

def get_summaries(pmids):
    """批量获取文献摘要信息。"""
    params = {
        "db": "pubmed",
        "id": ",".join(pmids),
        "retmode": "json",
    }
    r = requests.get(f"{BASE}/esummary.fcgi", params=params, timeout=10)
    r.raise_for_status()
    data = r.json()["result"]
    return {pmid: data[pmid] for pmid in data.get("uids", [])}

def fetch_abstract(pmid):
    """获取单篇文献的摘要全文。"""
    params = {
        "db": "pubmed",
        "id": pmid,
        "rettype": "abstract",
        "retmode": "text",
    }
    r = requests.get(f"{BASE}/efetch.fcgi", params=params, timeout=10)
    r.raise_for_status()
    return r.text

# 使用示例
ids = search_pubmed("transformer architecture in genomics", retmax=3)
summaries = get_summaries(ids)
for pmid, art in summaries.items():
    print(f"PMID:  {pmid}")
    print(f"标题:  {art.get('title', 'N/A')}")
    print(f"期刊:  {art.get('source', 'N/A')}")
    print(f"作者:  {[a['name'] for a in art.get('authors', [])[:3]]}")
    print(f"日期:  {art.get('pubdate', 'N/A')}")
    print("---")
    time.sleep(0.34)  # 速率限制：~3 请求/秒
```

### 按作者搜索

```python
ids = search_pubmed("vaswani[au]", retmax=5)
```

### 按 MeSH 主题词 + 日期范围搜索

```python
ids = search_pubmed(
    "neural networks[mh] AND genomics[mb] AND 2020:2024[dp]",
    retmax=10
)
```

### 直接 curl 示例

```bash
# 搜索
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/e_utils/esearch.fcgi?db=pubmed&term=transformer+architecture&retmax=3&retmode=json"

# 获取摘要
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/e_utils/esummary.fcgi?db=pubmed&id=42018049&retmode=json"

# 获取全文摘要
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/e_utils/efetch.fcgi?db=pubmed&id=42018049&rettype=abstract&retmode=text"
```

## 返回格式

### esearch 响应

```json
{
  "esearchresult": {
    "count": "5095",
    "retmax": "3",
    "retstart": "0",
    "idlist": ["42018049", "42014737", "42014555"],
    "querytranslation": "transformer[All Fields] AND architecture[All Fields]"
  }
}
```

### esummary 响应

```json
{
  "result": {
    "uids": ["42018049"],
    "42018049": {
      "uid": "42018049",
      "title": "Deep learning approaches for...",
      "source": "Nat Methods",
      "authors": [{"name": "Smith J"}, {"name": "Lee K"}],
      "pubdate": "2025 Jan",
      "fulljournalname": "Nature methods",
      "elocationid": "doi:10.1038/s41592-025-xxxxx"
    }
  }
}
```

### efetch 响应（rettype=abstract, retmode=text）

```
1. Author A, Author B.
Title of the article.
Journal Name. 2025 Jan;30(1):1-10.

Abstract text here...
```

## 速率限制

| 场景 | 限制 |
|------|------|
| 无 API key | ~3 请求/秒 |
| 有 API key（`api_key` 参数） | 10 请求/秒 |
| 批量请求 | 每次最多 200 个 PMID（esummary/efetch） |

**建议**：
- 在循环中加 `time.sleep(0.34)` 控制无 key 场景的速率
- 大量检索时使用 `retstart` 分页，每页 `retmax=100`

## 失败处理

| 场景 | 处理方式 |
|------|----------|
| HTTP 429（Too Many Requests） | 等待后重试，增加 `time.sleep` 间隔 |
| 空结果 `idlist: []` | 检查查询语法，尝试放宽搜索条件或换用 MeSH 术语 |
| PMID 无摘要 | 某些旧文献或信件类文章可能无摘要，检查 `esummary` 返回的 `title` 判断 |
| 网络超时 | 设置 `timeout=10`，重试最多 3 次 |
| XML 解析错误 | 使用 `retmode=json` 避免解析 XML |

## 相关 API

- **Unpaywall**：通过 DOI 查找开放获取 PDF → 参见 [api-unpaywall.md](api-unpaywall.md)
- **Google Scholar**：跨学科文献搜索与引用数据 → 参见 [api-google-scholar.md](api-google-scholar.md)
- **补充说明**：PubMed 返回的 `elocationid` 字段通常包含 DOI，可直接传给 Unpaywall 查找免费 PDF
