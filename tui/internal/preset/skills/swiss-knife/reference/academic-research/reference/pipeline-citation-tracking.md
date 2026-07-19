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

Standardize to `{title, authors:[{family,given}], year, journal, volume, issue,
pages, doi}` first, then format.

**APA 7** — `{author_str} ({year}). {title}. *{journal}*, {volume}({issue}),
{pages}. https://doi.org/{doi}` where `author_str` is `Family, G.` for one, joined
with ` & ` for two, and `Family, G., et al.` for 3+. Omit empty trailing fields.

**BibTeX** — key = `{first_author_family}{year}` (lowercased, spaces stripped);
emit only non-empty fields:

```python
def to_bibtex(p):
    key = (p["authors"][0].get("family", "unknown") + str(p.get("year", "nd"))).lower().replace(" ", "")
    fields = {"title": p.get("title", ""),
              "author": " and ".join(f"{a.get('family','?')}, {a.get('given','')}" for a in p.get("authors", [])),
              "year": str(p.get("year", "")), "journal": p.get("journal", ""),
              "volume": p.get("volume", ""), "number": p.get("issue", ""),
              "pages": p.get("pages", ""), "doi": p.get("doi", "")}
    return f"@article{{{key},\n" + ",\n".join(f"  {k} = {{{v}}}" for k, v in fields.items() if v) + "\n}"
```

**Batch-build a library** — OpenAlex search (`title_and_abstract.search`,
`sort=cited_by_count:desc`), then map each work: split `authorships[].author.
display_name` into given/family (last token = family), `host_venue.display_name`
→ journal (OpenAlex may rename this to `primary_location`), strip the `doi:` URL
prefix. Format each with the target style and write to a `.md`/`.bib` file.

**Literature review doc** — from a paper list: sort by citations for a Top-10
table, `Counter` on year for a temporal-trend bar list, then an APA references
section.

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
