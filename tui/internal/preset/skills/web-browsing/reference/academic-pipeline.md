# Academic Search Pipeline

> Part of the [web-browsing](../SKILL.md) skill.
> Complete pipeline for finding, enriching, and acquiring academic papers.

# academic-search-pipeline

> **Academic paper search, resolution, and acquisition — from a DOI string to a full PDF with metadata.**
> Part of the `web-browsing-manual` v3.0 skill family. For general web browsing, see the parent skill.

---

## Decision Tree: What Do You Have?

```
Input arrives ────────┐
                   │
    ┌─────────────────┼──────────────────────────────┐
    │              │                          │
  DOI string    arXiv ID                  Keywords only
 (10.xxx/...)  (2401.12345)                  │
    │              │                          │
    ▼              ▼                          ▼
 Unpaywall     arXiv API               What field?
 (free PDF?)   (metadata+PDF)               │
    │              │              ┌───────────┼───────────┐
    ▼              ▼              │           │           │
 CrossRef     Direct PDF       CS/ML      Biomedical    General
 (metadata)   download           │           │           │
    │                          ▼           ▼           ▼
    ▼                       DBLP      PubMed/     OpenAlex →
 OpenAlex                  arXiv     EuropePMC   CrossRef
 (citations,            Semantic     CORE        Semantic
  concepts)             Scholar                  Scholar
    │                      │           │           │
    └──────────────┬───────────┘           │           │
               ▼                       ▼           ▼
         Papers With Code         Zenodo      DOAJ
         (if ML+code)           (datasets)   (OA journals)
```

### Quick Routing Table

| Input Type | First API | Fallback 1 | Fallback 2 |
|-----------|-----------|------------|------------|
| DOI (`10.xxx/...`) | Unpaywall → CrossRef | OpenAlex | Semantic Scholar |
| arXiv ID (`2401.12345`) | arXiv API | Semantic Scholar | OpenAlex |
| PMID (`12345678`) | PubMed E-utilities | Europe PMC | CrossRef (by DOI) |
| Keywords + CS | DBLP | arXiv | Semantic Scholar |
| Keywords + Biomedical | PubMed | Europe PMC | CORE |
| Keywords + ML/AI | Papers With Code | arXiv | Semantic Scholar |
| Keywords + General | OpenAlex | CrossRef | Semantic Scholar |
| Keywords + Dataset | Zenodo | DOAJ | OpenAlex |

---

## PDF Acquisition Chain

The goal: get a free PDF for any paper. Try in this order:

### 1. Unpaywall (DOI → OA PDF)

**When to use:** You have a DOI. First check for open access.
**Speed:** ~0.5s | **Free:** ✅ (100k/day with email) | **Key needed:** No (just email)

```python
import requests

def unpaywall_find_pdf(doi, email="lingtai@users.noreply.github.com"):
    """Find free PDF for any paper via Unpaywall.
    
    Returns dict with pdf_url if found, None if no OA version exists.
    
    NOTE: Unpaywall requires a real-looking email address. Generic addresses
    like test@example.com or research@example.com will be rejected with 422.
    Pass your actual email, or at minimum something plausible.
    """
    url = f"https://api.unpaywall.org/v2/{doi}?email={email}"
    try:
        r = requests.get(url, timeout=15)
        r.raise_for_status()
        data = r.json()
        
        if data.get("is_oa"):
            best = data.get("best_oa_location", {})
            pdf_url = best.get("url_for_pdf") or best.get("url")
            if pdf_url:
                return {
                    "pdf_url": pdf_url,
                    "version": best.get("version"),  # publishedVersion, acceptedVersion
                    "host_type": best.get("host_type"),  # publisher, repository
                    "license": best.get("license"),
                }
        
        # Check all OA locations
        for loc in data.get("oa_locations", []):
            if loc.get("url_for_pdf"):
                return {
                    "pdf_url": loc["url_for_pdf"],
                    "version": loc.get("version"),
                    "host_type": loc.get("host_type"),
                }
        
        return None  # No OA version available
    except Exception as e:
        print(f"[Unpaywall error] {e}")
        return None
```

