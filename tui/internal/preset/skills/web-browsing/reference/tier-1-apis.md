# Tier 1 — API Metadata Queries

> Part of the [web-browsing](../SKILL.md) skill.

**When it applies:** Known academic IDs (DOI, arXiv, PMID, PMC), or sites with free APIs.
**Tools:** `requests` (HTTP) — call APIs directly from Python.
**Speed:** ~0.5s.

### Academic APIs

| API | Endpoint | Free? | Best for |
|-----|----------|-------|----------|
| **arXiv** | `GET https://export.arxiv.org/api/query?id_list={ID}` | ✅ | CS/Physics/Math papers |
| **OpenAlex** | `GET https://api.openalex.org/works/https://doi.org/{DOI}` | ✅ | Any DOI → full metadata + citations |
| **CrossRef** | `GET https://api.crossref.org/works/{DOI}` | ✅ | DOI → metadata (title, authors, journal) |
| **Semantic Scholar** | `GET https://api.semanticscholar.org/graph/v1/paper/{DOI}?fields=...` | ✅* | AI/ML papers, citation graphs |
| **PubMed E-utilities** | `GET https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi?db=pubmed&id={PMID}` | ✅ | Biomedical literature |
| **CORE** | `GET https://api.core.ac.uk/v3/search/works?q={query}` | ✅† | Open access full text (30M+ papers) |
| **Unpaywall** | `GET https://api.unpaywall.org/v2/{DOI}?email=lingtai@users.noreply.github.com` | ✅ | Find free PDF for any paper |
| **Europe PMC** | `GET https://www.ebi.ac.uk/europepmc/webservices/rest/search?query={q}&format=json` | ✅ | Biomedical + PMC full text |
| **DBLP** | `GET https://dblp.org/search/publ/api?q={query}&format=json&h=10` | ✅ | Computer science conference papers |
| **Papers With Code** | `GET https://paperswithcode.com/api/v1/search/?q={query}` | ✅ | ML/AI papers with code + benchmarks |
| **DOAJ** | `GET https://doaj.org/api/search/articles/{query}` | ✅ | Open access journal articles |
| **Zenodo** | `GET https://zenodo.org/api/records?q={query}` | ✅ | Research data, software, datasets |
| **NASA ADS** | `GET https://ui.adsabs.harvard.edu/abs/{arxiv_id}/bibtex` | ✅ | Astrophysics/astronomy |

\* Semantic Scholar: 100 req/5min without key, 10k/day with key.
† CORE: free API key at https://core.ac.uk/services/api, 1000/day.

### Quick Examples

```python
import requests

# OpenAlex — most powerful, completely free
r = requests.get("https://api.openalex.org/works/https://doi.org/10.1038/s41586-023-05995-9")
data = r.json()
# Returns: title, authors, abstract, citation_count, topics, pdf_link

# Unpaywall — find free PDF for any DOI
r = requests.get(f"https://api.unpaywall.org/v2/{doi}?email=lingtai@users.noreply.github.com")
oa = r.json()
if oa.get("best_oa_location"):
    pdf_url = oa["best_oa_location"].get("url_for_pdf")

# DBLP — computer science papers
r = requests.get("https://dblp.org/search/publ/api?q=transformer+attention&format=json&h=5")
# Returns: title, authors, venue, year, DOI, URL

# Papers With Code — ML papers with implementations
r = requests.get("https://paperswithcode.com/api/v1/search/?q=vision+transformer")
# Returns: papers linked to GitHub repos + benchmark results

# Europe PMC — biomedical, includes PMC full text
r = requests.get("https://www.ebi.ac.uk/europepmc/webservices/rest/search?query=malaria+ vaccine&format=json&pageSize=5")
```

### Academic Search Pipeline: Find → Enrich → Get PDF

```python
def academic_search_pipeline(query, max_results=10):
    """Complete pipeline: search → enrich with metadata → find OA PDF."""
    # Step 1: Search via OpenAlex (best general academic search)
    r = requests.get("https://api.openalex.org/works",
                     params={"search": query, "per_page": max_results,
                             "sort": "cited_by_count:desc"})
    papers = []
    for result in r.json()["results"]:
        paper = {
            "title": result["title"],
            "doi": result.get("doi", "").replace("https://doi.org/", ""),
            "year": result.get("publication_year"),
            "citations": result.get("cited_by_count", 0),
            "authors": [a["author"]["display_name"] for a in result.get("authorships", [])],
        }
        # Step 2: Find OA PDF via Unpaywall
        if paper["doi"]:
            try:
                oa = requests.get(
                    f"https://api.unpaywall.org/v2/{paper['doi']}?email=lingtai@users.noreply.github.com",
                    timeout=5).json()
                if oa.get("best_oa_location"):
                    paper["pdf_url"] = oa["best_oa_location"].get("url_for_pdf")
                    paper["oa_url"] = oa["best_oa_location"].get("url")
            except Exception:
                pass
        papers.append(paper)
    return papers
```

### ID Resolution Chain

```
Given any identifier:
  DOI? → CrossRef / OpenAlex / Unpaywall
  arXiv ID? → arXiv API
  PMID? → PubMed E-utilities / Europe PMC
  Title only? → OpenAlex search / Semantic Scholar / DBLP (CS)
  Need PDF? → Unpaywall → arXiv (if CS) → CORE → Europe PMC (biomedical)
```

**Use when:** you have a DOI, arXiv ID, PMID, or just need metadata + abstract quickly.
