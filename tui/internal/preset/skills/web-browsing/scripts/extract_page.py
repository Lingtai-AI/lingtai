#!/usr/bin/env python3
"""
web-content-extractor / 万能网页内容提取脚本 v3.0
七层渐进式提取：Tier 0-5 + auto + search + fallback

用法：
    # 自动分层提取
    python3 extract_page.py "https://arxiv.org/abs/1706.03762"

    # 强制指定层
    python3 extract_page.py "https://example.com" --tier 2
    python3 extract_page.py "https://example.com" --tier 1.5

    # 自动降级链（失败时自动尝试下一层）
    python3 extract_page.py "https://example.com" --fallback

    # 搜索模式
    python3 extract_page.py "quantum computing" --search
    python3 extract_page.py "quantum computing" --search --search-provider tavily

    # 保存结果
    python3 extract_page.py "https://example.com" --json result.json

Tier 层级说明：
    0   PDF 直接下载 (curl + fitz)
    1   API 元数据查询 (CrossRef, arXiv, OpenAlex 等)
    1.5 trafilatura 快速提取 (~0.05s/page)
    2   BeautifulSoup 结构化提取
    3   Playwright stealth JS 渲染 (~3-10s)
    4   Jina Reader API fallback (云端转换)
    5   AI 搜索 (Tavily / Exa)
"""

import argparse
import json
import os
import re
import sys
import time
import random
import requests
from bs4 import BeautifulSoup
from urllib.parse import urljoin, urlparse, quote_plus


# ──────────────────────────────────────────────────────────
#  Utility helpers
# ──────────────────────────────────────────────────────────

def _safe_get(url, headers=None, timeout=15, **kwargs):
    """requests.get with default User-Agent and error handling."""
    hdrs = {"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
            "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"}
    if headers:
        hdrs.update(headers)
    return requests.get(url, headers=hdrs, timeout=timeout, **kwargs)


def _extract_title(soup):
    """Extract page title from various meta sources."""
    og = soup.find("meta", property="og:title")
    if og and og.get("content"):
        return og["content"]
    dc = soup.find("meta", attrs={"name": "citation_title"})
    if dc and dc.get("content"):
        return dc["content"]
    title = soup.find("title")
    return title.get_text(strip=True) if title else None


def _is_pdf_response(r, url):
    """Check if response is a PDF (by Content-Type, .pdf suffix, or /pdf/ path)."""
    ct = r.headers.get("Content-Type", "").lower()
    u = url.lower()
    return "pdf" in ct or u.endswith(".pdf") or "/pdf/" in u


# ──────────────────────────────────────────────────────────
#  Tier 0: PDF Direct Download
# ──────────────────────────────────────────────────────────

def tier0(url, save_pdf=None):
    """Tier 0: PDF 直链下载 + fitz 文本提取"""
    print(f"[Tier 0] PDF 下载: {url}")
    try:
        r = _safe_get(url, stream=True, timeout=30)
        r.raise_for_status()

        if not _is_pdf_response(r, url):
            print("[Tier 0] 警告: 目标可能不是 PDF")

        if save_pdf:
            with open(save_pdf, "wb") as f:
                for chunk in r.iter_content(chunk_size=8192):
                    f.write(chunk)
            print(f"[Tier 0] 已保存到 {save_pdf}")

        # Try fitz for text extraction
        try:
            import fitz
            from io import BytesIO
            doc = fitz.open(stream=r.content, filetype="pdf")
            n_pages = len(doc)
            text = "\n".join(page.get_text() for page in doc)
            doc.close()
            return {"url": url, "method": "tier0-pdf+fitz",
                    "text_preview": text[:2000], "pages": n_pages,
                    "text_length": len(text)}
        except ImportError:
            return {"url": url, "method": "tier0-curl",
                    "saved_to": save_pdf, "size": len(r.content)}

    except Exception as e:
        return {"url": url, "method": "tier0", "error": str(e)}


# ──────────────────────────────────────────────────────────
#  Tier 1: API Metadata Queries
# ──────────────────────────────────────────────────────────

