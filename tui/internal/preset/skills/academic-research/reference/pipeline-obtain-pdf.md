# Pipeline: 获取论文全文与 PDF（Obtain PDF）

> 合并 scholar-obtainer + web-content-extractor 的能力。
> 从元数据到全文：DOI → 元数据 → 免费 PDF → 下载 → 文本提取，支持网页正文抓取。

## 目标

给定 DOI / arXiv ID / 论文 URL，尽可能拿到论文全文（PDF 或纯文本），同时获取完整元数据。

---

## 工作流步骤

1. **判断输入类型** — DOI / arXiv ID / PDF 直链 / 网页 URL？
2. **解析元数据** — CrossRef（DOI）/ OpenAlex / arXiv API。
3. **寻找免费 PDF** — Unpaywall / arXiv 直链 / PMC。
4. **下载 PDF** — curl / requests 直接下载。
5. **（若网页而非 PDF）提取网页正文** — 按站点选用 BeautifulSoup 或 Camoufox。
6. **从 PDF 提取文本** — PyMuPDF 文本提取。
7. **输出** — 返回 `(status, filepath_or_text, metadata)`。

---

## 决策树

```
输入是什么？
├─ PDF 直链（.pdf 结尾）
│   └─ curl 下载 → PyMuPDF 提取文本
│
├─ DOI（10.xxxx/...）
│   ├─ CrossRef 解析元数据
│   ├─ Unpaywall 查免费 PDF
│   │   ├─ 有 → 下载 PDF → 提取文本
│   │   └─ 无 → 返回 landing page URL
│   └─ OpenAlex 查补充元数据
│
├─ arXiv ID（如 2301.00001）
│   ├─ arXiv API 获取元数据
│   └─ https://arxiv.org/pdf/{ID}.pdf 下载 → 提取文本
│
├─ 网页 URL（nature.com / springer.com / scholar 等）
│   ├─ Tier 1: web_read 工具（最快）
│   ├─ Tier 2: curl + BeautifulSoup（结构化提取）
│   └─ Tier 3: Camoufox（JS 渲染 / 需要登录态）
│
└─ 标题 / 关键词 → 先发现再获取 → 参见 [pipeline-discovery.md](pipeline-discovery.md)
```

---

## 代码示例

### 1. DOI → 元数据（CrossRef）

```python
import requests


def resolve_doi(doi: str) -> dict:
    """用 CrossRef 解析 DOI，返回完整元数据。"""
    doi = doi.replace("https://doi.org/", "").replace("http://doi.org/", "")
    r = requests.get(
        f"https://api.crossref.org/works/{doi}",
        headers={"User-Agent": "ResearchBot/1.0 (mailto:user@example.com)"},
        timeout=10,
    )
    d = r.json()["message"]
    return {
        "title": d["title"][0],
        "authors": [f"{a.get('given', '')} {a.get('family', '')}" for a in d.get("author", [])],
        "year": d.get("published-print", d.get("published-online", {})).get("date-parts", [[0]])[0][0],
        "journal": d.get("container-title", [""])[0],
        "doi": doi,
        "citations": d.get("is-referenced-by-count", 0),
        "url": d.get("URL", f"https://doi.org/{doi}"),
    }
```

### 2. 找免费 PDF（Unpaywall）

```python
def find_free_pdf(doi: str, email: str = "user@example.com") -> dict:
    """用 Unpaywall 查找免费 PDF URL。"""
    doi = doi.replace("https://doi.org/", "")
    r = requests.get(
        f"https://api.unpaywall.org/v2/{doi}",
        params={"email": email},
        timeout=10,
    ).json()

    if r.get("is_oa") and r.get("best_oa_location"):
        loc = r["best_oa_location"]
        return {
            "free": True,
            "pdf_url": loc.get("pdf_url"),
            "source": loc.get("repository_name", "Unknown"),
            "license": loc.get("license", "Unknown"),
            "landing_url": loc.get("landing_url"),
        }
    return {"free": False, "title": r.get("title")}
```

### 3. 下载 PDF

