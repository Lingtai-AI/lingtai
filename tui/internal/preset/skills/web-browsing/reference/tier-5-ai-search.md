# Tier 5 — AI-Native Search (Tavily / Exa)

> Part of the [web-browsing](../SKILL.md) skill.
> See also: [search-strategies.md](./search-strategies.md) for comprehensive search strategy guidance.

**When it applies:** You need to *discover* content, not extract a known URL. AI-native search returns clean content with the results.
**Tools:** `requests` (Tavily/Exa APIs).
**Speed:** ~3-5s.
**Cost:** Tavily 1000 req/month free, Exa 1000 req/month free.

### Tavily — Search + Extract + Answer in One Call

```python
import requests

def tavily_search(query, api_key, max_results=5, include_answer=True):
    """AI-native search: returns search results + AI-generated answer + page content."""
    r = requests.post("https://api.tavily.com/search", json={
        "api_key": api_key,
        "query": query,
        "search_depth": "advanced",  # "basic" or "advanced"
        "include_answer": include_answer,
        "include_raw_content": True,  # Full page content
        "max_results": max_results,
    })
    data = r.json()
    return {
        "answer": data.get("answer"),  # AI-generated answer
        "results": data.get("results", []),  # Each: title, url, content, raw_content, score
    }
```

### Exa — Neural/Semantic Search

```python
def exa_search(query, api_key, num_results=5):
    """Neural search — finds content by meaning, not keywords."""
    r = requests.post("https://api.exa.ai/search",
        headers={"x-api-key": api_key},
        json={
            "query": query,
            "type": "auto",  # "neural", "keyword", or "auto"
            "numResults": num_results,
            "contents": {"text": {"maxCharacters": 3000}},  # Extract full content
        })
    return r.json()
```

### DuckDuckGo — Free, No-Key Search (Python library)

```python
# pip install duckduckgo-search (already available)
from duckduckgo_search import DDGS

with DDGS() as ddgs:
    results = list(ddgs.text("machine learning frameworks 2025", max_results=10))
    # Also: ddgs.news(), ddgs.images(), ddgs.videos()
    for r in results:
        print(r["title"], r["href"], r["body"])
```

**Search strategy decision tree:**
```
Need search → Free & no setup? → DuckDuckGo (ddgs)
           → AI agent workflow? → Tavily (search + answer + content)
           → Semantic/meaning search? → Exa
           → Google quality? → Serper or Google Custom Search
           → Academic? → OpenAlex > Semantic Scholar > DBLP (CS)
           → News? → Google News RSS (free) or ddgs.news()
```

**Use when:** you need to search the web and get content, not just links. Tavily and Exa combine search + extraction in a single call.