def tier1(url):
    """Tier 1: API 元数据查询 (arXiv, CrossRef, OpenAlex, PubMed, Unpaywall)"""
    print(f"[Tier 1] API 查询: {url}")
    parsed = urlparse(url)
    host = parsed.netloc.lower()
    u = url.lower()

    # --- arXiv ---
    arxiv_match = re.search(r"arxiv\.org/(?:abs|pdf|html)/(\d{4}\.\d{4,5}(?:v\d+)?)", url, re.I)
    if arxiv_match or "export.arxiv" in u:
        arxiv_id = arxiv_match.group(1) if arxiv_match else re.search(r"id_list=([^\s&]+)", url)
        if arxiv_id:
            if isinstance(arxiv_id, re.Match):
                arxiv_id = arxiv_id.group(1)
            api_url = f"https://export.arxiv.org/api/query?id_list={arxiv_id}"
            try:
                r = _safe_get(api_url, timeout=15)
                import xml.etree.ElementTree as ET
                root = ET.fromstring(r.text)
                ns = {"atom": "http://www.w3.org/2005/Atom"}
                entry = root.find("atom:entry", ns)
                if entry is not None:
                    title = entry.find("atom:title", ns)
                    summary = entry.find("atom:summary", ns)
                    authors = [a.find("atom:name", ns).text
                               for a in entry.findall("atom:author", ns)]
                    pdf_link = None
                    for link in entry.findall("atom:link", ns):
                        if link.get("title") == "pdf":
                            pdf_link = link.get("href")
                    return {
                        "url": url, "method": "tier1-arxiv-api",
                        "title": title.text.strip() if title is not None else None,
                        "abstract": summary.text.strip()[:1000] if summary is not None else None,
                        "authors": authors,
                        "pdf_url": pdf_link,
                        "arxiv_id": arxiv_id,
                    }
            except Exception as e:
                return {"url": url, "method": "tier1-arxiv-api", "error": str(e)}

    # --- DOI → CrossRef ---
    doi = _extract_doi(url)
    if doi:
        # Try CrossRef
        result = _crossref_lookup(doi, url)
        if result and "error" not in result:
            return result

    # --- PubMed ---
    pmid_match = re.search(r"pubmed\.ncbi\.nlm\.nih\.gov/(\d+)", url, re.I)
    if pmid_match:
        pmid = pmid_match.group(1)
        try:
            api_url = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi"
            r = _safe_get(api_url, params={"db": "pubmed", "id": pmid,
                                           "rettype": "abstract", "retmode": "xml"},
                          timeout=15)
            import xml.etree.ElementTree as ET
            root = ET.fromstring(r.text)
            article = root.find(".//PubmedArticle/MedlineCitation/Article")
            if article is not None:
                title_el = article.find("ArticleTitle")
                abstract_el = article.find("Abstract/AbstractText")
                return {
                    "url": url, "method": "tier1-pubmed-api",
                    "pmid": pmid,
                    "title": title_el.text if title_el is not None else None,
                    "abstract": abstract_el.text[:1000] if abstract_el is not None else None,
                }
        except Exception as e:
            return {"url": url, "method": "tier1-pubmed-api", "error": str(e)}

    # --- Semantic Scholar ---
    if doi:
        result = _semantic_scholar_lookup(doi, url)
        if result and "error" not in result:
            return result

    # --- Fallback: simple request ---
    try:
        r = _safe_get(url, timeout=10)
        soup = BeautifulSoup(r.text, "lxml")
        return {
            "url": url, "method": "tier1-requests",
            "title": _extract_title(soup),
            "status": r.status_code,
        }
    except Exception as e:
        return {"url": url, "method": "tier1", "error": str(e)}


def _extract_doi(url):
    """Extract DOI from URL or page content."""
    # Direct DOI in URL
    doi_match = re.search(r"(?:doi\.org/|doi[:/])\s*(10\.\d{4,}/[^\s\"'<>)]+)", url, re.I)
    if doi_match:
        return doi_match.group(1).rstrip("/")

    # DOI in URL path
    doi_match = re.search(r"(10\.\d{4,}/[^\s\"'<>)]+)", url)
    if doi_match:
        return doi_match.group(1).rstrip("/")

    # Try to fetch and extract from page
    try:
        r = _safe_get(url, timeout=10)
        soup = BeautifulSoup(r.text, "lxml")
        for tag in [soup.find("meta", attrs={"name": "citation_doi"}),
                     soup.find("meta", attrs={"name": "DC.identifier"}),
                     soup.find("meta", attrs={"name": "dc.identifier"})]:
            if tag and tag.get("content") and "10." in tag.get("content", ""):
                doi_match = re.search(r"(10\.\d{4,}/[^\s\"'<>)]+)", tag["content"])
                if doi_match:
                    return doi_match.group(1).rstrip("/")
    except Exception:
        pass
    return None


def _crossref_lookup(doi, original_url):
    """Look up DOI via CrossRef API."""
    try:
        api_url = f"https://api.crossref.org/works/{doi}"
        r = _safe_get(api_url,
                      headers={"User-Agent": "LingTai/3.0 (mailto:lingtai@users.noreply.github.com)"},
                      timeout=15)
        if r.status_code == 404:
            return None
        r.raise_for_status()
        data = r.json().get("message", {})
        return {
            "url": original_url, "method": "tier1-crossref-api",
            "doi": doi,
            "title": data.get("title", [""])[0],
            "authors": [f"{a.get('given', '')} {a.get('family', '')}".strip()
                        for a in data.get("author", [])],
            "journal": data.get("container-title", [""])[0],
            "year": str(data.get("published-print", data.get("published-online", {}))
                        .get("date-parts", [[None]])[0][0]),
            "type": data.get("type"),
            "abstract": data.get("abstract", "")[:500] if data.get("abstract") else None,
            "references_count": len(data.get("reference", [])),
            "is_oa": data.get("is-referenced-by-count"),
        }
    except Exception as e:
        return {"url": original_url, "method": "tier1-crossref-api", "error": str(e)}


