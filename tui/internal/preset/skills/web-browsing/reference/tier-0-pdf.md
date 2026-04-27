# Tier 0 — PDF Direct Download

> Part of the [web-browsing](../SKILL.md) skill.

**When it applies:** PDF direct links, DOI strings, arXiv IDs.
**Tools:** `curl` + `fitz` (no script needed).
**Speed:** ~1s.

```bash
# Direct PDF link
curl -L "https://arxiv.org/pdf/1706.03762.pdf" -o paper.pdf

# arXiv ID → derive the PDF path
curl -L "https://arxiv.org/pdf/$(echo "2401.12345" | sed 's/\.//').pdf" -o paper.pdf
```

**Python (extract PDF text):**
```python
import fitz  # pip install pymupdf
doc = fitz.open("paper.pdf")
print(doc[0].get_text()[:500])  # first-page preview
```

**Use when:** the URL contains `.pdf`, or you already have a DOI / arXiv ID.
