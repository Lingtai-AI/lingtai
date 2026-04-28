# Pipeline: Reference Management & Formatting (Citation Tracking)

## Objective

Format discovered paper metadata into standard citation styles (APA, BibTeX, etc.), batch-build reference libraries, and generate structured literature review documents.

## Workflow Steps

1. **Collect metadata**: Retrieve paper lists from the discovery/obtain pipeline, or batch-query via OpenAlex
2. **Standardize fields**: Unify fields from CrossRef / OpenAlex / manual input into an internal format `{title, authors, year, journal, volume, issue, pages, doi}`
3. **Format citations**: Generate citation strings in the target style (APA / BibTeX / IEEE)
4. **Batch processing**: Process dozens of papers at once, outputting as Markdown or .bib files
5. **Generate review documents**: Automatically produce literature reviews with high-impact paper rankings, temporal trends, and complete references

## Decision Tree

```
What do you need?
├── Single-paper citation formatting
│   ├── APA 7 → format_apa(paper)
│   ├── BibTeX → to_bibtex(paper)
│   └── Other formats → adjust based on APA template
│
├── Batch-build reference library
│   ├── Existing paper list → batch formatting → output file
│   └── Only search terms → OpenAlex search → formatting → output file
│
└── Generate literature review document
    ├── Existing paper list → compile_literature_review(papers, topic)
    └── Need to search first → discovery pipeline → formatting → review
```

## Code Examples

### APA 7 Formatting

```python
def format_apa(paper):
    """APA 7 format"""
    authors = paper.get("authors", [])
    year = paper.get("year", "n.d.")
    title = paper.get("title", "")
    journal = paper.get("journal", paper.get("venue", ""))
    volume = paper.get("volume", "")
    issue = paper.get("issue", "")
    pages = paper.get("pages", "")
    doi = paper.get("doi", "")

    if len(authors) == 0:
        author_str = "Unknown"
    elif len(authors) == 1:
        author_str = f"{authors[0].get('family','')}, {authors[0].get('given','')[0]}."
    elif len(authors) == 2:
        a1, a2 = authors[0], authors[1]
        author_str = (
            f"{a1.get('family','')}, {a1.get('given','')[0]}. & "
            f"{a2.get('family','')}, {a2.get('given','')[0]}."
        )
    else:
        author_str = f"{authors[0].get('family','')}, {authors[0].get('given','')[0]}., et al."

    parts = [f"{author_str} ({year}). {title}."]
    if journal:
        parts.append(f"*{journal}*")
    if volume:
        parts[-1] += f", {volume}"
    if issue:
        parts[-1] += f"({issue})"
    if pages:
        parts[-1] += f", {pages}"
    if doi:
        parts.append(f"https://doi.org/{doi}")

    return " ".join(parts)
```

### BibTeX Export

```python
def to_bibtex(paper):
    """Export to BibTeX"""
    key_parts = []
    if paper.get("authors"):
        key_parts.append(paper["authors"][0].get("family", "unknown"))
    key_parts.append(str(paper.get("year", "nd")))
    key = "".join(key_parts).lower().replace(" ", "")

    fields = {
        "title": paper.get("title", ""),
        "author": " and ".join(
            f"{a.get('family','?')}, {a.get('given','')}"
            for a in paper.get("authors", [])
        ),
        "year": str(paper.get("year", "")),
        "journal": paper.get("journal", paper.get("venue", "")),
        "volume": paper.get("volume", ""),
        "number": paper.get("issue", ""),
        "pages": paper.get("pages", ""),
        "doi": paper.get("doi", ""),
    }

    entries = [f"  {k} = {{{v}}}" for k, v in fields.items() if v]
    return f"@article{{{key},\n" + ",\n".join(entries) + "\n}"
```

### Batch-Build Reference Library