def _semantic_scholar_lookup(doi, original_url):
    """Look up DOI via Semantic Scholar API."""
    try:
        api_url = f"https://api.semanticscholar.org/graph/v1/paper/DOI:{doi}"
        r = _safe_get(api_url,
                      params={"fields": "title,authors,abstract,citationCount,year,openAccessPdf,tldr"},
                      timeout=15)
        if r.status_code == 404:
            return None
        r.raise_for_status()
        p = r.json()
        return {
            "url": original_url, "method": "tier1-semantic-scholar",
            "doi": doi,
            "title": p.get("title"),
            "authors": [a.get("name") for a in p.get("authors", [])],
            "abstract": p.get("abstract", "")[:1000],
            "citations": p.get("citationCount"),
            "year": p.get("year"),
            "pdf": (p.get("openAccessPdf") or {}).get("url"),
            "tldr": (p.get("tldr") or {}).get("text"),
        }
    except Exception:
        return None


# ──────────────────────────────────────────────────────────
#  Tier 1.5: trafilatura Fast Extraction
# ──────────────────────────────────────────────────────────

def tier1_5(url):
    """Tier 1.5: trafilatura 快速正文提取 (~0.05s/page)"""
    print(f"[Tier 1.5] trafilatura: {url}")
    try:
        import trafilatura
    except ImportError:
        return {"url": url, "method": "tier1.5", "error":
                "trafilatura 未安装: pip install trafilatura"}

    try:
        html = trafilatura.fetch_url(url)
        if not html:
            return {"url": url, "method": "tier1.5", "error": "trafilatura 返回空内容"}

        # Extract text
        text = trafilatura.extract(html, include_comments=False, favor_precision=True)
        if not text or len(text.strip()) < 50:
            return {"url": url, "method": "tier1.5",
                    "error": f"trafilatura 提取内容过短 ({len(text or '')} chars)"}

        # Extract metadata — bare_extraction returns a Document, not a dict
        doc = trafilatura.bare_extraction(html, include_comments=False)

        # Convert Document to dict if needed (trafilatura may return Document object)
        if doc is not None and not isinstance(doc, dict):
            try:
                metadata = doc.as_dict()
            except AttributeError:
                # Fallback: access attributes directly
                metadata = {
                    "title": getattr(doc, "title", None),
                    "author": getattr(doc, "author", None),
                    "date": getattr(doc, "date", None),
                    "description": getattr(doc, "description", None),
                    "categories": getattr(doc, "categories", None),
                    "tags": getattr(doc, "tags", None),
                }
        else:
            metadata = doc  # Already a dict or None

        result = {
            "url": url, "method": "tier1.5-trafilatura",
            "title": metadata.get("title") if metadata else None,
            "author": metadata.get("author") if metadata else None,
            "date": metadata.get("date") if metadata else None,
            "text": text[:5000],
            "text_length": len(text),
        }

        # Include full text if requested
        if metadata:
            result["description"] = metadata.get("description")
            result["categories"] = metadata.get("categories")
            result["tags"] = metadata.get("tags")

        return result

    except Exception as e:
        return {"url": url, "method": "tier1.5", "error": str(e)}


# ──────────────────────────────────────────────────────────
#  Tier 2: BeautifulSoup Structured Extraction
# ──────────────────────────────────────────────────────────

def tier2(url):
    """Tier 2: curl + BeautifulSoup 结构化提取"""
    print(f"[Tier 2] BeautifulSoup: {url}")
    try:
        r = _safe_get(url, timeout=15)
        r.raise_for_status()
        soup = BeautifulSoup(r.text, "lxml")
        title = _extract_title(soup)

        result = {"url": url, "method": "tier2-bs", "title": title}

        # Extract metadata from meta tags
        _extract_meta(soup, result)

        # Extract JSON-LD structured data
        jsonld = _extract_jsonld(soup)
        if jsonld:
            result["jsonld"] = jsonld

        # Extract OpenGraph data
        og = _extract_opengraph(soup)
        if og:
            result["opengraph"] = og

        # Site-specific extraction
        u = url.lower()

        if "scholar.google" in u:
            _extract_scholar(soup, result)
        elif "arxiv.org" in u:
            _extract_arxiv(soup, result)
        elif "nature.com" in u:
            _extract_nature(soup, result)
        elif "springer.com" in u or "link.springer" in u:
            _extract_springer(soup, result)
        elif "reddit.com" in u or "old.reddit.com" in u:
            _extract_reddit(soup, result)
        elif "github.com" in u:
            _extract_github(soup, result)
        elif "medium.com" in u:
            _extract_medium(soup, result)
        else:
            # Generic: extract main content
            _extract_generic(soup, result)

        return result

    except Exception as e:
        return {"url": url, "method": "tier2", "error": str(e)}


def _extract_meta(soup, result):
    """Extract common meta tags."""
    for name in ["description", "author", "keywords", "citation_doi",
                  "citation_journal_title", "citation_volume", "citation_issue",
                  "citation_firstpage", "citation_lastpage", "citation_date"]:
        tag = soup.find("meta", attrs={"name": name})
        if tag and tag.get("content"):
            key = name.replace("citation_", "").replace("_", "-")
            result[f"meta_{key}"] = tag["content"]


def _extract_jsonld(soup):
    """Extract JSON-LD structured data."""
    scripts = soup.find_all("script", type="application/ld+json")
    if not scripts:
        return None
    data = []
    for s in scripts:
        if s.string:
            try:
                data.append(json.loads(s.string))
            except json.JSONDecodeError:
                pass
    return data if data else None


