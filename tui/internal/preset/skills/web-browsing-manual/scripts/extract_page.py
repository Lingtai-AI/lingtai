#!/usr/bin/env python3
"""
web-content-extractor / 万能网页内容提取脚本 v2.0
支持 --tier 渐进式参数：0-3 + auto

用法：
    python3 extract_page.py "https://arxiv.org/abs/1706.03762"         # auto
    python3 extract_page.py "https://arxiv.org/pdf/1706.03762.pdf"       # auto → tier0
    python3 extract_page.py "https://scholar.google.com/..." --tier 2     # 强制tier2
    python3 extract_page.py "https://example.com" --tier auto            # 自动决策
    python3 extract_page.py "https://doi.org/10.1038/..." --tier 1      # API查询
"""

import argparse
import json
import re
import sys
import time
import requests
from bs4 import BeautifulSoup
from urllib.parse import urljoin, urlparse


# ─── Tier 0: PDF 下载 ────────────────────────────────────────
def tier0(url, save_pdf=None):
    """PDF 直链：curl -L，下载到文件或提取文本"""
    print(f"[Tier 0] PDF下载: {url}")
    try:
        r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=30, stream=True)
        r.raise_for_status()
        content_type = r.headers.get("Content-Type", "")
        if "pdf" not in content_type.lower() and not url.lower().endswith(".pdf"):
            print("[Tier 0] 警告：目标可能不是PDF")

        if save_pdf:
            with open(save_pdf, "wb") as f:
                for chunk in r.iter_content(chunk_size=8192):
                    f.write(chunk)
            print(f"[Tier 0] 已保存到 {save_pdf}")

        # 尝试用 fitz 提取文本
        try:
            import fitz
            from io import BytesIO
            doc = fitz.open(stream=r.content, filetype="pdf")
            n_pages = len(doc)
            text = "\n".join(page.get_text() for page in doc)
            doc.close()
            return {"url": url, "method": "tier0-pdf+fitz", "text_preview": text[:1000], "pages": n_pages}
        except ImportError:
            return {"url": url, "method": "tier0-curl", "saved_to": save_pdf}

    except Exception as e:
        return {"url": url, "method": "tier0", "error": str(e)}


# ─── Tier 1: web_read / API ───────────────────────────────────
def tier1(url):
    """Layer 1：web_read / API 查询（DOI/arXiv ID → API）"""
    print(f"[Tier 1] API/web_read: {url}")
    parsed = urlparse(url)
    host = parsed.netloc.lower()

    # arXiv API
    arxiv_match = re.search(r"arxiv\.org/(?:abs|pdf)/(\d{4}\.\d{4,5})", url)
    if arxiv_match:
        arxiv_id = arxiv_match.group(1)
        api_url = f"https://export.arxiv.org/api/query?id_list={arxiv_id}"
        try:
            r = requests.get(api_url, timeout=15)
            soup = BeautifulSoup(r.text, "lxml", features="xml")
            entry = soup.find("entry")
            if entry:
                return {
                    "url": url,
                    "method": "tier1-arxiv-api",
                    "title": entry.find("title").get_text(strip=True) if entry.find("title") else None,
                    "summary": entry.find("summary").get_text(strip=True)[:500] if entry.find("summary") else None,
                    "authors": [a.get_text(strip=True) for a in entry.find_all("author")],
                    "pdf_url": entry.find("link", {"title": "pdf"})["href"] if entry.find("link", {"title": "pdf"}) else None,
                }
        except Exception as e:
            return {"url": url, "method": "tier1-arxiv-api", "error": str(e)}

    # DOI → CrossRef API
