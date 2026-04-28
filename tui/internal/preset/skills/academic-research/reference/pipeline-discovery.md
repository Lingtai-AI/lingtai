# Pipeline: 学术论文发现（Discovery）

> 从任意入口发现论文：Google Scholar 页面 → 作者名 → 关键词 → DOI，按需渐进深入。

## 目标

给定一个起始点（关键词 / 作者名 / Scholar 页面 URL / DOI），快速返回一批候选论文的标题、作者、引用数、摘要和原文链接。

---

## 工作流步骤

1. **判断输入类型** — 关键词 / 作者名 / Scholar URL / DOI？
2. **选择最优通道** — 按下方决策树选择 A / B / C / D 方案。
3. **执行抓取 / API 调用** — 获取候选论文列表。
4. **标准化输出** — 统一为 `{title, authors, year, citations, doi, url, snippet}` 列表。
5. **（可选）深入** — 对感兴趣的单篇进入 → 参见 [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md)。

---

## 决策树

```
输入是什么？
├─ 关键词 / 短语
│   ├─ 需要 Scholar 页面级数据（引用数、snippet）？
│   │   ├─ 是 → 方案 B: curl + BeautifulSoup
│   │   └─ 否 → 方案 D: OpenAlex API（结构化，最快）
│   └─ 物理或 CS 领域？
│       └─ 是 → 方案 D': arXiv API（预印本优先）
│
├─ Google Scholar URL（citations?user=... 或 scholar?q=...）
│   ├─ 快速浏览标题 → 方案 A: web_read 工具
│   └─ 完整数据     → 方案 B: curl + BeautifulSoup
│
├─ 作者名
│   └─ 方案 D: OpenAlex /author 端点 → 返回作者 profile + 代表作
│
├─ DOI
│   └─ 已有精确目标 → 跳过 discovery，直接 → 参见 [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md)
│
└─ 无法判断 → 默认方案 B: curl + BeautifulSoup（最通用）
```

---

## 代码示例

### 方案 A: web_read 工具（零代码，最快）

适用：快速浏览标题和类型标识。元数据（作者、引用数、DOI）大量缺失。

```python
# 使用 web_read 工具（非 Python，直接调用工具）
# web_read({ url: "https://scholar.google.com/citations?user=XXXXXXX&hl=en", output_format: "text" })
```

### 方案 B: curl + BeautifulSoup（推荐！实测可用）

依赖：`pip install requests beautifulsoup4 lxml`

```python
import re
import requests
from bs4 import BeautifulSoup


def scrape_scholar(query: str, limit: int = 10) -> list[dict]:
    """用 curl + BeautifulSoup 抓取 Scholar 搜索结果，返回标准化论文列表。"""
    headers = {
        "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
        "Accept-Language": "en-US,en;q=0.9",
    }
    search_url = f"https://scholar.google.com/scholar?q={query.replace(' ', '+')}&hl=en"
    r = requests.get(search_url, headers=headers, timeout=15)
    soup = BeautifulSoup(r.text, "lxml")

    papers = []
    for item in soup.select(".gs_ri")[:limit]:
        title_tag = item.select_one(".gs_rt a")
        raw_title = title_tag.get_text() if title_tag else ""
        # 关键修复：Scholar HTML 中 <b> 标签分割标题，需补充空格
        title = re.sub(r"([a-z])([A-Z])", r"\1 \2", raw_title).strip()

        meta = item.select_one(".gs_a")
        meta_text = meta.get_text(strip=True) if meta else ""

        cite_tag = item.select_one(".gs_fl a")
        cit_match = re.search(r"Cited by (\d+)", cite_tag.get_text() if cite_tag else "")
        citations = int(cit_match.group(1)) if cit_match else 0

        link = title_tag["href"] if title_tag and title_tag.has_attr("href") else ""
        snippet_tag = item.select_one(".gs_rs")
        snippet = snippet_tag.get_text(strip=True) if snippet_tag else ""

        papers.append({
            "title": title,
            "authors_meta": meta_text,
            "citations": citations,
            "url": link,
            "snippet": snippet,
        })
    return papers


# 使用
for p in scrape_scholar("transformer attention is all you need"):
    print(f"[{p['citations']} cites] {p['title'][:60]}")
```

