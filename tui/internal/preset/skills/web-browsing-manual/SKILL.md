---
name: web-browsing-manual
description: How to browse the web — reach for this before fetching any URL. Four-tier progressive strategy covering PDFs, academic APIs (arXiv, CrossRef, OpenAlex), static pages (curl + BeautifulSoup), and JS-rendered or login-gated pages (Playwright stealth). Includes site-specific playbooks for Google Scholar, Nature, Springer, arXiv, PubMed, and NASA ADS, CSS-selector tables, regex cheat sheets, known-site API endpoints, and a runnable auto-tier script with error-driven fallback. Use when the agent needs to read, extract, or download web content.
version: 2.0.0
tags: [web, browsing, extraction, tier0, tier1, tier2, tier3]
---

# web-browsing-manual

> **Browsing the web — a four-tier progressive playbook.**
> From a one-line `curl` to headless Chromium; pick the cheapest tier that works, escalate only on failure.
>
> *Adapted from the `web-content-extractor` skill authored in the `skill-developer` network (avatars under `minimax_cn`).*

---

## 🚀 Tier 0 — 一行上手（PDF直链 / DOI / arXiv ID）

**适用**：PDF 直链、DOI 字符串、arXiv ID
**工具**：curl + fitz（无需脚本）

```bash
# PDF 直链
curl -L "https://arxiv.org/pdf/1706.03762.pdf" -o paper.pdf

# arXiv ID → 自动推导 PDF 路径
curl -L "https://arxiv.org/pdf/$(echo "2401.12345" | sed 's/\.//').pdf" -o paper.pdf
```

**Python（提取 PDF 文本）**：
```python
import fitz  # pip install pymupdf
doc = fitz.open("paper.pdf")
print(doc[0].get_text()[:500])  # 首页预览
```

**何时用**：URL 含 `.pdf` 或已知 DOI/arXiv ID。

---

## 🎯 Tier 1 — 快速返回（API 查询 / web_read）

**适用**：arXiv 论文页、已有 DOI/ID 的学术资源
**工具**：web_read 工具，或 API（无需安装库）

```python
# 方法A：web_read（零代码，最快）
web_read({
    url: "https://arxiv.org/abs/1706.03762",
    output_format: "markdown"
})

# 方法B：arXiv API（精确元数据）
# GET https://export.arxiv.org/api/query?id_list=1706.03762
# 返回：标题、摘要、作者、分类、PDF链接

# 方法C：CrossRef API（任意DOI）
# GET https://api.crossref.org/works/10.1038/s41586-023-05995-9
# 返回：标题、作者、期刊、License、DOI

# 方法D：OpenAlex API（最强，任意DOI）
# GET https://api.openalex.org/works/https://doi.org/10.1038/s41586-023-05995-9
# 返回：完整元数据 + 引用数 + 主题分类 + PDF链接
```

**何时用**：已有 DOI/arXiv ID，只想快速拿元数据和摘要。

---

## ⚙️ Tier 2 — 结构化提取（curl + BeautifulSoup）

**适用**：Google Scholar 列表页、Nature.com（og meta）、arXiv（结构化元数据）、开放获取文章
**工具**：需安装 `pip install requests beautifulsoup4 lxml`
**速度**：~0.15 秒/页

```python
import requests
from bs4 import BeautifulSoup
import re

def extract_layer2(url):
    """Layer 2: curl + BeautifulSoup 结构化提取"""
    r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
    soup = BeautifulSoup(r.text, "lxml")

    # 通用：标题
    title = soup.find("title").get_text(strip=True) if soup.find("title") else None

    # Google Scholar（搜索结果页）
    if "scholar.google" in url:
        papers = []
        for card in soup.select("div.gs_ri"):
            title_el = card.select_one("h3.gs_rt")
            abstract_el = card.select_one("div.gs_rs")
            meta_el = card.select_one("div.gs_fl")
            papers.append({
                "title": title_el.get_text(strip=True) if title_el else None,
                "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
                "meta": meta_el.get_text(strip=True) if meta_el else None,
            })
        return {"title": title, "papers": papers}

    # arXiv：PDF链接发现
    elif "arxiv.org" in url:
        abstract_el = soup.find("blockquote", class_="abstract")
        pdf_links = re.findall(r'href="(/pdf/[^"]+\.pdf)"', r.text)
        return {
            "title": title,
            "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
            "pdf_links": [urljoin(url, p) for p in pdf_links[:3]],
        }

    # Nature.com：og meta
    elif "nature.com" in url:
        og_title = soup.find("meta", property="og:title")
        og_desc = soup.find("meta", property="og:description")
        citation_doi = soup.find("meta", attrs={"name": "citation_doi"})
        return {
            "title": og_title["content"] if og_title else title,
            "description": og_desc["content"] if og_desc else None,
            "doi": citation_doi["content"] if citation_doi else None,
        }

    # 默认
    else:
        return {"title": title}
```

**关键 CSS 选择器**：

