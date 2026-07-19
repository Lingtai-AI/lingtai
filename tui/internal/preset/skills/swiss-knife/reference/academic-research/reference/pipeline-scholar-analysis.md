# Pipeline: Academic Analysis & Trend Tracking (Scholar Analysis)

## Goal

Starting from a list of papers, build citation networks, track research trends, identify research gaps, evaluate scholar impact, and generate structured analysis reports.

## Workflow Steps

1. **Build Citation Network**: Starting from a single DOI, retrieve forward citations (who the paper cites) and backward citations (who cites the paper)
2. **Trend Analysis**: Aggregate publication counts and average citation counts per year for a given topic, generating a timeline
3. **Research Gap Identification**: Analyze concept tag frequency — high frequency = well-studied, low frequency = potential gaps
4. **Scholar Impact Evaluation**: Combine multiple metrics including publication count, total citations, and h-index
5. **Automatic Literature Review**: Consolidate analysis results into a structured literature review document

## Decision Tree

```
What analysis is needed?
├── Citation Network
│   ├── Forward citations (who the paper cites)
│   │   └── OpenAlex: referenced_works field
│   └── Backward citations (who cites the paper)
│       └── OpenAlex: cited_by API
│
├── Topic Trends
│   └── Query OpenAlex year by year → aggregate publication count + citations
│       └── ASCII trend chart visualization
│
├── Research Gaps
│   └── OpenAlex concepts field → concept frequency analysis
│       ├── High-frequency concepts → well-studied areas
│       └── Low-frequency concepts → potential research gaps
│
├── Scholar Impact
│   └── OpenAlex authors API → h-index / publication count / citation count
│
└── Comprehensive Review
    └── Integrate all analyses above → generate Markdown document
```

## Code Examples

All four analyses are OpenAlex `requests.get(...).json()` queries (see
[api-openalex.md](api-openalex.md)); each differs only in the filter/select and
how you aggregate `results`.

```python
import requests, time
from collections import Counter

# 1. Citation network — forward refs live on the work; backward via cites: filter
work = requests.get(f"https://api.openalex.org/works/https://doi.org/{doi}", timeout=10).json()
refs = work.get("referenced_works", [])[:20]              # forward (dereference each URL for detail)
citing = requests.get("https://api.openalex.org/works",
    params={"filter": f"cites:https://doi.org/{doi}", "per_page": 20}, timeout=10).json()["results"]

# 2. Topic trends — one query per year, aggregate count + avg cited_by_count
for year in range(2015, 2025):
    r = requests.get("https://api.openalex.org/works", params={
        "filter": f"title_and_abstract.search:{topic},publication_year:{year}",
        "per_page": 100, "select": "id,cited_by_count"}, timeout=10).json()["results"]
    stats[year] = {"count": len(r), "avg": sum(p["cited_by_count"] for p in r) / max(len(r), 1)}
    time.sleep(0.3)                                        # ASCII bar chart optional

# 3. Research gaps — count concepts (level>=1); rare concepts = candidate gaps
counts = Counter(c["display_name"]
    for w in requests.get("https://api.openalex.org/works", params={
        "filter": f"title_and_abstract.search:{topic},publication_year:2018:2024",
        "per_page": 200, "select": "concepts"}, timeout=15).json()["results"]
    for c in w.get("concepts", []) if c.get("level", 0) >= 1)

# 4. Author impact — summary_stats carries works_count / cited_by_count / h_index
authors = requests.get("https://api.openalex.org/authors",
    params={"filter": f"display_name.search:{name}", "per_page": 3}, timeout=10).json()["results"]
# author["summary_stats"]["h_index"], author["last_known_institution"]["display_name"]
```

## Failure Fallbacks

| Failure Scenario | Fallback Strategy |
|------------------|-------------------|
| OpenAlex citation data is empty | The paper may be too new to be indexed; fall back to Semantic Scholar |
| Year-by-year query timeout | Reduce the year range and increase sleep intervals |
| Concept tags are empty | Fall back to title keyword extraction as an alternative |
| Too many scholar homonyms | Add institution filter criteria |
| Low citation count (recent papers) | OpenAlex has a time lag; low citation counts for recent papers are normal |

## Notes

- OpenAlex citation data may have a time lag, resulting in lower citation counts for recent papers
- Average citation counts vary greatly across disciplines; cross-disciplinary comparisons should be made with caution
- Recursive citation network fetching requires adding delays (`time.sleep(0.3)`) to avoid rate limiting
- Filtering concepts with level ≥ 1 removes overly broad top-level concepts

## Related Pipelines

- [pipeline-discovery.md](pipeline-discovery.md) — Paper discovery (upstream)
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — Full-text retrieval (upstream)
- [pipeline-citation-tracking.md](pipeline-citation-tracking.md) — Reference formatting (downstream)
- [decision-tree.md](decision-tree.md) — Overall decision routing
