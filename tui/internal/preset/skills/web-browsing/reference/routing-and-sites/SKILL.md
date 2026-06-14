---
name: web-browsing-routing-and-sites
description: >
  Nested web-browsing reference for auto-tier decisions, per-site tier
  recommendations, known limitations/gotchas, and real-time data endpoints.
version: 1.0.0
---

# Web Browsing Routing and Site Reference

Nested web-browsing reference. Open this when the auto-tier extractor misroutes a
site, when you know the site class, or when you need real-time data endpoints.

## Auto-Tier Decision Tree

The bundled `extract_page.py` script auto-selects the cheapest viable tier.完整逻辑见 `scripts/extract_page.py` 的 `auto_tier()` 函数。

**分层规则概要：**

| URL 特征 | 分配 Tier |
|----------|----------|
| PDF (.pdf 后缀 或 /pdf/ 路径) | Tier 0 |
| 学术 API (arXiv, CrossRef, DOI, PubMed, etc.) | Tier 1 |
| 静态内容站 (Wikipedia, BBC, Stack Overflow, etc.) | Tier 1.5 |
| 需结构化提取 (GitHub, Reddit, Nature) | Tier 2 |
| 需 JS 渲染/反爬 (Scholar, Springer, Medium, Reuters) | Tier 3 |
| 其他 → 默认 Tier 1.5 (trafilatura) | Tier 1.5 |

---

## Per-Site Tier Recommendations

| Site | Recommended tier | Success rate | Notes |
|------|------------------|--------------|-------|
| arXiv abstract | Tier 1 | high | API call via `requests` |
| arXiv PDF | Tier 0 | high | curl -L + fitz |
| OpenAlex / CrossRef | Tier 1 | high | Fully free, most reliable |
| Unpaywall | Tier 1 | high | Finds OA PDF for any DOI |
| DBLP | Tier 1 | high | CS papers, conference proceedings |
| CORE | Tier 1 | high | OA full text (30M+ papers) |
| Europe PMC | Tier 1 | high | Biomedical + PMC full text |
| Papers With Code | Tier 1 | high | ML papers with code |
| Google Scholar list | Tier 2 | medium-high | curl + BS, needs clean IP |
| Nature.com | Tier 2/3 | medium | og meta cheap; full body needs JS |
| Springer paywalled | Tier 3 | low | Needs cookies/session |
| Medium / Substack | Tier 1.5 | high | trafilatura extracts clean text |
| Reddit | Tier 2 | high | `.json` API or old.reddit.com + BS |
| GitHub | Tier 2 | high | API or BS |
| Wikipedia | Tier 1 | high | REST API `/page/summary/{title}` |
| Hacker News | Tier 1 | high | Firebase API, fully free |
| Google News | Tier 2 | high | RSS feed, free, no key |
| Twitter/X | Tier 3 | low | Aggressive bot detection |
| LinkedIn | Tier 3 | low | Requires login + stealth |
| Any generic article | Tier 1.5 | high | trafilatura — your default |

---

## Known Limitations & Gotchas

Read this before fighting a site — most of these are paired with the per-site table above.

1. **Major publishers (Wiley / Science / PNAS / Elsevier):** almost always return 403; APIs are the only practical route. Use Unpaywall to find OA versions.
2. **Nature.com:** do NOT use `networkidle` with Playwright — it will time out. Use `domcontentloaded`.
3. **Google Scholar:** rapid requests get IP-blocked; pace with `time.sleep(2)`. Better: use SerpAPI/Serper.
4. **Semantic Scholar API:** needs free API key for usable rate limits (otherwise 100 req/5min).
5. **PDF links on arXiv:** the abstract page does NOT contain a direct PDF link. Derive: `/pdf/{ID}.pdf`.
6. **Jina Reader:** 20 req/min free tier. For heavy use, get an API key.
7. **Reddit:** must include a descriptive User-Agent header. Rate limit: ~60 req/min.
8. **Medium paywall:** trafilatura often extracts full text even from paywalled articles. If not, try Jina Reader.
9. **DuckDuckGo search:** no API key needed but rate-limited. Use responsibly.
10. **CORE API:** requires free API key from https://core.ac.uk/services/api for reasonable limits.

---

## Real-Time Data Quick Reference

| Source | Method | Free? | Endpoint / Pattern |
|--------|--------|-------|-------------------|
| **Google News** | RSS | ✅ | `https://news.google.com/rss/search?q={query}` |
| **Reddit** | JSON API | ✅ | Append `.json` to any URL + User-Agent header |
| **Hacker News** | Firebase API | ✅ | `https://hacker-news.firebaseio.com/v0/topstories.json` |
| **GitHub** | REST API | ✅* | `https://api.github.com/search/repositories?q={q}&sort=stars` |
| **Stack Exchange** | API | ✅ | `https://api.stackexchange.com/2.3/search?intitle={q}&site=stackoverflow` |
| **Wikipedia** | REST API | ✅ | `https://en.wikipedia.org/api/rest_v1/page/summary/{title}` |
| **Wayback Machine** | API | ✅ | `https://archive.org/wayback/available?url={url}` |
| **Stock data** | yfinance | ✅ | `yf.Ticker("AAPL").history(period="1mo")` |
| **Weather** | Open-Meteo | ✅ | `https://api.open-meteo.com/v1/forecast?...` |

→ Deep-dive: [reference/realtime-data.md](../realtime-data.md)
→ News/RSS guide: [reference/news-and-rss.md](../news-and-rss.md)
→ Social media: [reference/social-media.md](../social-media.md)

---