# DOI → CrossRef API
    doi_in_url = re.search(r"10\.\d{4,}/[^\s\"'<>)]+", url)

    if not doi_in_url:
        # 尝试从页面提取 DOI
        r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
        soup = BeautifulSoup(r.text, "lxml")
        doi_tag = soup.find("meta", attrs={"name": "citation_doi"}) or \
                   soup.find("meta", attrs={"name": "DC.identifier"})
        if doi_tag and "10." in doi_tag.get("content", ""):
            doi_in_url = re.search(r"10\.\d{4,}/[^\s\"'<>\)]+", doi_tag["content"])

    if doi_in_url:
        doi = doi_in_url.group(0).rstrip("/")
        api_url = f"https://api.crossref.org/works/{doi}"
        try:
            r = requests.get(api_url, headers={"User-Agent": "Mozilla/5.0 (mailto:research@example.com)"}, timeout=15)
            data = r.json().get("message", {})
            return {
                "url": url,
                "method": "tier1-crossref-api",
                "doi": doi,
                "title": data.get("title", ["N/A"])[0],
                "authors": [a.get("family") for a in data.get("author", [])],
                "journal": data.get("container-title", ["N/A"])[0],
                "year": str(data.get("published-print", {}).get("date-parts", [[None]])[0][0]),
                "type": data.get("type"),
            }
        except Exception as e:
            return {"url": url, "method": "tier1-crossref-api", "error": str(e)}

    # 默认：web_read（简化版，返回标题）
    try:
        r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
        soup = BeautifulSoup(r.text, "lxml")
        return {
            "url": url,
            "method": "tier1-requests",
            "title": soup.find("title").get_text(strip=True) if soup.find("title") else None,
            "status": r.status_code,
        }
    except Exception as e:
        return {"url": url, "method": "tier1", "error": str(e)}


# ─── Tier 2: curl + BeautifulSoup ─────────────────────────────
def tier2(url):
    """Layer 2: curl + BeautifulSoup 结构化提取"""
    print(f"[Tier 2] curl+BeautifulSoup: {url}")
    try:
        r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
        soup = BeautifulSoup(r.text, "lxml")
        title = soup.find("title").get_text(strip=True) if soup.find("title") else None

        result = {"url": url, "method": "tier2-curl+bs", "title": title}

        # Google Scholar
        if "scholar.google" in url:
            papers = []
            for card in soup.select("div.gs_ri"):
                title_el = card.select_one("h3.gs_rt")
                abstract_el = card.select_one("div.gs_rs")
                meta_el = card.select_one("div.gs_fl")
                link_el = card.select_one("h3.gs_rt a")
                pdf_tag = card.select_one("a.gs_or_ggsm")
                papers.append({
                    "title": title_el.get_text(strip=True) if title_el else None,
                    "link": link_el["href"] if link_el else None,
                    "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
                    "meta": meta_el.get_text(strip=True) if meta_el else None,
                    "pdf": pdf_tag["href"] if pdf_tag else None,
                })
            result["papers"] = papers[:10]
            result["count"] = len(papers)

        # arXiv
        elif "arxiv.org" in url:
            abstract_el = soup.find("blockquote", class_="abstract")
            result["abstract"] = abstract_el.get_text(strip=True) if abstract_el else None
            pdf_links = re.findall(r'href="(/pdf/[^\"]+\.pdf)"', r.text)
            result["pdf_links"] = [urljoin(url, p) for p in pdf_links[:3]]

        # Nature.com
        elif "nature.com" in url:
            og_title = soup.find("meta", property="og:title")
            og_desc = soup.find("meta", property="og:description")
            citation_doi = soup.find("meta", attrs={"name": "citation_doi"})
            result["og_title"] = og_title["content"] if og_title else None
            result["og_description"] = og_desc["content"] if og_desc else None
            result["doi"] = citation_doi["content"] if citation_doi else None

        # Springer
        elif "springer.com" in url:
            citation_doi = soup.find("meta", attrs={"name": "citation_doi"})
            title_meta = soup.find("meta", attrs={"name": "citation_title"})
            result["doi"] = citation_doi["content"] if citation_doi else None
            result["meta_title"] = title_meta["content"] if title_meta else None

        return result

    except Exception as e:
        return {"url": url, "method": "tier2", "error": str(e)}


# ─── Tier 3: Playwright stealth ────────────────────────────────
def tier3(url, wait_time=3):
    """Layer 3: Playwright stealth — JS 渲染 / 登录态页面"""
    print(f"[Tier 3] Playwright stealth: {url}")
    try:
        from playwright.sync_api import sync_playwright
        from playwright_stealth import stealth_sync
    except ImportError:
        return {"url": url, "method": "tier3", "error": "Playwright 未安装：pip install playwright && playwright install chromium && pip install playwright-stealth"}

    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True)
            page = browser.new_page()
            stealth_sync(page)
            # ⚠️ Nature/Springer 不用 networkidle，会超时！
            page.goto(url, wait_until="domcontentloaded", timeout=30000)
            page.wait_for_timeout(wait_time * 1000)
            content = page.inner_text("body")
            html = page.content()
            title = page.title()
            browser.close()
            return {
                "url": page.url,
                "method": "tier3-playwright-stealth",
                "title": title,
                "body_preview": content[:2000],
                "html_len": len(html),
            }
    except Exception as e:
        return {"url": url, "method": "tier3", "error": str(e)}