**When NOT to use:** No DOI available. Fall through to other methods.

### 2. arXiv (arXiv ID → PDF + Metadata)

**When to use:** CS/Physics/Math papers, arXiv ID known or discoverable.
**Speed:** ~1s | **Free:** ✅ | **Key needed:** No

```python
import xml.etree.ElementTree as ET

def arxiv_search(query, max_results=10, sort_by="relevance"):
    """Search arXiv for papers.
    
    sort_by: 'relevance', 'lastUpdatedDate', 'submittedDate'
    """
    url = "https://export.arxiv.org/api/query"
    params = {
        "search_query": query,
        "max_results": max_results,
        "sortBy": sort_by,
        "sortOrder": "descending"
    }
    try:
        r = requests.get(url, params=params, timeout=30)
        r.raise_for_status()
        root = ET.fromstring(r.text)
        ns = {"atom": "http://www.w3.org/2005/Atom", "arxiv": "http://arxiv.org/schemas/atom"}
        
        papers = []
        for entry in root.findall("atom:entry", ns):
            pdf_url = None
            for link in entry.findall("atom:link", ns):
                if link.get("title") == "pdf":
                    pdf_url = link.get("href")
            
            papers.append({
                "title": entry.find("atom:title", ns).text.strip().replace("\n", " "),
                "authors": [a.find("atom:name", ns).text for a in entry.findall("atom:author", ns)],
                "abstract": entry.find("atom:summary", ns).text.strip(),
                "pdf_url": pdf_url,
                "arxiv_id": entry.find("atom:id", ns).text.split("/abs/")[-1],
                "published": entry.find("atom:published", ns).text,
                "updated": entry.find("atom:updated", ns).text,
                "categories": [c.get("term") for c in entry.findall("atom:category", ns)],
            })
        return papers
    except Exception as e:
        print(f"[arXiv error] {e}")
        return []

def arxiv_by_id(arxiv_id):
    """Get a specific arXiv paper by ID (e.g., '2401.12345' or '2401.12345v2')."""
    url = "https://export.arxiv.org/api/query"
    params = {"id_list": arxiv_id, "max_results": 1}
    try:
        r = requests.get(url, params=params, timeout=30)
        r.raise_for_status()
        root = ET.fromstring(r.text)
        ns = {"atom": "http://www.w3.org/2005/Atom", "arxiv": "http://arxiv.org/schemas/atom"}
        papers = []
        for entry in root.findall("atom:entry", ns):
            pdf_url = None
            for link in entry.findall("atom:link", ns):
                if link.get("title") == "pdf":
                    pdf_url = link.get("href")
            papers.append({
                "title": entry.find("atom:title", ns).text.strip().replace("\n", " "),
                "authors": [a.find("atom:name", ns).text for a in entry.findall("atom:author", ns)],
                "abstract": entry.find("atom:summary", ns).text.strip(),
                "pdf_url": pdf_url,
                "arxiv_id": entry.find("atom:id", ns).text.split("/abs/")[-1],
                "published": entry.find("atom:published", ns).text,
                "updated": entry.find("atom:updated", ns).text,
                "categories": [c.get("term") for c in entry.findall("atom:category", ns)],
            })
        return papers
    except Exception as e:
        print(f"[arXiv error] {e}")
        return []
```

**Direct PDF download:** `https://arxiv.org/pdf/{ID}.pdf`

### 3. CORE (Open Access Full Text)

**When to use:** Need full text of open access papers. 30M+ OA articles.
**Speed:** ~1s | **Free:** ✅ (1000/day) | **Key needed:** Recommended (free at core.ac.uk)

