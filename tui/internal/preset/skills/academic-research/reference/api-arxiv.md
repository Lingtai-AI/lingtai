# arXiv API Reference

## API 概述

arXiv 提供 Open Archives Initiative (OAI) 风格的公开搜索 API，用于检索预印本论文的元数据与全文。

- **端点**: `https://export.arxiv.org/api/query`
- **认证**: 无需 API Key，完全开放
- **响应格式**: Atom XML
- **协议**: 强制 HTTPS（HTTP 自动 301 重定向）
- **适用场景**: 预印本检索、论文元数据获取、自动化学术文献追踪

## 端点与参数

### 搜索端点

| 参数 | 说明 | 示例 |
|---|---|---|
| `search_query` | 搜索表达式，支持字段前缀与布尔运算符 | `ti:transformer+AND+au:vaswani` |
| `start` | 结果偏移量（默认 0） | `start=5` |
| `max_results` | 返回数量上限（默认 25） | `max_results=10` |
| `sortBy` | 排序字段：`relevance` / `lastUpdatedDate` / `submittedDate` | `sortBy=submittedDate` |
| `sortOrder` | 排序方向：`descending` / `ascending` | `sortOrder=descending` |
| `id_list` | 逗号分隔的 arXiv ID，直接获取指定论文 | `id_list=1706.03762,1806.11202` |

### 字段前缀

| 前缀 | 字段 | 示例 |
|---|---|---|
| `ti:` | 标题 | `ti:attention` |
| `au:` | 作者 | `au:vaswani` |
| `abs:` | 摘要 | `abs:neural machine translation` |
| `all:` | 全部字段 | `all:transformer architecture` |
| `cat:` | 分类 | `cat:cs.CL` |
| `co:` | 注释 | `co:NeurIPS` |
| `jr:` | 期刊引用 | `jr:JHEP` |
| `rn:` | 报告编号 | `rn:NSF-1234` |

**布尔运算符**（必须大写）：`AND`、`OR`、`ANDNOT`

```bash
# 复合查询示例：标题含 transformer 且作者非 Smith
curl -s "https://export.arxiv.org/api/query?search_query=ti:transformer+ANDNOT+au:smith&max_results=5"
```

### 常用分类代码

| 代码 | 领域 |
|---|---|
| `cs.CL` | 计算语言学 |
| `cs.AI` | 人工智能 |
| `cs.LG` | 机器学习 |
| `cs.CV` | 计算机视觉 |
| `math.CO` | 组合数学 |
| `physics.hep-th` | 高能物理 - 理论 |
| `q-bio.NC` | 神经科学与计算 |
| `stat.ML` | 统计学 - 机器学习 |

## 代码示例

### 基础搜索

```python
import urllib.request
import xml.etree.ElementTree as ET

def search_arxiv(query, max_results=10, sort_by="relevance", sort_order="descending"):
    """搜索 arXiv 论文。

    Args:
        query: 搜索表达式（支持字段前缀，如 ti:transformer）
        max_results: 返回数量上限
        sort_by: 排序字段 (relevance / lastUpdatedDate / submittedDate)
        sort_order: 排序方向 (descending / ascending)

    Returns:
        list[dict]: 论文列表，每篇含 title, authors, published, abstract, pdf_link, arxiv_id
    """
    url = (
        f"https://export.arxiv.org/api/query?"
        f"search_query={query}&max_results={max_results}"
        f"&sortBy={sort_by}&sortOrder={sort_order}"
    )
    data = urllib.request.urlopen(url, timeout=15).read().decode("utf-8")
    root = ET.fromstring(data)
    ns = {"atom": "http://www.w3.org/2005/Atom", "arxiv": "http://arxiv.org/schemas/atom"}

    results = []
    for entry in root.findall("atom:entry", ns):
        title = entry.find("atom:title", ns).text.strip().replace("\n", " ")
        authors = [a.find("atom:name", ns).text for a in entry.findall("atom:author", ns)]
        published = entry.find("atom:published", ns).text[:10]
        summary = entry.find("atom:summary", ns).text.strip().replace("\n", " ")
        arxiv_id = entry.find("atom:id", ns).text.split("/")[-1]

        pdf_link = None
        for link in entry.findall("atom:link", ns):
            if link.attrib.get("title") == "pdf":
                pdf_link = link.attrib["href"]
                break

        results.append({
            "arxiv_id": arxiv_id,
            "title": title,
            "authors": authors,
            "published": published,
            "abstract": summary,
            "pdf_link": pdf_link,
        })
    return results

# 使用示例
papers = search_arxiv("ti:transformer+AND+au:vaswani", max_results=3)
for p in papers:
    print(f"[{p['published']}] {p['title']}")
    print(f"  Authors: {', '.join(p['authors'])}")
    print(f"  PDF: {p['pdf_link']}")
    print(f"  Abstract: {p['abstract'][:150]}...")
    print()
```

