# Tier 2 — BeautifulSoup Structured Extraction

> Part of the [web-browsing](../SKILL.md) skill.

**When it applies:** Pages needing structured data extraction (lists, tables, multi-element scraping).
**Tools:** `requests` + `beautifulsoup4` + `lxml` (all installed).
**Speed:** ~0.15s per page.

```python
import requests, re
from bs4 import BeautifulSoup
from urllib.parse import urljoin

def tier2(url):
    """Structured extraction with site-specific selectors."""
    r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
    soup = BeautifulSoup(r.text, "lxml")
    title = soup.find("title").get_text(strip=True) if soup.find("title") else None
    result = {"url": url, "method": "tier2-bs", "title": title}

    # Google Scholar (search results page)
    if "scholar.google" in url:
        papers = []
        for card in soup.select("div.gs_ri"):
            title_el = card.select_one("h3.gs_rt")
            abstract_el = card.select_one("div.gs_rs")
            link_el = card.select_one("h3.gs_rt a")
            papers.append({
                "title": title_el.get_text(strip=True) if title_el else None,
                "link": link_el["href"] if link_el else None,
                "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
            })
        result["papers"] = papers[:10]

    # Reddit (old.reddit.com or .json)
    elif "reddit.com" in url:
        for post in soup.select("div.thing.linkflair"):
            result.setdefault("posts", []).append({
                "title": post.select_one("a.title").get_text(strip=True) if post.select_one("a.title") else None,
                "score": post.select_one("div.score").get_text(strip=True) if post.select_one("div.score") else None,
                "comments": post.select_one("a.comments").get_text(strip=True) if post.select_one("a.comments") else None,
            })

    # Nature.com (og meta)
    elif "nature.com" in url:
        og_title = soup.find("meta", property="og:title")
        og_desc = soup.find("meta", property="og:description")
        citation_doi = soup.find("meta", attrs={"name": "citation_doi"})
        result.update({
            "og_title": og_title["content"] if og_title else None,
            "og_description": og_desc["content"] if og_desc else None,
            "doi": citation_doi["content"] if citation_doi else None,
        })

    # arXiv (structured metadata)
    elif "arxiv.org" in url:
        abstract_el = soup.find("blockquote", class_="abstract")
        pdf_links = re.findall(r'href="(/pdf/[^"]+\.pdf)"', r.text)
        result.update({
            "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
            "pdf_links": [urljoin(url, p) for p in pdf_links[:3]],
        })

    # Generic: extract JSON-LD structured data
    jsonld_scripts = soup.find_all("script", type="application/ld+json")
    if jsonld_scripts:
        import json
        result["jsonld"] = [json.loads(s.string) for s in jsonld_scripts if s.string]

    # Generic: extract OpenGraph + Twitter Cards
    og_data = {}
    for tag in soup.find_all("meta", attrs={"property": True}):
        if tag["property"].startswith("og:"):
            og_data[tag["property"]] = tag.get("content", "")
    for tag in soup.find_all("meta", attrs={"name": True}):
        if tag["name"].startswith("twitter:"):
            og_data[tag["name"]] = tag.get("content", "")
    if og_data:
        result["og"] = og_data

    return result
```

### Quick Structured Extraction Patterns

**Reddit JSON API** (append `.json` to any Reddit URL):
```python
import requests
r = requests.get("https://www.reddit.com/r/programming/hot.json?limit=25",
                 headers={"User-Agent": "LingTai/1.0"}, timeout=10)
posts = r.json()["data"]["children"]
for post in posts:
    print(post["data"]["title"], post["data"]["score"])
```

**Hacker News API** (completely free, no key):
```python
import requests
ids = requests.get("https://hacker-news.firebaseio.com/v0/topstories.json").json()[:10]
for id in ids:
    item = requests.get(f"https://hacker-news.firebaseio.com/v0/item/{id}.json").json()
    print(item["title"], item.get("url"))
```

**Google News RSS** (free, no key):
```python
import requests, xml.etree.ElementTree as ET
r = requests.get("https://news.google.com/rss/search?q=AI+safety&hl=en&gl=US&ceid=US:en")
root = ET.fromstring(r.text)
for item in root.findall(".//item")[:5]:
    print(item.find("title").text, item.find("link").text)
```

**GitHub Search** (60/hr without token, 5000/hr with token):
```python
r = requests.get("https://api.github.com/search/repositories?q=web+scraping&sort=stars")
for repo in r.json()["items"][:5]:
    print(repo["full_name"], repo["stargazers_count"], repo["html_url"])
```

**Wikipedia Summary** (free, no key):
```python
r = requests.get("https://en.wikipedia.org/api/rest_v1/page/summary/Web_scraping")
data = r.json()
print(data["title"], data["extract"])
```

### Key CSS Selectors

| Site | Selector | Extracts |
|------|----------|----------|
| Google Scholar | `div.gs_ri` | One paper card |
| Google Scholar | `h3.gs_rt` | Paper title |
| Google Scholar | `div.gs_rs` | Abstract / snippet |
| arXiv | `h1.title` | Title |
| arXiv | `blockquote.abstract` | Abstract |
| arXiv | `a[href*="/pdf/"]` | PDF link |
| Nature.com | `meta[property="og:title"]` | Title |
| Nature.com | `meta[name="citation_doi"]` | DOI |
| Springer | `meta[name="citation_doi"]` | DOI |
| Reddit (old) | `div.thing` | Post card |
| Reddit (old) | `a.title` | Post title |
| Medium | `article` | Article body |
| Generic | `article` / `[itemprop="articleBody"]` | Main content |

**Use when:** you need structured data from a page (lists, tables, metadata), not just raw text.