def _extract_opengraph(soup):
    """Extract OpenGraph + Twitter Card metadata."""
    og = {}
    for tag in soup.find_all("meta", attrs={"property": True}):
        if tag["property"].startswith("og:"):
            og[tag["property"]] = tag.get("content", "")
    for tag in soup.find_all("meta", attrs={"name": True}):
        if tag["name"].startswith("twitter:"):
            og[tag["name"]] = tag.get("content", "")
    return og if og else None


def _extract_scholar(soup, result):
    """Google Scholar result extraction."""
    papers = []
    for card in soup.select("div.gs_ri"):
        title_el = card.select_one("h3.gs_rt")
        abstract_el = card.select_one("div.gs_rs")
        link_el = card.select_one("h3.gs_rt a")
        pdf_tag = card.select_one("a.gs_or_ggsm")
        papers.append({
            "title": title_el.get_text(strip=True) if title_el else None,
            "link": link_el["href"] if link_el else None,
            "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
            "pdf": pdf_tag["href"] if pdf_tag else None,
        })
    result["papers"] = papers[:10]
    result["count"] = len(papers)


def _extract_arxiv(soup, result):
    """arXiv page extraction."""
    abstract_el = soup.find("blockquote", class_="abstract")
    if abstract_el:
        result["abstract"] = abstract_el.get_text(strip=True)
    # Try subject areas
    subjects = soup.find("span", class_="primary-subject")
    if subjects:
        result["subject"] = subjects.get_text(strip=True)


def _extract_nature(soup, result):
    """Nature.com page extraction."""
    og_desc = soup.find("meta", property="og:description")
    if og_desc:
        result["og_description"] = og_desc["content"]
    # Article body
    article = soup.find("div", class_="c-article-body") or soup.find("article")
    if article:
        result["body_preview"] = article.get_text(strip=True)[:2000]


def _extract_springer(soup, result):
    """Springer page extraction."""
    article = soup.find("article") or soup.find("div", class_="c-article-body")
    if article:
        result["body_preview"] = article.get_text(strip=True)[:2000]


def _extract_reddit(soup, result):
    """Reddit page extraction."""
    post = soup.find("div", attrs={"data-testid": "post-container"})
    if not post:
        post = soup.find("div", id="siteTable")
    if post:
        result["post_preview"] = post.get_text(strip=True)[:2000]


def _extract_github(soup, result):
    """GitHub page extraction."""
    readme = soup.find("article", class_="markdown-body") or soup.find("div", id="readme")
    if readme:
        result["readme_preview"] = readme.get_text(strip=True)[:3000]
    # Star count
    star_el = soup.find("span", id="repo-stars-counter-star")
    if star_el:
        result["stars"] = star_el.get_text(strip=True)


def _extract_medium(soup, result):
    """Medium page extraction."""
    article = soup.find("article")
    if article:
        result["body_preview"] = article.get_text(strip=True)[:3000]


def _extract_generic(soup, result):
    """Generic content extraction: main content areas."""
    # Try common content containers
    for selector in ["article", "main", "[role='main']", ".post-content",
                      ".entry-content", ".article-body", "#content", ".content"]:
        container = soup.select_one(selector)
        if container:
            text = container.get_text(strip=True)
            if len(text) > 200:
                result["body_preview"] = text[:3000]
                result["text_length"] = len(text)
                return

    # Last resort: body text
    body = soup.find("body")
    if body:
        result["body_preview"] = body.get_text(strip=True)[:2000]


# ──────────────────────────────────────────────────────────
#  Tier 3: Playwright Stealth
# ──────────────────────────────────────────────────────────

def tier3(url, wait_time=3):
    """Tier 3: Playwright stealth — JS 渲染 / 保护页面"""
    print(f"[Tier 3] Playwright stealth: {url}")
    try:
        from playwright.sync_api import sync_playwright
        try:
            from playwright_stealth import Stealth
            _stealth_v2 = True
        except ImportError:
            try:
                from playwright_stealth import stealth_sync
                _stealth_v2 = False
            except ImportError:
                raise ImportError("playwright-stealth not installed")
    except ImportError:
        return {"url": url, "method": "tier3", "error":
                "Playwright 未安装: pip install playwright && playwright install chromium && "
                "pip install playwright-stealth"}

    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True)
            page = browser.new_page()
            # playwright-stealth v2.0.3+ uses Stealth().use_sync(), old API was stealth_sync(page)
            if _stealth_v2:
                Stealth().use_sync(page)
            else:
                stealth_sync(page)

            # Resource blocking for speed
            def block_resources(route):
                if route.request.resource_type in ["image", "stylesheet", "font", "media"]:
                    route.abort()
                else:
                    route.continue_()
            page.route("**/*", block_resources)

            # ⚠️ Nature/Springer 不用 networkidle，会超时！
            page.goto(url, wait_until="domcontentloaded", timeout=30000)
            page.wait_for_timeout(wait_time * 1000)

            title = page.title()
            content = page.inner_text("body")
            html = page.content()
            final_url = page.url
            browser.close()

            return {
                "url": final_url, "method": "tier3-playwright-stealth",
                "title": title,
                "body_preview": content[:3000],
                "text_length": len(content),
                "html_len": len(html),
            }
    except Exception as e:
        return {"url": url, "method": "tier3", "error": str(e)}


# ──────────────────────────────────────────────────────────
#  Tier 4: Jina Reader API Fallback
# ──────────────────────────────────────────────────────────