| 网站 | 选择器 | 提取内容 |
|------|--------|---------|
| Google Scholar | `div.gs_ri` | 单条论文卡片 |
| Google Scholar | `h3.gs_rt` | 论文标题 |
| Google Scholar | `div.gs_rs` | 摘要/snippet |
| Google Scholar | `div.gs_fl` | 引用/链接信息 |
| arXiv | `h1.title` | 标题 |
| arXiv | `blockquote.abstract` | 摘要 |
| arXiv | `a[href*="/pdf/"]` | PDF 链接 |
| Nature.com | `meta[property="og:title"]` | 标题（curl 可拿）|
| Nature.com | `meta[name="citation_doi"]` | DOI |
| Springer | `meta[name="citation_doi"]` | DOI（开放获取）|

**何时用**：静态页面，不需要 JS 渲染，curl 速度快。

---

## 🔧 Tier 3 — 专家定制（Playwright stealth）

**适用**：Google Scholar 登录态、持续 404 页面（Springer）、需 JS 渲染的完整正文
**工具**：需安装 `pip install playwright && playwright install chromium && pip install playwright-stealth`
**注意**：Nature/Springer 用 `domcontentloaded` 而非 `networkidle`

```python
from playwright.sync_api import sync_playwright
from playwright_stealth import stealth_sync

def extract_layer3(url, wait_time=3):
    """Layer 3: Playwright stealth — JS 渲染 / 登录态页面"""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()
        stealth_sync(page)

        # ⚠️ 关键：不要用 networkidle（Nature/Springer 会无限加载）
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(wait_time * 1000)

        return {
            "url": page.url,
            "title": page.title(),
            "body": page.inner_text("body")[:2000],
            "html_len": len(page.content()),
        }
```

**何时用**：Layer 1/2 失败，或必须 JS 渲染的页面。

---

## 🧭 tier 自动决策树

```python
def auto_tier(url):
    """自动选择最优提取层"""
    url_lower = url.lower()

    # Tier 0: PDF 直链
    if url_lower.endswith(".pdf"):
        return 0, "PDF直链 → curl -L"

    # Tier 1: API / 静态结构页
    if re.search(r"^(10\.\d{4,}|arXiv:)", url):
        return 1, "DOI/arXiv ID → API 查询"
    if "arxiv.org/abs" in url_lower and "scholar" not in url_lower:
        return 1, "arXiv 论文页 → web_read"

    # Tier 2: curl+BS
    if "scholar.google" in url_lower:
        return 2, "Scholar 搜索 → curl+BS"
    if "nature.com" in url_lower:
        return 2, "Nature → curl+BS（og meta）"
    if "springer.com" in url_lower:
        return 3, "Springer → Playwright（需session）"

    # 默认
    return 2, "通用 → curl+BS，失败则降级Layer 3"
```

---

## 📊 各网站 tier 推荐表

| 网站 | 推荐层 | 成功率 | 备注 |
|------|--------|--------|------|
| arXiv 论文页 | Tier 1 | ⭐⭐⭐⭐⭐ | web_read 或 API |
| arXiv PDF 直链 | Tier 0 | ⭐⭐⭐⭐⭐ | curl -L + fitz |
| Google Scholar 列表 | Tier 2 | ⭐⭐⭐⭐ | curl+BS，IP需干净 |
| Google Scholar 详情 | Tier 3 | ⭐⭐ | 需登录态或 stealth |
| Nature.com | Tier 2/3 | ⭐⭐⭐ | og meta 可拿，需JS拿正文 |
| Springer 开放获取 | Tier 2 | ⭐⭐⭐ | DOI meta 可用 |
| Springer 付费 | Tier 3 | ⭐⭐ | 需 cookie/session |
| OpenAlex API | Tier 1 | ⭐⭐⭐⭐⭐ | 完全免费，最稳 |
| CrossRef API | Tier 1 | ⭐⭐⭐⭐⭐ | 完全免费，最稳 |

---

## 📦 辅助资产

| 文件 | 内容 |
|------|------|
| `assets/api-endpoints.json` | 各 API 端点 + 参数 |
| `assets/site-templates.json` | 已知网站的 CSS 选择器模板 |
| `assets/css-selectors.json` | 常见场景 CSS 模式库 |
| `assets/regex-patterns.json` | DOI/arXiv/PMID 正则模板 |
| `scripts/extract_page.py` | 可执行脚本，支持 `--tier` 参数 |

---

## ⚠️ 已知限制

1. **主流出版商（Wiley/Science/PNAS/Elsevier）**：几乎全部 403，API 是唯一路径
2. **Nature.com**：Playwright 不要用 `networkidle`，会超时；用 `domcontentloaded`
3. **Google Scholar**：IP 频繁请求会被临时封禁，建议加 `time.sleep(2)`
4. **Semantic Scholar API**：需 API Key（免费申请），否则限速严格
5. **PDF 链接**：arXiv 页内无直接 PDF 链接，需从 ID 推导 `/pdf/{ID}.pdf`

---