```python
def core_search(query, api_key=None, limit=10):
    """Search CORE for open access papers with full text.
    
    CORE is the world's largest collection of OA research papers.
    Many entries include fullText directly in the API response.
    """
    url = "https://api.core.ac.uk/v3/search/works"
    headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
    params = {"q": query, "limit": limit}
    try:
        r = requests.get(url, headers=headers, params=params, timeout=30)
        r.raise_for_status()
        results = r.json().get("results", [])
        return [{
            "title": p.get("title"),
            "authors": p.get("authors", []),
            "abstract": p.get("abstract"),
            "fullText": p.get("fullText"),  # Often available!
            "downloadUrl": p.get("downloadUrl"),
            "year": p.get("yearPublished"),
            "doi": p.get("doi"),
            "journals": p.get("journals", []),
        } for p in results]
    except Exception as e:
        print(f"[CORE error] {e}")
        return []
```

### 4. Europe PMC (Biomedical + PMC Full Text)

**When to use:** Biomedical/life sciences papers. PMC full text XML available.
**Speed:** ~1s | **Free:** ✅ | **Key needed:** No

```python
def europe_pmc_search(query, page_size=25):
    """Search Europe PMC for biomedical literature."""
    url = "https://www.ebi.ac.uk/europepmc/webservices/rest/search"
    params = {"query": query, "format": "json", "pageSize": page_size}
    try:
        r = requests.get(url, params=params, timeout=30)
        r.raise_for_status()
        data = r.json()
        return [{
            "title": r.get("title"),
            "authors": r.get("authorString"),
            "doi": r.get("doi"),
            "pmid": r.get("pmid"),
            "pmcid": r.get("pmcid"),
            "journal": r.get("journalTitle"),
            "year": r.get("pubYear"),
            "isOpenAccess": r.get("isOpenAccess") == "Y",
        } for r in data.get("resultList", {}).get("result", [])]
    except Exception as e:
        print(f"[Europe PMC error] {e}")
        return []

def europe_pmc_fulltext(pmcid):
    """Get full text XML for a PMC article."""
    url = f"https://www.ebi.ac.uk/europepmc/webservices/rest/{pmcid}/fullTextXML"
    try:
        r = requests.get(url, timeout=30)
        if r.status_code == 200:
            return r.text  # XML full text
        return None
    except Exception:
        return None
```

---

## DOI Resolution Chain

Given a DOI, extract metadata in order of richness:

### 1. CrossRef (DOI → Metadata + BibTeX)

**When to use:** First stop for any DOI. Most comprehensive metadata.
**Speed:** ~0.5s | **Free:** ✅ | **Key needed:** No (polite to add mailto)

```python
def crossref_metadata(doi):
    """Get rich metadata for a DOI from CrossRef.
    
    Returns: title, authors, journal, year, abstract, references count, type.
    """
    url = f"https://api.crossref.org/works/{doi}"
    headers = {"User-Agent": "LingTai/3.0 (mailto:lingtai@users.noreply.github.com)"}
    try:
        r = requests.get(url, headers=headers, timeout=15)
        if r.status_code == 404:
            return None  # DOI not found
        r.raise_for_status()
        m = r.json()["message"]
        return {
            "doi": doi,
            "title": m.get("title", [""])[0],
            "authors": [f"{a.get('given', '')} {a.get('family', '')}".strip()
                        for a in m.get("author", [])],
            "journal": m.get("container-title", [""])[0],
            "year": (m.get("published-print") or m.get("published-online") or
                     {}).get("date-parts", [[None]])[0][0],
            "type": m.get("type"),
            "abstract": m.get("abstract"),
            "references_count": len(m.get("reference", [])),
            "cited_by_count": m.get("is-referenced-by-count"),
            "license": [l.get("URL") for l in m.get("license", [])],
        }
    except Exception as e:
        print(f"[CrossRef error] {e}")
        return None

def crossref_bibtex(doi):
    """Get BibTeX citation for a DOI via CrossRef content negotiation."""
    url = f"https://api.crossref.org/works/{doi}/transform/application/x-bibtex"
    headers = {"Accept": "application/x-bibtex"}
    try:
        r = requests.get(url, headers=headers, timeout=15)
        if r.status_code == 200:
            return r.text  # Raw BibTeX string
        return None
    except Exception:
        return None
```

