# Search Strategies

> Part of the [web-browsing](../SKILL.md) skill.
> Decision trees, query optimization, engine comparison, and search+extract workflows.

# search-strategies

> **Choosing the right search engine and crafting the right query.**
> Part of the `web-browsing-manual` v3.0 skill family.

---

## Decision Tree: Academic vs General Search

```
Search need arrives ────────┐
                         │
    ┌────────────────────────┼────────────────────────────┐
    │                    │                     │
  Academic            General              Real-time
  intent              intent               intent
    │                    │                     │
    ▼                    ▼                     ▼
  Is it a known     What quality?         What source?
  identifier?          │                     │
    │          ┌───────┼───────┐        ┌────┼────┐
    ▼          │       │       │        │    │    │
  DOI/arXiv  Google  Semantic  Free &   News  Social Reddit
  → Tier 1   quality meaning  no-setup  RSS   media  JSON
  API route     │       │       │        │    │    │
             Serper   Exa     Duck     Google  HN   Reddit
             /SerperAPI       DuckGo   News    API   JSON
             /GoogleCS                RSS
    │          │       │       │        │    │    │
    └──────────┴───────┴───────┴────────┴────┴────┘
                         │
                    Return results +
                    optionally extract
                    content from top hits
```

### Quick Selection

| Need | Engine | Why |
|------|--------|-----|
| Quick free search, no setup | DuckDuckGo | Zero config, decent results |
| AI agent workflow (search+answer) | Tavily | Returns answer + raw content |
| Find by meaning, not keywords | Exa | Neural/semantic search |
| Google-quality results | Serper | Google proxy, simple API |
| Privacy/independent index | Brave | Not Google/Bing |
| Unlimited self-hosted | SearXNG | Aggregates all engines |
| Academic papers | OpenAlex > Semantic Scholar > SerpAPI Scholar | See `academic-search-pipeline` |

---

## Engine Reference: Complete Code + Params

### 1. DuckDuckGo (Free, No Key)

**Best for:** Quick searches, no setup needed. Good for general queries.
**Rate limit:** Implicit (be reasonable, add delays). **Key:** Not needed.

```python
from duckduckgo_search import DDGS

def search_ddg(query, max_results=10):
    """Standard web search via DuckDuckGo."""
    with DDGS() as ddgs:
        results = list(ddgs.text(query, max_results=max_results))
        return [{"title": r["title"], "url": r["href"],
                 "snippet": r["body"]} for r in results]

def search_ddg_news(query, max_results=10):
    """News search via DuckDuckGo."""
    with DDGS() as ddgs:
        results = list(ddgs.news(query, max_results=max_results))
        return results

def search_ddg_images(query, max_results=10):
    """Image search via DuckDuckGo."""
    with DDGS() as ddgs:
        results = list(ddgs.images(query, max_results=max_results))
        return results

# Usage
results = search_ddg("transformer attention mechanism")
for r in results[:3]:
    print(f"- {r['title']}: {r['url']}")
```

**When NOT to use:** Need Google-quality results, semantic search, or structured answers.

### 2. Tavily (AI-Native Search)

**Best for:** AI agent workflows — search + extract + answer in one call.
**Rate limit:** 1000/month free. **Key:** Required (`TAVILY_API_KEY`).

```python
import os, requests

def search_tavily(query, api_key=None, max_results=5, include_answer=True,
                  search_depth="advanced"):
    """Tavily search — returns AI-generated answer + raw content.
    
    search_depth: "basic" (fast) or "advanced" (thorough)
    include_raw_content: True to get full page text (expensive!)
    """
    api_key = api_key or os.environ.get("TAVILY_API_KEY")
    if not api_key:
        raise ValueError("Need TAVILY_API_KEY")
    
    r = requests.post("https://api.tavily.com/search", json={
        "api_key": api_key,
        "query": query,
        "search_depth": search_depth,
        "include_answer": include_answer,
        "include_raw_content": True,  # Get full page content
        "max_results": max_results,
    }, timeout=30)
    r.raise_for_status()
    data = r.json()
    return {
        "answer": data.get("answer"),  # AI-generated summary
        "results": data.get("results", []),
        "count": len(data.get("results", [])),
    }

# Usage
result = search_tavily("what is retrieval-augmented generation?")
print(f"Answer: {result['answer']}")
```

**When NOT to use:** No API key available, or need pure keyword search without AI overhead.

### 3. Exa (Neural / Semantic Search)

**Best for:** Finding content by meaning. "Find papers about using transformers for code generation" works better than exact keyword matching.
**Rate limit:** 1000/month free. **Key:** Required (`EXA_API_KEY`).

