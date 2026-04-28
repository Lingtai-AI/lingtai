# OpenAlex API Reference

OpenAlex is the successor to Microsoft Academic Graph (MAG), providing comprehensive metadata for academic papers, concept classifications, and research institutions. It is completely free and requires no API key.

## API Overview

| Item | Description |
|---|---|
| Base URL | `https://api.openalex.org` |
| Authentication | No API key required (optional `mailto` parameter for higher rate limits) |
| Rate Limits | ~10 requests/sec, 1000 requests/day (without key); adding `mailto=you@example.com` increases limits |
| Response Format | JSON |
| Best Use Cases | Large-scale paper discovery, institutional analysis, topic modeling, research trend mapping |

Three core endpoints:

| Endpoint | Purpose |
|---|---|
| `/works` | Paper search and metadata |
| `/concepts` | Research concept/topic classification |
| `/institutions` | Research institution lookup |

---

## Endpoints & Parameters

### Basic Query — Works (Paper Search)

**Endpoint**: `GET https://api.openalex.org/works`

| Parameter | Description | Example |
|---|---|---|
| `search` | Full-text search | `search=transformer architecture` |
| `search.title` | Search titles only | `search.title=attention` |
| `search.author` | Search by author name | `search.author=vaswani` |
| `filter` | Structured filtering | `filter=publication_year:2020` |
| `per-page` | Results per page (max 200) | `per-page=10` |
| `page` | Page number | `page=2` |
| `select` | Fields to return | `select=title,authorships,publication_year` |
| `sort` | Sort field | `sort=cited_by_count:desc` |

**Common filter values**:

```
publication_year:2020                # Single year
publication_year:2017-2020           # Year range
authorships.author.id:A5101082644    # By author ID
authorships.institutions.id:I145311948  # By institution ID
primary_location.source.id:S2764280280 # By journal
topics.id:T10038                     # By topic
concepts.id:C119857082               # By concept
is_oa:true                           # Open access only
cited_by_count:>1000                 # Citation count filter (use > prefix, not + suffix)
```

**Available return fields**: `id`, `title`, `display_name`, `authorships`, `publication_year`, `publication_date`, `type`, `open_access`, `cited_by_count`, `doi`, `primary_location`, `source`, `topics`, `classifications`, `keywords`, `funding`, `institutions`, `related_works`

### Concept Classification — Concepts (Topic Taxonomy)

**Endpoint**: `GET https://api.openalex.org/concepts`

| Parameter | Description | Example |
|---|---|---|
| `search` | Search concepts | `search=machine learning` |
| `per-page` | Results per page | `per-page=5` |
| `filter` | Structured filtering | `filter=level:1` |
| `select` | Fields to return | `select=display_name,level,works_count` |

**Concept levels** (5-level hierarchy):

| Level | Meaning | Example |
|---|---|---|
| Level 0 | Broad domain | Computer Science |
| Level 1 | Sub-domain | Machine learning |
| Level 2 | Narrower sub-domain | — |
| Level 3 | Specific topic | — |
| Level 4 | Very specific topic | — |

Each concept returns: `id`, `display_name`, `level`, `works_count`, `cited_by_count`, `description`, `ancestors` (parent concept chain)

### Institution Lookup — Institutions (Research Institutions)

**Endpoint**: `GET https://api.openalex.org/institutions`

| Parameter | Description | Example |
|---|---|---|
| `search` | Search institutions | `search=Stanford` |
| `per-page` | Results per page | `per-page=5` |
| `filter` | Structured filtering | `filter=country_code:US` |
| `select` | Fields to return | `select=display_name,country_code,works_count` |

**Institution return fields**: `id`, `display_name`, `country_code`, `type`, `works_count`, `cited_by_count`, `summary_stats` (includes `h_index`, `2yr_mean_citedness`)

**Filter institutions by country**: `filter=country_code:US`

---

## Code Examples

### Paper Search

```python
import requests

def search_openalex(query, per_page=10, select='title,authorships,publication_year,cited_by_count'):
    """Search OpenAlex papers."""
    url = "https://api.openalex.org/works"
    params = {"search": query, "per-page": per_page, "select": select}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

papers = search_openalex("attention is all you need", per_page=5)
for p in papers:
    print(f"Title: {p['title']}")
    print(f"Year: {p['publication_year']}, Cited by: {p['cited_by_count']}")
    for a in p.get('authorships', [])[:3]:
        inst = a['institutions'][0]['display_name'] if a.get('institutions') else 'N/A'
        print(f"  - {a['author']['display_name']} ({inst})")
    print("---")
```

