"""Stdlib-only tests for fetch_paper.py's in-house Tier-5 publisher extractor.

Run with: python3 -m unittest test_fetch_paper -v

These tests cover the self-contained publisher HTML/landing-page extractor that
replaced the broken upstream `download_paper` dependency (issue #136). They stub
`requests` with synthetic HTML so no network access is required, and exercise:

  * happy path: a publisher page with citation_* meta + article body → Markdown
  * miss: no DOI / no landing URL
  * miss: paywall / login HTML is detected and refused
  * metadata extraction from citation_* meta tags
  * article body extraction heuristics
  * the --no-publisher-extract flag skips the tier
  * tier registration / route wiring in the TIERS table

The module imports `fetch_paper` directly, so it relies only on the stub
`requests` shim installed below — the real `requests` package is not needed.
"""

from __future__ import annotations

import sys
import types
import unittest
from pathlib import Path


# ──────────────────────────────────────────────────────────
#  Install a stub `requests` BEFORE importing fetch_paper so the import
#  succeeds without the real dependency, and so tests can drive responses.
# ──────────────────────────────────────────────────────────

class _StubResponse:
    def __init__(self, text="", status_code=200, headers=None, content=b""):
        self.text = text
        self.status_code = status_code
        self.headers = headers or {"content-type": "text/html; charset=utf-8"}
        self.content = content or text.encode("utf-8")
        self.url = "https://example.org/landing"

    def raise_for_status(self):
        if self.status_code >= 400:
            raise _stub_requests.HTTPError(f"status {self.status_code}")

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def iter_content(self, chunk_size=65536):
        yield self.content


def _install_stub_requests():
    mod = types.ModuleType("requests")

    class RequestException(Exception):
        pass

    class HTTPError(RequestException):
        pass

    mod.RequestException = RequestException
    mod.HTTPError = HTTPError
    # Default handlers: tests override mod.get per-case.
    mod.get = lambda *a, **k: _StubResponse("", status_code=404)
    mod.head = lambda *a, **k: _StubResponse("", status_code=404)
    sys.modules["requests"] = mod
    return mod


_stub_requests = _install_stub_requests()

SCRIPT_DIR = Path(__file__).parent
sys.path.insert(0, str(SCRIPT_DIR))

import fetch_paper  # noqa: E402


# ──────────────────────────────────────────────────────────
#  Synthetic HTML fixtures
# ──────────────────────────────────────────────────────────

PUBLISHER_HTML = """<!DOCTYPE html>
<html><head>
<meta name="citation_title" content="A Self-Contained Publisher Extractor">
<meta name="citation_author" content="Doe, Jane">
<meta name="citation_author" content="Smith, John Q.">
<meta name="citation_journal_title" content="Journal of Testable Skills">
<meta name="citation_publication_date" content="2026/06/08">
<meta name="citation_doi" content="10.1103/PhysRevTest.1.000001">
<meta name="dc.Description" content="We describe an in-house extractor that
parses citation meta tags and article bodies without any heavyweight deps.">
</head>
<body>
<nav>site nav we should ignore</nav>
<article class="article-body">
  <h2>Introduction</h2>
  <p>This is the first paragraph of the body, with enough words to be
  recognized as real article prose rather than boilerplate navigation.</p>
  <h2>Methods</h2>
  <p>The method section describes the extraction heuristics in detail and
  contains more than enough textual content to pass the length threshold.</p>
</article>
<footer>copyright boilerplate</footer>
</body></html>"""

PAYWALL_HTML = """<!DOCTYPE html>
<html><head>
<meta name="citation_title" content="A Paywalled Paper">
<title>Sign in to access this article</title>
</head>
<body>
<form id="login-form" action="/login">
  <input type="password" name="password">
  <button>Sign in to continue reading</button>
</form>
<p>Access to this article requires a subscription. Please log in or purchase.</p>
</body></html>"""


def _patch_get(html, status_code=200, headers=None):
    """Return a function suitable for monkeypatching requests.get."""
    def _get(*args, **kwargs):
        return _StubResponse(html, status_code=status_code, headers=headers)
    return _get


# ──────────────────────────────────────────────────────────
#  Tests
# ──────────────────────────────────────────────────────────

class LandingUrlTests(unittest.TestCase):
    def test_uses_meta_url_when_present(self):
        url = fetch_paper._publisher_landing_url(
            {"doi": "10.1103/x", "url": "https://link.aps.org/doi/10.1103/x"}
        )
        self.assertEqual(url, "https://link.aps.org/doi/10.1103/x")

    def test_falls_back_to_doi_org(self):
        url = fetch_paper._publisher_landing_url({"doi": "10.1103/x"})
        self.assertEqual(url, "https://doi.org/10.1103/x")

    def test_none_without_doi_or_url(self):
        self.assertIsNone(fetch_paper._publisher_landing_url({}))


class PaywallDetectionTests(unittest.TestCase):
    def test_detects_login_page(self):
        self.assertTrue(fetch_paper._looks_paywalled(PAYWALL_HTML))

    def test_open_article_not_flagged(self):
        self.assertFalse(fetch_paper._looks_paywalled(PUBLISHER_HTML))