```python
import requests

def build_reference_library(query, limit=50, style="apa"):
    """
    Build a reference library from a search:
    1. Search papers via OpenAlex
    2. Standardize fields
    3. Format citations
    4. Output to file
    """
    r = requests.get(
        "https://api.openalex.org/works",
        params={
            "filter": f"title_and_abstract.search:{query}",
            "sort": "cited_by_count:desc",
            "per_page": limit
        },
        timeout=10
    ).json()

    papers = []
    for w in r.get("results", []):
        papers.append({
            "title": w.get("display_name"),
            "year": w.get("publication_year"),
            "authors": [
                {"family": a.get("author", {}).get("display_name", "").split()[-1],
                 "given": " ".join(a.get("author", {}).get("display_name", "").split()[:-1])}
                for a in w.get("authorships", [])
            ],
            "journal": w.get("host_venue", {}).get("display_name", ""),
            "citations": w.get("cited_by_count", 0),
            "doi": w.get("doi", "").replace("https://doi.org/", ""),
        })

    if style == "apa":
        formatted = [format_apa(p) for p in papers]
    elif style == "bibtex":
        formatted = [to_bibtex(p) for p in papers]
    else:
        formatted = [format_apa(p) for p in papers]

    output = f"# References — {query}\n\n" + "\n\n".join(formatted)
    with open("/tmp/references.md", "w") as f:
        f.write(output)
    return "/tmp/references.md", len(papers)
```

### Generate Literature Review Document

```python
def compile_literature_review(papers, topic):
    """Generate a structured review from a paper list"""
    from collections import Counter

    by_year = Counter(p.get("year") for p in papers if p.get("year"))
    by_citations = sorted(papers, key=lambda x: x.get("citations", 0), reverse=True)
    top_papers = by_citations[:10]

    doc = f"""# Literature Review: {topic}

## Overview
- **Total papers**: {len(papers)}
- **Publication years**: {min(by_year)} – {max(by_year)}
- **Average per year**: {len(papers) / max(len(by_year), 1):.1f} papers

## High-Impact Papers (Top 10)

| # | Title | Year | Citations |
|---|------|------|--------|
"""
    for i, p in enumerate(top_papers, 1):
        title = p.get("title", "Unknown")[:60]
        year = p.get("year", "?")
        cites = p.get("citations", 0)
        doc += f"| {i} | {title} | {year} | {cites} |\n"

    doc += "\n## Temporal Trends\n\n"
    for year in sorted(by_year):
        bar = "▓" * by_year[year]
        doc += f"- **{year}**: {bar} {by_year[year]} papers\n"

    doc += "\n## References\n\n"
    doc += "\n\n".join(format_apa(p) for p in by_citations)

    with open("/tmp/literature_review.md", "w") as f:
        f.write(doc)
    return "/tmp/literature_review.md"
```

## Failure Fallbacks

| Failure Scenario | Fallback Strategy |
|------------------|-------------------|
| OpenAlex returns no results | Use broader keywords, or retrieve from the discovery pipeline |
| Author name parsing error | Use full name as family name, leave given empty |
| BibTeX key collision | Append `_2`, `_3` suffixes |
| Incomplete fields (missing volume/issue/pages) | Skip missing fields, generate an incomplete but valid citation |
| Cross-discipline citation style differences | Default to APA; specific journal formats require manual adjustment |

## Notes

- Citation formats vary slightly across journals — always confirm the target journal's formatting requirements
- BibTeX keys must be unique; auto-generated keys may conflict with existing entries
- CrossRef and OpenAlex use different field names — standardize before formatting
- The `host_venue` field in OpenAlex may be updated to `primary_location`

## Related Pipelines

- [pipeline-discovery.md](pipeline-discovery.md) — Paper discovery (upstream)
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — Full-text retrieval (upstream)
- [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md) — Citation network & trend analysis
- [pipeline-latex-writing.md](pipeline-latex-writing.md) — **BibTeX → `.bib` file integration**: after generating BibTeX entries, append to `references.bib` and compile with `latexmk`. See that pipeline's §3 (Bibliography Management) for the full workflow.
- [decision-tree.md](decision-tree.md) — Comprehensive decision routing