```python
import os


def download_pdf(url: str, filepath: str, headers: dict | None = None) -> str:
    """下载 PDF 并保存到 filepath。"""
    os.makedirs(os.path.dirname(filepath) or ".", exist_ok=True)
    if headers is None:
        headers = {"User-Agent": "ResearchBot/1.0 (mailto:user@example.com)"}
    r = requests.get(url, headers=headers, stream=True, timeout=30)
    r.raise_for_status()
    with open(filepath, "wb") as f:
        for chunk in r.iter_content(chunk_size=8192):
            f.write(chunk)
    return filepath
```

### 4. 从 PDF 提取文本（PyMuPDF）

```python
import fitz  # pip install pymupdf


def extract_pdf_text(filepath: str, max_pages: int | None = None) -> str:
    """从 PDF 提取纯文本。"""
    doc = fitz.open(filepath)
    pages = max_pages or len(doc)
    return "\n".join(doc[i].get_text() for i in range(min(pages, len(doc))))


def extract_pdf_summary(filepath: str) -> dict:
    """提取 PDF 摘要（前 3 页）和元数据。"""
    doc = fitz.open(filepath)
    meta = doc.metadata
    first_pages = "\n".join(doc[i].get_text() for i in range(min(3, len(doc))))
    return {"meta": meta, "preview": first_pages[:1000]}
```

### 5. 网页正文提取（多层级）

> ⚠️ 已从 `playwright_stealth` 旧 API 迁移到 Camoufox。

```python
import re
from urllib.parse import urljoin
import requests
from bs4 import BeautifulSoup


def extract_web_tier2(url: str) -> dict:
    """Tier 2: curl + BeautifulSoup 结构化提取，适用于静态页面。"""
    r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
    soup = BeautifulSoup(r.text, "lxml")
    title = soup.find("title").get_text(strip=True) if soup.find("title") else None

    # Google Scholar 搜索结果
    if "scholar.google" in url:
        papers = []
        for card in soup.select("div.gs_ri"):
            t = card.select_one("h3.gs_rt")
            a = card.select_one("div.gs_rs")
            papers.append({
                "title": t.get_text(strip=True) if t else None,
                "abstract": a.get_text(strip=True) if a else None,
            })
        return {"title": title, "papers": papers}

    # arXiv
    if "arxiv.org" in url:
        abstract_el = soup.find("blockquote", class_="abstract")
        pdf_links = [urljoin(url, p) for p in re.findall(r'href="(/pdf/[^"]+\.pdf)"', r.text)]
        return {"title": title, "abstract": abstract_el.get_text(strip=True) if abstract_el else None, "pdf_links": pdf_links[:3]}

    # Nature.com（og meta）
    if "nature.com" in url:
        og_title = soup.find("meta", property="og:title")
        og_desc = soup.find("meta", property="og:description")
        citation_doi = soup.find("meta", attrs={"name": "citation_doi"})
        return {
            "title": og_title["content"] if og_title else title,
            "description": og_desc["content"] if og_desc else None,
            "doi": citation_doi["content"] if citation_doi else None,
        }

    # 通用
    return {"title": title}


def extract_web_tier3(url: str, wait_time: int = 3) -> dict:
    """Tier 3: Camoufox 浏览器提取，适用于 JS 渲染页面。
    
    已从 playwright_stealth 迁移到 Camoufox。
    依赖：pip install camoufox && python -m camoufox fetch
    """
    from camoufox.sync_api import Camoufox

    with Camoufox(headless=True) as browser:
        page = browser.new_page()
        # ⚠️ 不要用 networkidle（Nature/Springer 会无限加载）
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(wait_time * 1000)

        return {
            "url": page.url,
            "title": page.title(),
            "body": page.inner_text("body")[:2000],
            "html_len": len(page.content()),
        }
```

### 6. 一站式获取函数