### 分页遍历

```python
def search_arxiv_all(query, total=100, per_page=50):
    """分页获取大量结果。arXiv 建议每次不超过 50 条以避免超时。"""
    all_results = []
    for start in range(0, total, per_page):
        count = min(per_page, total - start)
        batch = search_arxiv(query, max_results=count)
        all_results.extend(batch)
        if len(batch) < count:
            break  # 无更多结果
    return all_results
```

### 通过 arXiv ID 直接获取

```python
def get_by_arxiv_id(arxiv_id):
    """通过 arXiv ID 直接获取单篇论文。"""
    url = f"https://export.arxiv.org/api/query?id_list={arxiv_id}"
    data = urllib.request.urlopen(url, timeout=15).read().decode("utf-8")
    root = ET.fromstring(data)
    ns = {"atom": "http://www.w3.org/2005/Atom"}
    entry = root.find("atom:entry", ns)
    if entry is None:
        return None
    return {
        "title": entry.find("atom:title", ns).text.strip().replace("\n", " "),
        "id": entry.find("atom:id", ns).text,
        "published": entry.find("atom:published", ns).text[:10],
    }
```

## 返回格式

响应为 Atom XML，主要结构：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/"
      xmlns:arxiv="http://arxiv.org/schemas/atom">
  <opensearch:totalResults>1234</opensearch:totalResults>
  <opensearch:startIndex>0</opensearch:startIndex>
  <opensearch:itemsPerPage>10</opensearch:itemsPerPage>
  <entry>
    <id>http://arxiv.org/abs/1706.03762v1</id>
    <title>Attention Is All You Need</title>
    <summary>Abstract text...</summary>
    <published>2017-06-12T17:34:57Z</published>
    <updated>2017-12-06T17:05:42Z</updated>
    <author><name>Ashish Vaswani</name></author>
    <link rel="alternate" type="text/html" href="http://arxiv.org/abs/1706.03762v1"/>
    <link title="pdf" rel="related" type="application/pdf" href="http://arxiv.org/pdf/1706.03762v1"/>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="cs.CL"/>
    <category term="cs.CL"/>
    <category term="cs.AI"/>
    <arxiv:comment>12 pages, 5 figures</arxiv:comment>
  </entry>
</feed>
```

### 关键 XML 路径

| 路径 | 说明 |
|---|---|
| `feed/opensearch:totalResults` | 总匹配数 |
| `entry/id` | arXiv ID（如 `1706.03762v1`） |
| `entry/title` | 论文标题 |
| `entry/summary` | 摘要（可能含 LaTeX） |
| `entry/published` | 首次提交日期 |
| `entry/updated` | 最后更新日期 |
| `entry/author/name` | 作者姓名 |
| `entry/link[@title='pdf']/@href` | PDF 下载链接 |
| `entry/link[@rel='alternate']/@href` | HTML 摘要页 |
| `entry/arxiv:primary_category/@term` | 主分类 |
| `entry/category/@term` | 所有分类 |
| `entry/arxiv:comment` | 作者注释 |

## 速率限制

| 限制类型 | 值 |
|---|---|
| 推荐最大频率 | ~3 请求/秒 |
| 单次最大结果数 | 无硬上限，但建议 ≤ 50 |
| 连接超时 | 建议 15 秒 |
| 高峰期响应时间 | 可能数秒 |

**最佳实践**:
- 请求间加入 0.5–1 秒延迟
- 分页时每次取 50 条以内
- 避免短时间大量并发请求
- 使用 `time.sleep()` 节流

## 失败处理

| 场景 | 处理方式 |
|---|---|
| HTTP 301 | 使用 `https://` 直接请求，或在 curl 中加 `-L` 跟随重定向 |
| 超时 | 增大 timeout 或重试（指数退避） |
| 空结果 | 检查查询语法，简化搜索词，尝试 `all:` 前缀 |
| XML 解析错误 | 检查响应是否为 HTML 错误页而非 Atom XML |
| `entry` 中无 `title` | 跳过该条目（有时 arXiv 迠除条目后 ID 仍出现在索引中） |

```python
import time
import urllib.error

def search_arxiv_robust(query, max_results=10, retries=3):
    """带重试的健壮搜索。"""
    for attempt in range(retries):
        try:
            return search_arxiv(query, max_results)
        except urllib.error.URLError as e:
            if attempt < retries - 1:
                time.sleep(2 ** attempt)
            else:
                raise
```

## 相关 API

- → 参见 [api-crossref.md](api-crossref.md) — 通过 DOI 获取已发表论文的元数据
- → 参见 [api-doi-resolver.md](api-doi-resolver.md) — 将 DOI 解析为完整引用信息
- arXiv 论文通常无 DOI；若论文已正式发表，可通过标题在 CrossRef 中检索对应 DOI