```python
def search_exa(query, api_key=None, num_results=5, search_type="auto",
               include_domains=None, exclude_domains=None):
    """Exa neural search — understands meaning, not just keywords.
    
    search_type: "neural" (meaning), "keyword" (exact), "auto"
    include_domains/exclude_domains: filter by site
    """
    api_key = api_key or os.environ.get("EXA_API_KEY")
    if not api_key:
        raise ValueError("Need EXA_API_KEY")
    
    payload = {
        "query": query,
        "type": search_type,
        "numResults": num_results,
        "contents": {"text": {"maxCharacters": 3000}},  # Include content!
    }
    if include_domains:
        payload["includeDomains"] = include_domains
    if exclude_domains:
        payload["excludeDomains"] = exclude_domains
    
    r = requests.post("https://api.exa.ai/search",
        headers={"x-api-key": api_key},
        json=payload, timeout=30)
    r.raise_for_status()
    data = r.json()
    return {
        "results": data.get("results", []),
        "count": len(data.get("results", [])),
    }

# Usage: find similar pages by meaning
results = search_exa("papers on scaling laws for language models",
                     search_type="neural")
```

**When NOT to use:** Need exact keyword matching, no API key.

### 4. Serper (Google Search Proxy)

**Best for:** Google-quality results with a simple API.
**Rate limit:** 2500 free (one-time). **Key:** Required (`SERPER_API_KEY`).

```python
def search_serper(query, api_key=None, gl="us", hl="en", num=10):
    """Google search via Serper API."""
    api_key = api_key or os.environ.get("SERPER_API_KEY")
    r = requests.post("https://google.serper.dev/search",
        headers={"X-API-KEY": api_key},
        json={"q": query, "gl": gl, "hl": hl, "num": num},
        timeout=15)
    r.raise_for_status()
    return r.json()
    # Returns: organic[], knowledgeGraph, answerBox, etc.

# Scholar search
def search_serper_scholar(query, api_key=None):
    """Google Scholar via Serper."""
    r = requests.post("https://google.serper.dev/scholar",
        headers={"X-API-KEY": api_key or os.environ.get("SERPER_API_KEY")},
        json={"q": query}, timeout=15)
    r.raise_for_status()
    return r.json()
```

### 5. Brave Search

**Best for:** Independent search index, privacy-focused.
**Rate limit:** 2000/month free. **Key:** Required (`BRAVE_API_KEY`).

```python
def search_brave(query, api_key=None):
    """Brave Search — independent index."""
    api_key = api_key or os.environ.get("BRAVE_API_KEY")
    r = requests.get("https://api.search.brave.com/res/v1/web/search",
        headers={"X-Subscription-Token": api_key},
        params={"q": query}, timeout=15)
    r.raise_for_status()
    return r.json()
```

### 6. SearXNG (Self-Hosted Meta-Search)

**Best for:** Unlimited searches, privacy, aggregating multiple engines.
**Rate limit:** None (self-hosted). **Key:** Not needed.

```bash
# Start SearXNG instance
docker run -d -p 8888:8080 searxng/searxng
```

```python
def search_searxng(query, instance="http://localhost:8888", categories="general"):
    """SearXNG meta-search — aggregates Google, Bing, DDG, etc."""
    r = requests.get(f"{instance}/search",
        params={"q": query, "format": "json", "categories": categories},
        timeout=15)
    r.raise_for_status()
    return r.json()
```

---

## Query Optimization

### Operators (work on most engines)

| Operator | Example | Effect |
|----------|---------|--------|
| `site:` | `"attention" site:arxiv.org` | Restrict to specific domain |
| `filetype:` | `"deep learning" filetype:pdf` | Find specific file types |
| `""` (quotes) | `"retrieval-augmented generation"` | Exact phrase match |
| `OR` | `"transformer" OR "attention mechanism"` | Either term |
| `-` (minus) | `"apple" -fruit` | Exclude term |
| `intitle:` | `intitle:"large language model"` | Must appear in title |
| `inurl:` | `inurl:blog "machine learning"` | Must appear in URL |
| `after:/before:` | `"AI" after:2024-01-01 before:2024-12-31` | Date range |

### Query Reformulation Strategy

```python
def reformulate_query(original_query, strategy="broader"):
    """Generate alternative queries for better results."""
    queries = [original_query]
    
    if strategy == "broader":
        # Remove specific terms, use synonyms
        # e.g., "RoPE attention mechanism" → "rotary position embedding"
        pass
    elif strategy == "narrower":
        # Add domain-specific terms
        # e.g., "transformers" → "transformers NLP attention"
        pass
    elif strategy == "alternative":
        # Use different phrasing
        # e.g., "how to train LLMs" → "large language model training methods"
        pass
    
    return queries
```