```python
import os


def obtain_paper(identifier: str, output_dir: str = "/tmp/papers", email: str = "user@example.com"):
    """
    一站式获取论文：
    - 输入：DOI / arXiv ID / PDF URL
    - 输出：(status, filepath_or_url, metadata)
      status ∈ {"pdf", "url", "text", "unknown"}
    """
    os.makedirs(output_dir, exist_ok=True)

    # PDF 直链
    if identifier.endswith(".pdf"):
        fname = f"{output_dir}/{identifier.split('/')[-1]}"
        download_pdf(identifier, fname)
        return ("pdf", fname, {"source": "direct_link"})

    # DOI
    if identifier.startswith("10."):
        meta = resolve_doi(identifier)
        result = find_free_pdf(identifier, email)
        if result["free"] and result["pdf_url"]:
            fname = f"{output_dir}/{identifier.replace('/', '_')}.pdf"
            download_pdf(result["pdf_url"], fname)
            return ("pdf", fname, meta)
        return ("url", result.get("landing_url", meta["url"]), meta)

    # arXiv ID
    arxiv_clean = identifier.replace("arXiv:", "")
    if re.match(r"\d{4}\.\d{4,5}", arxiv_clean):
        pdf_url = f"https://arxiv.org/pdf/{arxiv_clean}.pdf"
        fname = f"{output_dir}/{arxiv_clean}.pdf"
        download_pdf(pdf_url, fname)
        return ("pdf", fname, {"id": arxiv_clean, "source": "arXiv"})

    return ("unknown", None, {"error": "无法识别格式，请提供 DOI / arXiv ID / PDF URL"})


# 使用示例
status, path, meta = obtain_paper("10.1038/nature12373")
print(f"状态: {status}, 路径: {path}, 标题: {meta.get('title', '?')[:50]}")

status, path, meta = obtain_paper("2301.00001")
print(f"状态: {status}, 路径: {path}")
```

---

## 失败回退

| 场景 | 表现 | 回退策略 |
|------|------|---------|
| Unpaywall 无免费版本 | `free: False` | 返回 landing page URL，提示用户手动获取 |
| PDF 下载 403 | `raise_for_status` 失败 | ① 尝试 Camoufox 浏览器下载 ② 换源（PMC / Sci-Hub） |
| PDF 是扫描版（图片格式） | PyMuPDF 提取为空 | 需要 OCR（pytesseract / Tesseract），不在本 pipeline 范围 |
| 网页 Tier 2 提取空 | BeautifulSoup 无匹配 | 降级到 Tier 3: Camoufox 浏览器渲染 |
| Nature/Springer 超时 | `networkidle` 无限等待 | 改用 `domcontentloaded` 事件（见代码注释） |
| Scholar 封 IP | 429 错误 | ① 等 60s ② 切换 API（OpenAlex）③ Camoufox + 代理 |
| 主流出版商全部 403（Wiley/Elsevier） | 不可下载 | 只能通过 API 获取元数据，全文需机构权限 |

---

## 网页抓取 Tier 自动选择

```python
import re


def auto_select_tier(url: str) -> tuple[int, str]:
    """自动选择最优提取层级。"""
    url_lower = url.lower()

    if url_lower.endswith(".pdf"):
        return (0, "PDF直链 → curl 下载")
    if re.match(r"^(10\.\d{4,}|arXiv:)", url):
        return (1, "DOI/arXiv ID → API 查询")
    if "arxiv.org/abs" in url_lower:
        return (1, "arXiv 论文页 → web_read 或 API")
    if "scholar.google" in url_lower:
        return (2, "Scholar 搜索 → curl+BS")
    if "nature.com" in url_lower:
        return (2, "Nature → curl+BS（og meta）")
    if "springer.com" in url_lower:
        return (3, "Springer → Camoufox（需 session）")
    return (2, "通用 → curl+BS，失败则 Tier 3")
```

---

## 相关 pipeline

- 论文发现（从关键词/作者入手） → 参见 [pipeline-discovery.md](pipeline-discovery.md)
- 分析引用网络与趋势 → 参见 [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md)
- 格式化参考文献 → 参见 [pipeline-citation-tracking.md](pipeline-citation-tracking.md)
- 综合入口：我有什么信息，该去哪个 API？ → 参见 [decision-tree.md](decision-tree.md)