### 2. OpenAlex (DOI → Citations + Concepts + OA Status)

**When to use:** Need citation counts, research concepts/topics, OA URL.
**Speed:** ~0.5s | **Free:** ✅ | **Key needed:** No

```python
def openalex_work(doi):
    """Get OpenAlex data for a DOI — citations, concepts, OA status."""
    url = f"https://api.openalex.org/works/https://doi.org/{doi}"
    try:
        r = requests.get(url, timeout=15)
        if r.status_code == 404:
            return None
        r.raise_for_status()
        w = r.json()
        return {
            "title": w.get("title"),
            "doi": doi,
            "authors": [a["author"]["display_name"] for a in w.get("authorships", [])],
            "cited_by_count": w.get("cited_by_count"),
            "concepts": [{"name": c["display_name"], "score": c["score"]}
                         for c in w.get("concepts", [])[:5]],
            "open_access_url": (w.get("open_access") or {}).get("oa_url"),
            "type": w.get("type"),
            "publication_year": w.get("publication_year"),
            "host_venue": (w.get("host_venue") or {}).get("display_name"),
            "referenced_works_count": len(w.get("referenced_works", [])),
        }
    except Exception as e:
        print(f"[OpenAlex error] {e}")
        return None
```

### 3. Semantic Scholar (DOI → AI Summary + Citation Graph)

**When to use:** AI/ML papers, need TLDR summary or citation graph.
**Speed:** ~1s | **Free:** ✅ (100/5min without key) | **Key needed:** Recommended

```python
def semantic_scholar_paper(doi):
    """Get Semantic Scholar data — includes AI-generated TLDR summary."""
    url = f"https://api.semanticscholar.org/graph/v1/paper/DOI:{doi}"
    params = {
        "fields": "title,authors,abstract,citationCount,referenceCount,"
                  "year,openAccessPdf,tldr,venue,publicationTypes"
    }
    try:
        r = requests.get(url, params=params, timeout=15)
        if r.status_code == 404:
            return None
        r.raise_for_status()
        p = r.json()
        return {
            "title": p.get("title"),
            "doi": doi,
            "authors": [a.get("name") for a in p.get("authors", [])],
            "abstract": p.get("abstract"),
            "citations": p.get("citationCount"),
            "references": p.get("referenceCount"),
            "year": p.get("year"),
            "venue": p.get("venue"),
            "pdf": (p.get("openAccessPdf") or {}).get("url"),
            "tldr": (p.get("tldr") or {}).get("text"),  # AI-generated summary!
            "publication_types": p.get("publicationTypes"),
        }
    except Exception as e:
        print(f"[Semantic Scholar error] {e}")
        return None
```

### 4. DBLP (CS Conference Papers)

**When to use:** Computer science conference papers specifically.
**Speed:** ~0.5s | **Free:** ✅ | **Key needed:** No

```python
def dblp_search(query, max_results=10):
    """Search DBLP for CS publications."""
    url = "https://dblp.org/search/publ/api"
    params = {"q": query, "format": "json", "h": max_results}
    try:
        r = requests.get(url, params=params, timeout=15)
        r.raise_for_status()
        hits = r.json().get("result", {}).get("hits", {}).get("hit", [])
        return [{
            "title": h.get("info", {}).get("title"),
            "authors": h.get("info", {}).get("authors", {}).get("author", []),
            "venue": h.get("info", {}).get("venue"),
            "year": h.get("info", {}).get("year"),
            "doi": h.get("info", {}).get("doi"),
            "url": h.get("info", {}).get("url"),
            "type": h.get("info", {}).get("type"),
        } for h in hits]
    except Exception as e:
        print(f"[DBLP error] {e}")
        return []
```

### 5. Papers With Code (ML/AI + Code + Benchmarks)

**When to use:** ML/AI papers that may have code implementations.
**Speed:** ~1s | **Free:** ✅ | **Key needed:** No