def tier4(url):
    """Tier 4: Jina Reader API — 云端 URL 转 markdown"""
    print(f"[Tier 4] Jina Reader: {url}")
    try:
        jina_url = f"https://r.jina.ai/{url}"
        r = _safe_get(jina_url,
                      headers={"Accept": "text/markdown",
                               "X-Return-Format": "markdown"},
                      timeout=45)
        if r.status_code != 200:
            return {"url": url, "method": "tier4-jina",
                    "error": f"Jina returned HTTP {r.status_code}"}

        text = r.text
        if not text or len(text.strip()) < 50:
            return {"url": url, "method": "tier4-jina",
                    "error": f"Jina 返回内容过短 ({len(text)} chars)"}

        # Try to extract title from markdown
        title = None
        first_line = text.strip().split("\n")[0] if text.strip() else ""
        if first_line.startswith("#"):
            title = first_line.lstrip("#").strip()

        return {
            "url": url, "method": "tier4-jina-reader",
            "title": title,
            "text": text[:5000],
            "text_length": len(text),
        }
    except Exception as e:
        return {"url": url, "method": "tier4", "error": str(e)}


# ──────────────────────────────────────────────────────────
#  Tier 5: AI Search (Tavily / Exa / DuckDuckGo)
# ──────────────────────────────────────────────────────────

def tier5(query, provider="duckduckgo", api_key=None, max_results=5):
    """Tier 5: AI 搜索引擎 — 发现式搜索"""
    print(f"[Tier 5] AI Search ({provider}): {query}")

    if provider == "tavily":
        return _search_tavily(query, api_key, max_results)
    elif provider == "exa":
        return _search_exa(query, api_key, max_results)
    elif provider == "duckduckgo":
        return _search_duckduckgo(query, max_results)
    else:
        return {"query": query, "method": f"tier5-{provider}",
                "error": f"未知搜索引擎: {provider}"}


def _search_duckduckgo(query, max_results=10):
    """DuckDuckGo search (free, no key). Supports both ddgs and duckduckgo_search."""
    try:
        try:
            from ddgs import DDGS  # New package name (v9+)
        except ImportError:
            from duckduckgo_search import DDGS  # Legacy name (v8)
        with DDGS() as ddgs:
            results = list(ddgs.text(query, max_results=max_results))
            return {
                "query": query, "method": "tier5-duckduckgo",
                "results": [{"title": r["title"], "url": r["href"],
                             "snippet": r["body"]} for r in results],
                "count": len(results),
            }
    except ImportError:
        return {"query": query, "method": "tier5-duckduckgo", "error":
                "搜索库未安装: pip install ddgs"}
    except Exception as e:
        return {"query": query, "method": "tier5-duckduckgo", "error": str(e)}


def _search_tavily(query, api_key, max_results=5):
    """Tavily AI search."""
    if not api_key:
        api_key = os.environ.get("TAVILY_API_KEY")
    if not api_key:
        return {"query": query, "method": "tier5-tavily",
                "error": "需要 Tavily API key (--api-key 或 TAVILY_API_KEY 环境变量)"}
    try:
        r = requests.post("https://api.tavily.com/search",
                          json={"api_key": api_key, "query": query,
                                "search_depth": "advanced",
                                "include_answer": True,
                                "max_results": max_results,
                                "include_raw_content": False},
                          timeout=30)
        r.raise_for_status()
        data = r.json()
        return {
            "query": query, "method": "tier5-tavily",
            "answer": data.get("answer"),
            "results": data.get("results", []),
            "count": len(data.get("results", [])),
        }
    except Exception as e:
        return {"query": query, "method": "tier5-tavily", "error": str(e)}


def _search_exa(query, api_key, max_results=5):
    """Exa neural search."""
    if not api_key:
        api_key = os.environ.get("EXA_API_KEY")
    if not api_key:
        return {"query": query, "method": "tier5-exa",
                "error": "需要 Exa API key (--api-key 或 EXA_API_KEY 环境变量)"}
    try:
        r = requests.post("https://api.exa.ai/search",
                          headers={"x-api-key": api_key},
                          json={"query": query, "type": "auto",
                                "numResults": max_results,
                                "contents": {"text": {"maxCharacters": 3000}}},
                          timeout=30)
        r.raise_for_status()
        data = r.json()
        return {
            "query": query, "method": "tier5-exa",
            "results": data.get("results", []),
            "count": len(data.get("results", [])),
        }
    except Exception as e:
        return {"query": query, "method": "tier5-exa", "error": str(e)}


# ──────────────────────────────────────────────────────────
#  Auto Tier Decision Engine
# ──────────────────────────────────────────────────────────