# ─── auto tier 决策 ────────────────────────────────────────────
def auto_tier(url):
    """
自动选择最优提取层"""
    u = url.lower()

    if u.endswith(".pdf"):
        return 0
    if "arxiv.org/abs" in u and "scholar" not in u:
        return 1
    if "openalex" in u or "crossref" in u or "export.arxiv" in u:
        return 1
    if "pubmed" in u:
        return 1
    if "nature.com" in u:
        return 2
    if "scholar.google" in u:
        return 2
    if "springer.com" in u:
        return 3
    if "doi.org" in u:
        return 1
    return 2

# ─── main ──────────────────────────────────────────────────────
def main():
    parser = argparse.ArgumentParser(description="万能网页内容提取 v2.0")
    parser.add_argument("url", help="目标 URL")
    parser.add_argument("--tier", choices=["0", "1", "2", "3", "auto"], default="auto",
                        help="提取层（默认 auto 自动决策）")
    parser.add_argument("--save", help="保存 PDF 到文件")
    parser.add_argument("--wait", type=int, default=3, help="Playwright 等待秒数（默认3）")
    parser.add_argument("--json", help="保存结果为 JSON")
    parser.add_argument("--delay", type=float, default=1.0, help="请求间隔秒数（默认1.0）")
    args = parser.parse_args()

    # tier 决策
    if args.tier == "auto":
        chosen = auto_tier(args.url)
        print(f"[INFO] 自动选择 Tier {chosen}")
    else:
        chosen = int(args.tier)

    tier_funcs = {0: tier0, 1: tier1, 2: tier2, 3: tier3}
    if chosen == 0:
        result = tier_funcs[0](args.url, save_pdf=args.save)
    else:
        result = tier_funcs[chosen](args.url)

    # 输出
    print(f"\n[结果] 方法: {result.get('method', 'unknown')}")
    if "title" in result and result["title"]:
        print(f"[标题] {result['title']}")
    if "papers" in result:
        print(f"[论文数] {result['count']}")
        for i, p in enumerate(result["papers"][:3], 1):
            print(f"  {i}. {p.get('title', 'N/A')[:60]}")
    if "abstract" in result:
        print(f"[摘要] {str(result['abstract'])[:100]}...")
    if "pdf_links" in result:
        print(f"[PDF] {result['pdf_links']}")
    if "doi" in result:
        print(f"[DOI] {result['doi']}")

    if "error" in result:
        print(f"[错误] {result['error']}")
        # 自动降级
        if chosen < 3:
            print(f"[降级] 尝试 Tier {chosen + 1}...")
            time.sleep(args.delay)
            if chosen + 1 == 3:
                result = tier_funcs[chosen + 1](args.url, wait_time=args.wait)
            else:
                result = tier_funcs[chosen + 1](args.url)

    if args.json:
        with open(args.json, "w", encoding="utf-8") as f:
            json.dump(result, f, ensure_ascii=False, indent=2)
        print(f"[INFO] 结果已保存到 {args.json}")

    # 返回码：成功=0，有误=1
    sys.exit(0 if "error" not in result else 1)




# ─── Smoke test ────────────────────────────────────────────────
def _smoke_test():
    """基本 sanity test：auto_tier 决策 + import 检查"""
    # Test auto_tier
    cases = [
        ("https://arxiv.org/abs/1706.03762", 1),
        ("https://arxiv.org/pdf/1706.03762.pdf", 0),
        ("https://scholar.google.com/scholar?q=solar", 2),
        ("https://www.nature.com/articles/s41586-023-05995-9", 2),
        ("https://link.springer.com/article/10.12942/lrr-2014-3", 3),
        ("https://doi.org/10.1038/s41586-023-05995-9", 1),
    ]
    all_ok = True
    for url, expected in cases:
        got = auto_tier(url)
        if got != expected:
            print(f"FAIL auto_tier({url!r}): expected {expected}, got {got}")
            all_ok = False
    print("[TEST] auto_tier: " + ("all passed" if all_ok else "SOME FAILED"))
    print("[TEST] tier2 imports: OK")

if __name__ == "__main__":
    _smoke_test()
    main()

if __name__ == "__main__":
    main()