```python
def pwc_search(query, limit=10):
    """Search Papers With Code for ML papers with code."""
    url = "https://paperswithcode.com/api/v1/search/"
    params = {"q": query, "page": 1, "items_per_page": limit}
    try:
        r = requests.get(url, params=params, timeout=15)
        r.raise_for_status()
        return r.json().get("results", [])
    except Exception as e:
        print(f"[PWC error] {e}")
        return []
```

---

## BibTeX / Citation Export

```python
def get_bibtex(doi):
    """Get BibTeX for a DOI via CrossRef content negotiation."""
    return crossref_bibtex(doi)

def get_ris(doi):
    """Get RIS citation for a DOI."""
    url = f"https://api.crossref.org/works/{doi}/transform/application/x-research-info-systems"
    try:
        r = requests.get(url, timeout=15)
        return r.text if r.status_code == 200 else None
    except Exception:
        return None
```

---

## End-to-End Pipeline

```python
import re

def academic_pipeline(query_or_id):
    """Complete pipeline: identify input → resolve → enrich → get PDF.
    
    Accepts: DOI, arXiv ID, PMID, or keyword search query.
    Returns: dict with metadata, pdf_url (if found), and sources queried.
    """
    result = {"input": query_or_id, "metadata": {}, "pdf_url": None, "sources": []}
    
    # ── Step 1: Identify input type ──
    doi_pattern = re.compile(r'10\.\d{4,}/[^\s"\'<>)]+')
    arxiv_pattern = re.compile(r'\d{4}\.\d{4,5}(?:v\d+)?')
    
    input_type = "keywords"
    if doi_pattern.search(query_or_id):
        input_type = "doi"
        result["doi"] = doi_pattern.search(query_or_id).group(0).rstrip("/")
    elif arxiv_pattern.search(query_or_id):
        input_type = "arxiv"
        result["arxiv_id"] = arxiv_pattern.search(query_or_id).group(0)
    elif query_or_id.isdigit() and len(query_or_id) <= 8:
        input_type = "pmid"
        result["pmid"] = query_or_id
    
    # ── Step 2: Get metadata ──
    if input_type == "doi":
        doi = result["doi"]
        
        # CrossRef first (richest metadata)
        cr = crossref_metadata(doi)
        if cr:
            result["metadata"].update(cr)
            result["sources"].append("crossref")
        
        # OpenAlex (citations + concepts)
        oa = openalex_work(doi)
        if oa:
            result["metadata"]["cited_by"] = oa.get("cited_by_count")
            result["metadata"]["concepts"] = oa.get("concepts")
            result["metadata"]["oa_url"] = oa.get("open_access_url")
            result["sources"].append("openalex")
        
        # Semantic Scholar (TLDR + citation graph)
        ss = semantic_scholar_paper(doi)
        if ss:
            result["metadata"]["tldr"] = ss.get("tldr")
            result["metadata"]["ss_citations"] = ss.get("citations")
            if ss.get("pdf"):
                result["pdf_url"] = ss["pdf"]
            result["sources"].append("semantic_scholar")
        
        # ── Step 3: Try PDF acquisition ──
        if not result["pdf_url"]:
            upw = unpaywall_find_pdf(doi)
            if upw and upw.get("pdf_url"):
                result["pdf_url"] = upw["pdf_url"]
                result["metadata"]["oa_version"] = upw.get("version")
                result["sources"].append("unpaywall")
        
        if not result["pdf_url"]:
            core = core_search(f"doi:{doi}", limit=1)
            if core and core[0].get("downloadUrl"):
                result["pdf_url"] = core[0]["downloadUrl"]
                result["sources"].append("core")
        
        # BibTeX
        result["bibtex"] = get_bibtex(doi)
    
    elif input_type == "arxiv":
        papers = arxiv_by_id(result['arxiv_id'])
        if papers:
            result["metadata"] = papers[0]
            result["pdf_url"] = papers[0].get("pdf_url")
            result["sources"].append("arxiv")
    
    elif input_type == "pmid":
        # PubMed lookup
        url = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/efetch.fcgi"
        try:
            r = requests.get(url, params={
                "db": "pubmed", "id": result["pmid"],
                "rettype": "abstract", "retmode": "xml"
            }, timeout=15)
            import xml.etree.ElementTree as ET
            root = ET.fromstring(r.text)
            article = root.find(".//PubmedArticle/MedlineCitation/Article")
            if article is not None:
                result["metadata"]["title"] = article.find("ArticleTitle").text
                abstract = article.find("Abstract/AbstractText")
                if abstract is not None:
                    result["metadata"]["abstract"] = abstract.text
                result["sources"].append("pubmed")
                
                # Try to find DOI for further enrichment
                doi_el = root.find(".//ArticleId[@IdType='doi']")
                if doi_el is not None:
                    result["doi"] = doi_el.text
                    # Recurse with DOI for more metadata
        except Exception as e:
            print(f"[PubMed error] {e}")
    
    else:  # keywords
        # Try OpenAlex first (broadest)
        oa_url = f"https://api.openalex.org/works?search={query_or_id}&per_page=5"
        try:
            r = requests.get(oa_url, timeout=15)
            if r.status_code == 200:
                results = r.json().get("results", [])
                if results:
                    result["search_results"] = [{
                        "title": w.get("title"),
                        "doi": w.get("doi"),
                        "year": w.get("publication_year"),
                        "cited_by": w.get("cited_by_count"),
                        "oa_url": (w.get("open_access") or {}).get("oa_url"),
                    } for w in results]
                    result["sources"].append("openalex")
        except Exception:
            pass
        
        # Also try arXiv if CS-related
        arxiv_results = arxiv_search(query_or_id, max_results=5)
        if arxiv_results:
            result.setdefault("search_results", [])
            result["search_results"].extend([{
                "title": p["title"],
                "arxiv_id": p["arxiv_id"],
                "pdf_url": p.get("pdf_url"),
                "year": p.get("published", "")[:4],
            } for p in arxiv_results])
            result["sources"].append("arxiv")
    
    return result
```