def auto_tier(url):
    """
    自动选择最优提取层。
    返回 int (0, 1, 2, 3) 或 float (1.5)。
    """
    u = url.lower()

    # PDF — includes both .pdf suffix and arXiv-style /pdf/ID routes (no .pdf suffix)
    if u.endswith(".pdf") or "/pdf/" in u:
        return 0

    # Known API endpoints
    api_domains = ["arxiv.org/abs", "arxiv.org/html",
                   "openalex.org", "crossref.org", "export.arxiv",
                   "pubmed.ncbi", "eutils.ncbi",
                   "api.core.ac.uk", "api.unpaywall",
                   "dblp.org/search", "paperswithcode.com/api",
                   "doaj.org/api", "zenodo.org/api",
                   "api.semanticscholar"]
    for domain in api_domains:
        if domain in u:
            return 1

    # DOI redirect
    if "doi.org" in u:
        return 1

    # Static content sites → trafilatura (1.5) is best first try
    static_sites = ["wikipedia.org", "substack.com",
                    "blog.", "news.ycombinator.com", "bbc.com",
                    "nytimes.com", "theguardian.com",
                    "stackoverflow.com", "stackexchange.com",
                    "docs.python.org", "docs.google.com"]
    for domain in static_sites:
        if domain in u:
            return 1.5

    # Sites needing JS rendering → Playwright
    js_sites = ["scholar.google", "springer.com", "link.springer",
                "sciencedirect.com", "wiley.com", "ieee.org",
                "dl.acm.org", "tandfonline.com",
                "medium.com", "reuters.com"]
    for domain in js_sites:
        if domain in u:
            return 3

    # Nature.com → BeautifulSoup works for most pages
    if "nature.com" in u:
        return 2

    # Reddit → JSON API if .json, else BS
    if "reddit.com" in u:
        if u.endswith(".json"):
            return 1
        return 2

    # GitHub → BS works well
    if "github.com" in u:
        return 2

    # Default: try trafilatura first (fastest for most content)
    return 1.5


# ──────────────────────────────────────────────────────────
#  Fallback Chain
# ──────────────────────────────────────────────────────────

# The escalation order for fallback
TIER_ORDER = [0, 1, 1.5, 2, 3, 4]


def extract_with_fallback(url, start_tier=None, max_tier=4, save_pdf=None):
    """
    自动降级提取：从 start_tier 开始，失败时自动尝试下一层。
    """
    if start_tier is None:
        start_tier = auto_tier(url)

    # Build tier chain
    tier_chain = [t for t in TIER_ORDER if t >= start_tier and t <= max_tier]

    for i, tier in enumerate(tier_chain):
        print(f"\n{'='*50}")
        print(f"[Fallback {i+1}/{len(tier_chain)}] Trying Tier {tier}")

        if tier == 0:
            result = tier0(url, save_pdf=save_pdf)
        elif tier == 1:
            result = tier1(url)
        elif tier == 1.5:
            result = tier1_5(url)
        elif tier == 2:
            result = tier2(url)
        elif tier == 3:
            result = tier3(url)
        elif tier == 4:
            result = tier4(url)
        else:
            continue

        if "error" not in result:
            result["fallback_used"] = i
            return result

        print(f"[Tier {tier}] 失败: {result['error']}")

        if i < len(tier_chain) - 1:
            delay = min(2 ** i + random.random(), 10)
            print(f"[等待 {delay:.1f}s 后尝试下一层...]")
            time.sleep(delay)

    return {"url": url, "method": "fallback-exhausted",
            "error": f"所有层 ({tier_chain}) 均失败"}