### 方案 C: Camoufox（当 B 失败 / 被封时）

> ⚠️ 已从 `playwright_stealth` 旧 API 迁移到 Camoufox。

依赖：`pip install camoufox && python -m camoufox fetch`

```python
from camoufox.sync_api import Camoufox


def scrape_scholar_camoufox(url: str) -> list[dict]:
    """使用 Camoufox 浏览器绕过反爬，抓取 Scholar 页面。"""
    with Camoufox(headless=True) as browser:
        page = browser.new_page()
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(3000)

        papers = []
        for row in page.query_selector_all("tr.gsc_a_tr"):
            title_el = row.query_selector("td.gsc_a_t a")
            cite_el = row.query_selector("td.gsc_a_c a")
            year_el = row.query_selector("td.gsc_a_y a")
            papers.append({
                "title": title_el.inner_text() if title_el else None,
                "url": title_el.get_attribute("href") if title_el else None,
                "citations": cite_el.inner_text() if cite_el else "0",
                "year": year_el.inner_text() if year_el else None,
            })
        return papers
```

**速率限制**：每分钟不超过 10–20 次，避免触发 429。

### 方案 D: OpenAlex API（结构化数据，最快）

```python
import requests


def search_openalex(query: str, limit: int = 10) -> list[dict]:
    """用 OpenAlex API 搜索论文，返回结构化数据。"""
    r = requests.get(
        "https://api.openalex.org/works",
        params={
            "filter": f"title_and_abstract.search:{query}",
            "sort": "cited_by_count:desc",
            "per_page": limit,
        },
        timeout=10,
    ).json()

    return [
        {
            "title": w.get("display_name"),
            "year": w.get("publication_year"),
            "citations": w.get("cited_by_count", 0),
            "doi": w.get("doi", ""),
            "url": w.get("id"),
        }
        for w in r.get("results", [])
    ]


# arXiv 专用
def search_arxiv(query: str, limit: int = 10) -> list[str]:
    """arXiv API 搜索，返回标题列表。"""
    r = requests.get(
        "https://export.arxiv.org/api/query",
        params={
            "search_query": f"all:{query}",
            "max_results": limit,
            "sortBy": "submittedDate",
            "sortOrder": "descending",
        },
        timeout=10,
    )
    titles = re.findall(r"<title>(.+?)</title>", r.text)
    return [t for t in titles if not t.strip().startswith("arXiv")][:limit]
```

---

## 失败回退

| 场景 | 表现 | 回退策略 |
|------|------|---------|
| Scholar 返回 429 | curl 被封 | ① 等待 60s 重试 ② 切换到 OpenAlex API ③ 使用 Camoufox + 代理 |
| BeautifulSoup 选择器失效 | 返回空列表 | Scholar 可能改了 HTML 结构，检查 `.gs_ri` / `.gs_rt` 是否仍存在 |
| OpenAlex 返回空 | 0 results | 检查查询语法，或降级到 Scholar 抓取 |
| Camoufox 超时 | timeout error | 增大 `timeout`，检查网络连通性，或回到方案 B |
| 英文 Scholar 页元数据缺失 | 作者/引用数为空 | 改用中文 Scholar 页面（`hl=zh-CN`），或转 API |

---

## 相关 pipeline

- 获取论文全文 → 参见 [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md)
- 分析引用网络与趋势 → 参见 [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md)
- 格式化参考文献 → 参见 [pipeline-citation-tracking.md](pipeline-citation-tracking.md)
- 综合入口：我有什么信息，该去哪个 API？ → 参见 [decision-tree.md](decision-tree.md)