---

## Failure Modes & Fallback Table

| Failure | Cause | Fallback |
|---------|-------|----------|
| DOI not in CrossRef | Non-standard DOI, very new paper | Try OpenAlex → Semantic Scholar |
| Unpaywall returns no OA | Paper is behind paywall | Try CORE full text → Europe PMC (if biomedical) → Playwright (Tier 3) on publisher page |
| arXiv API timeout | arXiv servers slow | Retry once (3s delay) → Semantic Scholar by title |
| Semantic Scholar 404 | Paper not indexed | CrossRef → Google Scholar via SerpAPI |
| CORE requires key | Rate limit exceeded without key | Get free key at core.ac.uk/services/api |
| All APIs fail | Obscure paper, network issues | Last resort: Playwright stealth on publisher page, or Google Scholar search |
| BibTeX not available | CrossRef content negotiation fails | Construct manually from metadata |

---

## Rate Limits Summary

| API | Free Tier | Rate Limit | Key Required? |
|-----|-----------|------------|---------------|
| Unpaywall | 100k/day | Generous | No (email param) |
| arXiv | Unlimited | Be reasonable | No |
| CrossRef | Unlimited | Be reasonable | No (add mailto) |
| OpenAlex | Unlimited | 10 req/s | No (polite pool) |
| Semantic Scholar | 100/5min | 1 req/s with key | Recommended |
| CORE | 1000/day | Higher with key | Recommended |
| Europe PMC | Unlimited | Reasonable use | No |
| DBLP | Unlimited | Reasonable use | No |
| Papers With Code | Unlimited | Reasonable use | No |
| PubMed E-utilities | 10/sec (no key) | Higher with key | No |

---

## Dependencies

```bash
# All academic search functions use only requests (standard)
pip install requests beautifulsoup4 lxml

# Optional for PDF text extraction
pip install pymupdf  # fitz - extract text from downloaded PDFs
```

---

*This sub-skill is part of `web-browsing-manual` v3.0. For general web browsing, search strategies, or stealth techniques, see the parent skill and other sub-skills.*