# ──────────────────────────────────────────────────────────
#  CLI
# ──────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="万能网页内容提取 v3.0 — 七层渐进式提取",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  %(prog)s "https://arxiv.org/abs/1706.03762"           # auto
  %(prog)s "https://example.com" --tier 1.5             # trafilatura
  %(prog)s "https://example.com" --fallback              # 自动降级
  %(prog)s "quantum computing" --search                  # DuckDuckGo 搜索
  %(prog)s "quantum computing" --search --search-provider tavily --api-key KEY
        """)

    parser.add_argument("url", help="目标 URL 或搜索关键词")
    parser.add_argument("--tier", type=str, default="auto",
                        help="提取层: 0/1/1.5/2/3/4/5/auto (默认 auto)")
    parser.add_argument("--fallback", action="store_true",
                        help="失败时自动降级到下一层")
    parser.add_argument("--save", help="保存 PDF 到文件")
    parser.add_argument("--wait", type=int, default=3,
                        help="Playwright 等待秒数 (默认 3)")
    parser.add_argument("--json", dest="json_file", help="保存结果为 JSON")
    parser.add_argument("--delay", type=float, default=1.0,
                        help="请求间隔秒数 (默认 1.0)")
    parser.add_argument("--max-tier", type=int, default=4,
                        help="fallback 最高层 (默认 4)")
    parser.add_argument("--search", action="store_true",
                        help="搜索模式 (URL 参数作为搜索关键词)")
    parser.add_argument("--search-provider", default="duckduckgo",
                        choices=["duckduckgo", "tavily", "exa"],
                        help="搜索引擎 (默认 duckduckgo)")
    parser.add_argument("--api-key", help="搜索 API key (或环境变量)")
    parser.add_argument("--max-results", type=int, default=5,
                        help="搜索结果数 (默认 5)")

    args = parser.parse_args()

    # ── Search mode ──
    if args.search:
        result = tier5(args.url, provider=args.search_provider,
                       api_key=args.api_key, max_results=args.max_results)
        _print_result(result)
        if args.json_file:
            _save_json(result, args.json_file)
        sys.exit(0 if "error" not in result else 1)

    # ── URL mode ──
    url = args.url

    # Normalize bare identifiers into URLs
    doi_match = re.match(r'^(10\.\d{4,}/[^\s]+)$', url)
    if doi_match:
        url = f"https://doi.org/{doi_match.group(1)}"
        print(f"[INFO] 裸 DOI 已转换为: {url}")
    elif re.match(r'^\d{4}\.\d{4,5}(?:v\d+)?$', url):
        url = f"https://arxiv.org/abs/{url}"
        print(f"[INFO] 裸 arXiv ID 已转换为: {url}")
    elif not url.startswith(("http://", "https://", "ftp://")):
        # Treat as search query if no scheme and not a known ID
        print(f"[INFO] 非 URL 输入，切换到搜索模式: {url}")
        result = tier5(url, provider=args.search_provider,
                       api_key=args.api_key, max_results=args.max_results)
        _print_result(result)
        if args.json_file:
            _save_json(result, args.json_file)
        sys.exit(0 if "error" not in result else 1)

    if args.fallback:
        start = auto_tier(url) if args.tier == "auto" else float(args.tier)
        result = extract_with_fallback(url, start_tier=start,
                                       max_tier=args.max_tier,
                                       save_pdf=args.save)
    else:
        # Determine tier
        if args.tier == "auto":
            chosen = auto_tier(url)
            print(f"[INFO] 自动选择 Tier {chosen}")
        else:
            chosen = float(args.tier)

        # Execute
        if chosen == 0:
            result = tier0(url, save_pdf=args.save)
        elif chosen == 1:
            result = tier1(url)
        elif chosen == 1.5:
            result = tier1_5(url)
        elif chosen == 2:
            result = tier2(url)
        elif chosen == 3:
            result = tier3(url, wait_time=args.wait)
        elif chosen == 4:
            result = tier4(url)
        elif chosen == 5:
            result = tier5(url, provider=args.search_provider,
                           api_key=args.api_key,
                           max_results=args.max_results)
        else:
            print(f"[错误] 无效 tier: {chosen}")
            sys.exit(1)

    _print_result(result)

    if args.json_file:
        _save_json(result, args.json_file)

    sys.exit(0 if "error" not in result else 1)


def _print_result(result):
    """Pretty-print extraction result."""
    print(f"\n{'='*50}")
    print(f"[结果] 方法: {result.get('method', 'unknown')}")

    for key in ["title", "doi", "arxiv_id", "pmid"]:
        if result.get(key):
            print(f"[{key.upper()}] {result[key]}")

    if "papers" in result:
        print(f"[论文数] {result.get('count', len(result['papers']))}")
        for i, p in enumerate(result.get("papers", [])[:3], 1):
            print(f"  {i}. {p.get('title', 'N/A')[:80]}")

    if "abstract" in result:
        print(f"[摘要] {str(result['abstract'])[:150]}...")

    if "text" in result:
        print(f"[正文预览] {result['text'][:200]}...")
        print(f"[正文长度] {result.get('text_length', len(result['text']))} 字符")

    if "text_preview" in result:
        print(f"[文本预览] {result['text_preview'][:200]}...")

    if "body_preview" in result:
        print(f"[正文预览] {result['body_preview'][:200]}...")

    if "pdf_url" in result:
        print(f"[PDF] {result['pdf_url']}")

    if "answer" in result and result["answer"]:
        print(f"[AI回答] {result['answer'][:300]}...")

    if "results" in result and isinstance(result["results"], list):
        print(f"[搜索结果] {result.get('count', len(result['results']))} 条")
        for i, r in enumerate(result["results"][:3], 1):
            title = r.get("title", "N/A")
            print(f"  {i}. {title[:80]}")
            if r.get("url") or r.get("href"):
                print(f"     {r.get('url') or r.get('href')}")

    if "fallback_used" in result:
        print(f"[降级次数] {result['fallback_used']}")

    if "error" in result:
        print(f"[错误] {result['error']}")


def _save_json(result, path):
    """Save result to JSON file."""
    with open(path, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
    print(f"[INFO] 结果已保存到 {path}")


# ──────────────────────────────────────────────────────────
#  Smoke Tests
# ──────────────────────────────────────────────────────────

def _smoke_test():
    """Smoke test: auto_tier 决策 + import 检查 + 浊输入/畸响应验证"""
    import traceback

    # ── Part 1: auto_tier 决策 (净例 + 浊例) ──
    cases = [
        # 净例（标准 URL）
        ("https://arxiv.org/abs/1706.03762", 1),
        ("https://arxiv.org/pdf/1706.03762.pdf", 0),
        ("https://arxiv.org/html/2401.12345", 1),
        ("https://scholar.google.com/scholar?q=solar", 3),
        ("https://www.nature.com/articles/s41586-023-05995-9", 2),
        ("https://link.springer.com/article/10.12942/lrr-2014-3", 3),
        ("https://doi.org/10.1038/s41586-023-05995-9", 1),
        ("https://en.wikipedia.org/wiki/Quantum_computing", 1.5),
        ("https://medium.com/some-article", 3),
        ("https://github.com/user/repo", 2),
        ("https://www.reddit.com/r/programming/hot", 2),
        ("https://www.reddit.com/r/programming/hot.json", 1),
        ("https://stackoverflow.com/questions/12345", 1.5),
        ("https://example.com/some-page", 1.5),
        ("https://docs.python.org/3/library/os.html", 1.5),
        # 浊例（实测踩坑的边界 URL）
        ("https://arxiv.org/pdf/1706.03762", 0),        # 无 .pdf 后缀
        ("https://arxiv.org/pdf/2401.12345v2", 0),      # 有版本号，无后缀
        ("https://reuters.com/technology/", 3),           # 需 JS
        ("https://www.reuters.com/article/some-id", 3),
    ]
    all_ok = True
    for url, expected in cases:
        got = auto_tier(url)
        if got != expected:
            print(f"FAIL auto_tier({url!r}): expected {expected}, got {got}")
            all_ok = False

    print("[TEST] auto_tier: " + ("all passed" if all_ok else "SOME FAILED"))

    # ── Part 2: trafilatura bare_extraction 返回类型验证 ──
    try:
        import trafilatura
        print("[TEST] trafilatura: OK")
        # 浊测试：bare_extraction 是否返回 dict（而非 Document）
        try:
            sample_html = "<html><head><title>Test</title></head><body><p>Hello world content here.</p></body></html>"
            doc = trafilatura.bare_extraction(sample_html, include_comments=False)
            if doc is not None:
                if isinstance(doc, dict):
                    print("[TEST] trafilatura bare_extraction: returns dict ✅")
                else:
                    # Document object — verify our conversion code handles it
                    try:
                        _ = doc.as_dict()
                        print(f"[TEST] trafilatura bare_extraction: returns {type(doc).__name__}, .as_dict() works ✅")
                    except AttributeError:
                        print(f"[TEST] trafilatura bare_extraction: returns {type(doc).__name__}, attribute fallback needed ✅")
        except Exception as e:
            print(f"[TEST] trafilatura bare_extraction: ERROR {e} ❌")
    except ImportError:
        print("[TEST] trafilatura: NOT INSTALLED (tier 1.5 unavailable)")

    # ── Part 3: playwright-stealth API 兼容性 ──
    try:
        from playwright.sync_api import sync_playwright
        try:
            from playwright_stealth import Stealth  # v2.0.3+
            print("[TEST] playwright+stealth (v2 API): OK")
        except ImportError:
            from playwright_stealth import stealth_sync  # legacy
            print("[TEST] playwright+stealth (legacy API): OK")
    except ImportError:
        print("[TEST] playwright+stealth: NOT INSTALLED (tier 3 unavailable)")

    # ── Part 4: DuckDuckGo search 包名兼容 ──
    try:
        try:
            from ddgs import DDGS  # New package name
        except ImportError:
            from duckduckgo_search import DDGS  # Legacy
        print("[TEST] ddgs/duckduckgo-search: OK")
    except ImportError:
        print("[TEST] ddgs/duckduckgo-search: NOT INSTALLED (tier 5 DDG unavailable)")

    # ── Part 5: fitz (pymupdf) ──
    try:
        import fitz
        print("[TEST] pymupdf (fitz): OK")
    except ImportError:
        print("[TEST] pymupdf (fitz): NOT INSTALLED (PDF text extraction unavailable)")

    # ── Part 6: 浊输入 — _is_pdf_response 边界 ──
    class FakeResponse:
        def __init__(self, ct):
            self.headers = {"Content-Type": ct}
    pdf_edge_ok = True
    # 正常 PDF
    if not _is_pdf_response(FakeResponse("application/pdf"), "https://example.com/p.pdf"):
        print("FAIL _is_pdf_response: normal PDF missed"); pdf_edge_ok = False
    # URL 含 /pdf/ 但无 .pdf 后缀（arXiv 实际 URL）
    if not _is_pdf_response(FakeResponse("text/html"), "https://arxiv.org/pdf/1706.03762"):
        print("FAIL _is_pdf_response: arXiv /pdf/ URL missed"); pdf_edge_ok = False
    # 非 PDF
    if _is_pdf_response(FakeResponse("text/html"), "https://example.com/page"):
        print("FAIL _is_pdf_response: false positive on non-PDF"); pdf_edge_ok = False
    # Content-Type 含 pdf 但 URL 不含
    if not _is_pdf_response(FakeResponse("application/pdf;charset=utf-8"), "https://cdn.example.com/download/abc123"):
        print("FAIL _is_pdf_response: Content-Type PDF missed"); pdf_edge_ok = False
    print("[TEST] _is_pdf_response edges: " + ("all passed" if pdf_edge_ok else "SOME FAILED"))

    # ── Part 7: 浊输入 — _extract_doi 边界 ──
    doi_ok = True
    for url, expected_doi in [
        ("https://doi.org/10.1038/s41586-023-05995-9", "10.1038/s41586-023-05995-9"),
        ("https://doi.org/10.1109/iccv48922.2021.00041", "10.1109/iccv48922.2021.00041"),
    ]:
        got = _extract_doi(url)
        if got != expected_doi:
            print(f"FAIL _extract_doi({url!r}): expected {expected_doi!r}, got {got!r}")
            doi_ok = False
    print("[TEST] _extract_doi: " + ("all passed" if doi_ok else "SOME FAILED"))

    print("[TEST] All smoke tests complete.")


if __name__ == "__main__":
    if "--test" in sys.argv:
        _smoke_test()
        sys.exit(0)
    main()
