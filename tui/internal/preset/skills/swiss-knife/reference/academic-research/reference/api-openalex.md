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

## Code Example

The three endpoints (`/works`, `/concepts`, `/institutions`) share one request
shape — `search=` or `filter=`, `per-page`, `select` — so one helper covers all
three; swap the URL and `filter`/`select` values per use case:

```python
import requests

def openalex_query(endpoint, search=None, filt=None, per_page=10, select=None, sort=None):
    """Generic OpenAlex query. endpoint: 'works' | 'concepts' | 'institutions'."""
    params = {"per-page": per_page}
    if search: params["search"] = search
    if filt: params["filter"] = filt
    if select: params["select"] = select
    if sort: params["sort"] = sort
    r = requests.get(f"https://api.openalex.org/{endpoint}", params=params, timeout=10)
    r.raise_for_status()
    return r.json()["results"]

# Paper search
papers = openalex_query("works", search="attention is all you need", per_page=5,
                         select="title,authorships,publication_year,cited_by_count")
for p in papers:
    print(f"{p['title']} ({p['publication_year']}) — cited by {p['cited_by_count']}")

# Filter by author/institution ID, or concept ID: swap the filter field
by_author = openalex_query("works", filt="authorships.author.id:A5101082644", per_page=10)
by_institution = openalex_query("works", filt="authorships.institutions.id:I145311948",
                                 sort="cited_by_count:desc")
by_concept = openalex_query("works", filt="concepts.id:C119857082", sort="cited_by_count:desc")

# Concepts and institutions use the same helper with a different endpoint
concepts = openalex_query("concepts", search="transformer architecture", per_page=5)
institutions = openalex_query("institutions", search="Stanford University", per_page=3)
# Single-record lookup (no wrapper): GET /institutions/{id} or /works/{id} directly
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

### Abstract Format (Inverted Index)

> **Important gotcha**: OpenAlex does **not** return `abstract` as a plain string. For copyright reasons, it returns `abstract_inverted_index` — a dictionary mapping each word to its position(s) in the original text.

```json
{
  "abstract_inverted_index": {
    "Attention": [0],
    "is": [1],
    "all": [2],
    "you": [3],
    "need": [4]
  }
}
```

If `abstract_inverted_index` is `null`, the paper has no abstract indexed. To reconstruct the plain text:

```python
def reconstruct_abstract(inverted_index):
    """Reconstruct plain-text abstract from OpenAlex inverted index."""
    if not inverted_index:
        return None
    word_positions = []
    for word, positions in inverted_index.items():
        for pos in positions:
            word_positions.append((pos, word))
    word_positions.sort()
    return " ".join(word for _, word in word_positions)
```

**Common pitfall**: Seeing an empty/missing `abstract` field and assuming the paper has no abstract — check `abstract_inverted_index` instead.

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

HTTP 429 = rate limit exceeded. Use the generic retry-with-backoff helper in
[error-handling.md](error-handling.md) (`base_delay=2` is a reasonable default
for OpenAlex).

---

## Related APIs

- → See [api-semantic-scholar.md](api-semantic-scholar.md) — Semantic Scholar paper/author lookup (stronger citation network)
- → See [api-core.md](api-core.md) — CORE open access paper full-text download
- → See [api-crossref.md](api-crossref.md) — CrossRef DOI metadata lookup