### Filter Papers by Author/Institution

```python
def get_works_by_author(author_id, per_page=10):
    """Retrieve paper list by OpenAlex author ID."""
    url = "https://api.openalex.org/works"
    params = {
        "filter": f"authorships.author.id:{author_id}",
        "per-page": per_page,
        "select": "title,publication_year,cited_by_count"
    }
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

def get_works_by_institution(inst_id, per_page=10):
    """Retrieve highly cited papers from a given institution."""
    url = "https://api.openalex.org/works"
    params = {
        "filter": f"authorships.institutions.id:{inst_id}",
        "per-page": per_page,
        "sort": "cited_by_count:desc",
        "select": "title,publication_year,cited_by_count,authorships"
    }
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]
```

### Concept Search and Paper Association

```python
def search_concepts(query, per_page=5):
    """Search research concepts/topics."""
    url = "https://api.openalex.org/concepts"
    params = {"search": query, "per-page": per_page}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

def get_concept_works(concept_id, per_page=10):
    """Retrieve highly cited papers under a given concept."""
    url = "https://api.openalex.org/works"
    params = {
        "filter": f"concepts.id:{concept_id}",
        "per-page": per_page,
        "sort": "cited_by_count:desc",
        "select": "title,publication_year,cited_by_count"
    }
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

# Example: search concept → retrieve its papers
concepts = search_concepts("transformer architecture")
for c in concepts:
    print(f"[Level {c['level']}] {c['display_name']} — {c['works_count']:,} papers")

ml_id = "C119857082"  # Machine learning
papers = get_concept_works(ml_id)
for p in papers[:5]:
    print(f"{p['title'][:60]}... ({p['publication_year']})")
```

### Institution Search and Statistics

```python
def search_institutions(query, per_page=5):
    """Search research institutions."""
    url = "https://api.openalex.org/institutions"
    params = {"search": query, "per-page": per_page}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

def get_institution_detail(inst_id):
    """Retrieve institution details."""
    url = f"https://api.openalex.org/institutions/{inst_id}"
    r = requests.get(url, timeout=10)
    r.raise_for_status()
    return r.json()

# Example
insts = search_institutions("Stanford University", per_page=3)
for i in insts:
    print(f"{i['display_name']} ({i['country_code']})")
    print(f"  Works: {i['works_count']:,}, Citations: {i['cited_by_count']:,}")
    stats = i.get('summary_stats', {})
    print(f"  h-index: {stats.get('h_index', 'N/A')}")
```

---

## Response Format

All endpoints return a uniform JSON structure:

```json
{
  "meta": {
    "count": 394212,
    "per_page": 10,
    "page": 1
  },
  "results": [
    {
      "id": "https://openalex.org/W123456789",
      "title": "...",
      "authorships": [
        {
          "author_position": "first",
          "author": {"id": "A5101082644", "display_name": "..."},
          "institutions": [{"display_name": "...", "country_code": "US"}]
        }
      ],
      "publication_year": 2022,
      "cited_by_count": 892,
      "doi": "https://doi.org/10...."
    }
  ]
}
```

Single-record queries (e.g., `/institutions/I97018004`) return the object directly without the `meta`/`results` wrapper.

---

## Rate Limits

| Scenario | Limit |
|---|---|
| No parameters | ~10 req/s, 1000 req/day |
| With `mailto=you@example.com` | Higher rate limits (recommended) |
| Response status code | HTTP 429 = rate limit exceeded |

It is recommended to include `mailto` in your request parameters: `?search=...&mailto=you@example.com`

---

## Error Handling

```python
import requests, time

def openalex_get(url, params, retries=3, delay=2):
    """OpenAlex request with retry logic."""
    for attempt in range(retries):
        r = requests.get(url, params=params, timeout=15)
        if r.status_code == 200:
            return r.json()
        elif r.status_code == 429:
            wait = delay * (attempt + 1)
            print(f"Rate limited, waiting {wait}s...")
            time.sleep(wait)
        else:
            raise Exception(f"OpenAlex error {r.status_code}: {r.text[:200]}")
    raise Exception(f"Max retries exceeded for {url}")
```

---

## Related APIs

- → See [api-semantic-scholar.md](api-semantic-scholar.md) — Semantic Scholar paper/author lookup (stronger citation network)
- → See [api-core.md](api-core.md) — CORE open access paper full-text download
- → See [api-crossref.md](api-crossref.md) — CrossRef DOI metadata lookup