---

## Pagination Pattern

```python
import time

def paginated_search(search_fn, query, max_pages=3, per_page=10, delay=1.0):
    """Generic pagination wrapper for any search API.
    
    Works with DDG, Tavily, Exa, etc. — any function that accepts
    (query, offset, limit) or equivalent.
    """
    all_results = []
    for page in range(max_pages):
        results = search_fn(query, offset=page * per_page, limit=per_page)
        if not results:
            break
        all_results.extend(results)
        if len(results) < per_page:
            break  # No more results
        if page < max_pages - 1:
            time.sleep(delay)  # Rate limit
    return all_results
```

---

## Search + Extract Unified Workflow

### Pattern 1: Tavily One-Shot

Tavily can search AND extract content in a single API call:

```python
def search_and_extract_tavily(query, api_key, max_results=5):
    """Search + extract content in one call via Tavily."""
    r = requests.post("https://api.tavily.com/search", json={
        "api_key": api_key,
        "query": query,
        "search_depth": "advanced",
        "include_answer": True,
        "include_raw_content": True,
        "max_results": max_results,
    }, timeout=60)
    data = r.json()
    
    return {
        "answer": data.get("answer"),
        "sources": [{
            "title": r.get("title"),
            "url": r.get("url"),
            "content": r.get("raw_content", "")[:3000],
            "score": r.get("score"),
        } for r in data.get("results", [])],
    }
```

### Pattern 2: Exa with Content

```python
def search_and_extract_exa(query, api_key, num_results=5):
    """Search + get full text via Exa."""
    r = requests.post("https://api.exa.ai/search",
        headers={"x-api-key": api_key},
        json={
            "query": query,
            "type": "auto",
            "numResults": num_results,
            "contents": {"text": {"maxCharacters": 5000}},
        }, timeout=30)
    data = r.json()
    return data.get("results", [])
```

### Pattern 3: Search Then Extract

```python
def search_then_extract(query, search_engine="ddg", max_extract=3):
    """Search with any engine, then extract content from top results."""
    # Step 1: Search
    if search_engine == "ddg":
        results = search_ddg(query, max_results=max_extract)
    else:
        raise ValueError(f"Unsupported engine: {search_engine}")
    
    # Step 2: Extract content from top hits
    import trafilatura
    enriched = []
    for r in results:
        try:
            html = trafilatura.fetch_url(r["url"])
            if html:
                text = trafilatura.extract(html)
                r["extracted_text"] = text[:3000] if text else None
        except Exception:
            r["extracted_text"] = None
        enriched.append(r)
        time.sleep(0.5)
    
    return enriched
```

---

## Failure Modes & Fallback Table

| Failure | Cause | Fallback |
|---------|-------|----------|
| DuckDuckGo 429 (rate limit) | Too many requests | Wait 60s, or switch to Tavily |
| Tavily quota exhausted | Monthly limit reached | Switch to DuckDuckGo |
| Exa returns empty | Too specific query | Switch to `type: "keyword"` mode |
| Serper 401 | Invalid/expired key | Check key, fallback to DDG |
| All search engines fail | Network/API outage | Use built-in `web_search` tool |
| No relevant results | Bad query | Reformulate query, try broader terms |
| Search results are spam | Low-quality sources | Add `site:` operator, use Exa for quality |

---

## Engine Comparison Matrix

| Feature | DDG | Tavily | Exa | Serper | Brave | SearXNG |
|---------|-----|--------|-----|--------|-------|---------|
| **Free tier** | ✅∞ | 1K/mo | 1K/mo | 2.5K once | 2K/mo | ✅∞ |
| **No key needed** | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| **Search quality** | Good | Good | Semantic | Google | Good | Best* |
| **Content extraction** | ❌ | ✅ | ✅ | ❌ | ❌ | ❌ |
| **AI answer** | ❌ | ✅ | ❌ | ❌ | ✅ | ❌ |
| **Self-hostable** | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| **Scholarly search** | ❌ | ❌ | Partial | ✅ | ❌ | ✅ |

*SearXNG quality depends on aggregated engines.

---

## Dependencies

```bash
pip install duckduckgo-search    # DuckDuckGo
pip install requests trafilatura # Search + extract pattern
pip install feedparser           # RSS search (optional)
```

Tavily, Exa, Serper, Brave require API keys set as environment variables:
```bash
export TAVILY_API_KEY=tvly-...
export EXA_API_KEY=...
export SERPER_API_KEY=...
export BRAVE_API_KEY=...
```

---

*This sub-skill is part of `web-browsing-manual` v3.0. For academic search specifically, see `academic-search-pipeline`. For general web content extraction, see the parent skill.*