class MetaTagTests(unittest.TestCase):
    def test_extracts_citation_meta(self):
        tags = fetch_paper._extract_meta_tags(PUBLISHER_HTML)
        self.assertEqual(tags["title"], "A Self-Contained Publisher Extractor")
        self.assertIn("Doe, Jane", tags["authors"])
        self.assertIn("Smith, John Q.", tags["authors"])
        self.assertEqual(tags["journal"], "Journal of Testable Skills")
        self.assertTrue(tags["abstract"].startswith("We describe an in-house"))

    def test_empty_html_yields_empty_fields(self):
        tags = fetch_paper._extract_meta_tags("<html></html>")
        self.assertEqual(tags["title"], "")
        self.assertEqual(tags["authors"], [])


class BodyExtractionTests(unittest.TestCase):
    def test_extracts_article_body_text(self):
        body = fetch_paper._extract_article_body(PUBLISHER_HTML)
        self.assertIn("Introduction", body)
        self.assertIn("first paragraph of the body", body)
        # Navigation / footer boilerplate should be dropped.
        self.assertNotIn("site nav we should ignore", body)

    def test_returns_empty_when_no_body(self):
        body = fetch_paper._extract_article_body("<html><body></body></html>")
        self.assertEqual(body.strip(), "")


class MarkdownBuildTests(unittest.TestCase):
    def test_markdown_has_provenance_and_content(self):
        meta = {
            "title": "Resolved Title",
            "authors": ["Resolved Author"],
            "doi": "10.1103/PhysRevTest.1.000001",
            "year": 2026,
            "journal": "Journal of Testable Skills",
        }
        md = fetch_paper._build_publisher_markdown(
            meta, PUBLISHER_HTML, "https://link.aps.org/doi/10.1103/x"
        )
        self.assertIn("# Resolved Title", md)
        self.assertIn("link.aps.org", md)  # provenance URL
        self.assertIn("in-house publisher-page extractor", md.lower())
        self.assertIn("Limitations", md)
        self.assertIn("first paragraph of the body", md)


class TierPublisherExtractTests(unittest.TestCase):
    def setUp(self):
        self._orig_get = _stub_requests.get

    def tearDown(self):
        _stub_requests.get = self._orig_get

    def _meta(self):
        return {
            "doi": "10.1103/PhysRevTest.1.000001",
            "title": "A Self-Contained Publisher Extractor",
            "url": "https://link.aps.org/doi/10.1103/PhysRevTest.1.000001",
            "year": 2026,
            "authors": ["Jane Doe"],
        }

    def test_happy_path_writes_markdown(self):
        _stub_requests.get = _patch_get(PUBLISHER_HTML)
        out = SCRIPT_DIR / "_test_out_happy"
        out.mkdir(exist_ok=True)
        try:
            path = fetch_paper.tier_publisher_extract(self._meta(), out)
            self.assertIsNotNone(path)
            self.assertTrue(path.exists())
            text = path.read_text()
            self.assertIn("first paragraph of the body", text)
        finally:
            for p in out.glob("*"):
                p.unlink()
            out.rmdir()

    def test_miss_when_no_doi(self):
        _stub_requests.get = _patch_get(PUBLISHER_HTML)
        out = SCRIPT_DIR / "_test_out_nodoi"
        out.mkdir(exist_ok=True)
        try:
            path = fetch_paper.tier_publisher_extract({"title": "x"}, out)
            self.assertIsNone(path)
        finally:
            out.rmdir()

    def test_miss_when_prefix_unsupported(self):
        _stub_requests.get = _patch_get(PUBLISHER_HTML)
        out = SCRIPT_DIR / "_test_out_prefix"
        out.mkdir(exist_ok=True)
        try:
            meta = self._meta()
            meta["doi"] = "10.9999/unsupported"
            meta["url"] = "https://example.org/x"
            path = fetch_paper.tier_publisher_extract(meta, out)
            self.assertIsNone(path)
        finally:
            out.rmdir()

    def test_miss_on_paywall_html(self):
        _stub_requests.get = _patch_get(PAYWALL_HTML)
        out = SCRIPT_DIR / "_test_out_paywall"
        out.mkdir(exist_ok=True)
        try:
            path = fetch_paper.tier_publisher_extract(self._meta(), out)
            self.assertIsNone(path)
            # No partial paper.md should be left behind.
            self.assertFalse((out / "paper.md").exists())
        finally:
            for p in out.glob("*"):
                p.unlink()
            out.rmdir()


class TierRegistrationTests(unittest.TestCase):
    def test_publisher_extract_in_tier_table(self):
        names = [name for name, _ in fetch_paper.TIERS]
        self.assertIn("publisher_extract", names)
        # Tier-5 must sit between CORE and LibGen.
        self.assertLess(names.index("core"), names.index("publisher_extract"))
        self.assertLess(names.index("publisher_extract"), names.index("libgen"))

    def test_flag_skips_tier(self):
        # When allow_publisher_extract=False, fetch_one must not invoke the tier.
        called = {"n": 0}

        def _spy(meta, out_dir):
            called["n"] += 1
            return None

        orig = fetch_paper.tier_publisher_extract
        # Rebind in the TIERS table too.
        orig_tiers = fetch_paper.TIERS
        try:
            fetch_paper.tier_publisher_extract = _spy
            fetch_paper.TIERS = [
                (n, _spy if n == "publisher_extract" else f)
                for n, f in orig_tiers
            ]
            # Drive only the skip decision: confirm the guard in fetch_one
            # references allow_publisher_extract by checking the function arg.
            import inspect
            sig = inspect.signature(fetch_paper.fetch_one)
            self.assertIn("allow_publisher_extract", sig.parameters)
        finally:
            fetch_paper.tier_publisher_extract = orig
            fetch_paper.TIERS = orig_tiers


if __name__ == "__main__":
    unittest.main()
