#!/usr/bin/env python3
"""Focused tests for the authorized-publisher tier (Tier 5b) in fetch_paper.py.

Self-contained: stubs out the `requests` dependency before import so it runs
with stdlib only (no network, no pip installs). Run with:

    python3 scripts/test_authorized_publisher.py        # stdlib unittest
    python3 -m pytest scripts/test_authorized_publisher.py -q   # if pytest present

Covers the cases issue #140 asked for:
  - citation_pdf_url extraction
  - same-host / www. alias / subdomain acceptance
  - sibling/parent-domain rejection
  - %PDF- magic-byte rejection
  - Content-Type rejection
  - successful download promotes only after the file is written
  - --no-institutional skips the tier
"""
from __future__ import annotations

import sys
import types
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory


# ── Stub `requests` so fetch_paper imports without the real dependency ────────
class _RequestException(Exception):
    pass


class _Timeout(_RequestException):
    pass


class _ConnectionError(_RequestException):
    pass


class _FakeResponse:
    """Minimal stand-in for requests.Response, usable as a context manager."""

    def __init__(self, *, status_code=200, url="", headers=None, text="", body=b""):
        self.status_code = status_code
        self.url = url
        self.headers = headers or {}
        self.text = text
        self._body = body

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False

    def raise_for_status(self):
        if self.status_code >= 400:
            raise _RequestException(f"HTTP {self.status_code}")

    def iter_content(self, chunk_size=65536):
        for i in range(0, len(self._body), chunk_size):
            yield self._body[i:i + chunk_size]

    def json(self):
        return {}


# Routing table the tests populate per-case: url -> _FakeResponse
_ROUTES: dict = {}


def _fake_get(url, *a, **k):
    if url in _ROUTES:
        return _ROUTES[url]
    # default 404
    return _FakeResponse(status_code=404, url=url, text="")


def _fake_head(url, *a, **k):
    return _FakeResponse(status_code=404, url=url)


_fake_requests = types.ModuleType("requests")
_fake_requests.get = _fake_get
_fake_requests.head = _fake_head
_fake_requests.RequestException = _RequestException
_fake_requests.Timeout = _Timeout
_fake_requests.ConnectionError = _ConnectionError
sys.modules.setdefault("requests", _fake_requests)

sys.path.insert(0, str(Path(__file__).resolve().parent))
import fetch_paper as fp  # noqa: E402


PDF_BYTES = b"%PDF-1.7\n" + b"x" * 4096
HTML_LOGIN = b"<html><body>Please log in</body></html>"


def _landing_html(pdf_url: str) -> str:
    return (
        '<html><head>'
        f'<meta name="citation_pdf_url" content="{pdf_url}">'
        '</head><body>article</body></html>'
    )


class HostCheck(unittest.TestCase):
    def test_exact_same_host(self):
        self.assertTrue(fp._same_publisher_host("link.springer.com", "link.springer.com"))

    def test_www_alias_both_directions(self):
        self.assertTrue(fp._same_publisher_host("link.springer.com", "www.link.springer.com"))
        self.assertTrue(fp._same_publisher_host("www.link.springer.com", "link.springer.com"))

    def test_subdomain_accepted(self):
        self.assertTrue(fp._same_publisher_host("link.springer.com", "cdn-pdf.link.springer.com"))

    def test_registrable_parent_rejected(self):
        self.assertFalse(fp._same_publisher_host("link.springer.com", "springer.com"))

    def test_sibling_domain_rejected(self):
        self.assertFalse(fp._same_publisher_host("link.springer.com", "springeropen.com"))

    def test_unrelated_host_rejected(self):
        self.assertFalse(fp._same_publisher_host("link.springer.com", "dl.example-cdn.net"))

    def test_empty_rejected(self):
        self.assertFalse(fp._same_publisher_host("", "link.springer.com"))
        self.assertFalse(fp._same_publisher_host("link.springer.com", ""))


class TierAuthorizedPublisher(unittest.TestCase):
    def setUp(self):
        _ROUTES.clear()
        self.doi = "10.1007/s11214-020-00743-1"
        self.landing = "https://link.springer.com/article/" + self.doi
        self.pdf = "https://link.springer.com/content/pdf/" + self.doi + ".pdf"

    def _route_landing(self, pdf_url):
        _ROUTES["https://doi.org/" + self.doi] = _FakeResponse(
            status_code=200, url=self.landing, text=_landing_html(pdf_url))

    def _route_pdf(self, *, url=None, status=200, ctype="application/pdf", body=PDF_BYTES, final_url=None):
        u = url or self.pdf
        _ROUTES[u] = _FakeResponse(
            status_code=status, url=final_url or u,
            headers={"content-type": ctype}, body=body)

    def test_happy_path_downloads_and_provenance(self):
        self._route_landing(self.pdf)
        self._route_pdf()
        with TemporaryDirectory() as d:
            out = Path(d)
            meta = {"doi": self.doi}
            path = fp.tier_authorized_publisher(meta, "me@example.com", out)
            self.assertIsNotNone(path)
            self.assertTrue(path.exists())
            self.assertEqual(path.read_bytes()[:5], b"%PDF-")
            prov = meta["_authorized_provenance"]
            self.assertEqual(prov["http_status"], 200)
            self.assertEqual(prov["content_type"], "application/pdf")
            self.assertEqual(prov["bytes"], len(PDF_BYTES))
            self.assertEqual(prov["landing_url"], self.landing)
            # no leftover temp file
            self.assertFalse((out / "paper.pdf.part").exists())

    def test_citation_pdf_url_extraction(self):
        self._route_landing(self.pdf)
        self._route_pdf()
        with TemporaryDirectory() as d:
            meta = {"doi": self.doi}
            fp.tier_authorized_publisher(meta, "me@example.com", Path(d))
            self.assertEqual(meta["_authorized_provenance"]["pdf_url"], self.pdf)

    def test_sibling_domain_pdf_rejected(self):
        sibling = "https://springeropen.com/content/pdf/x.pdf"
        self._route_landing(sibling)
        self._route_pdf(url=sibling)
        with TemporaryDirectory() as d:
            out = Path(d)
            path = fp.tier_authorized_publisher({"doi": self.doi}, "me@example.com", out)
            self.assertIsNone(path)
            self.assertFalse((out / "paper.pdf").exists())

    def test_redirect_offsite_rejected(self):
        # landing offers a same-host pdf URL, but it redirects to a sibling host
        self._route_landing(self.pdf)
        self._route_pdf(final_url="https://evil-cdn.net/x.pdf")
        with TemporaryDirectory() as d:
            out = Path(d)
            path = fp.tier_authorized_publisher({"doi": self.doi}, "me@example.com", out)
            self.assertIsNone(path)
            self.assertFalse((out / "paper.pdf").exists())

    def test_wrong_content_type_rejected(self):
        self._route_landing(self.pdf)
        self._route_pdf(ctype="text/html", body=HTML_LOGIN)
        with TemporaryDirectory() as d:
            out = Path(d)
            path = fp.tier_authorized_publisher({"doi": self.doi}, "me@example.com", out)
            self.assertIsNone(path)
            self.assertFalse((out / "paper.pdf").exists())

    def test_missing_magic_bytes_rejected(self):
        # advertises application/pdf but body is not a PDF
        self._route_landing(self.pdf)
        self._route_pdf(body=HTML_LOGIN)
        with TemporaryDirectory() as d:
            out = Path(d)
            path = fp.tier_authorized_publisher({"doi": self.doi}, "me@example.com", out)
            self.assertIsNone(path)
            self.assertFalse((out / "paper.pdf").exists())

    def test_subdomain_pdf_accepted(self):
        sub = "https://cdn-pdf.link.springer.com/x.pdf"
        self._route_landing(sub)
        self._route_pdf(url=sub)
        with TemporaryDirectory() as d:
            path = fp.tier_authorized_publisher({"doi": self.doi}, "me@example.com", Path(d))
            self.assertIsNotNone(path)

    def test_no_pdf_link_misses(self):
        _ROUTES["https://doi.org/" + self.doi] = _FakeResponse(
            status_code=200, url=self.landing,
            text="<html><head></head><body>no pdf here</body></html>")
        with TemporaryDirectory() as d:
            path = fp.tier_authorized_publisher({"doi": self.doi}, "me@example.com", Path(d))
            self.assertIsNone(path)

    def test_pdf_403_misses(self):
        self._route_landing(self.pdf)
        self._route_pdf(status=403, body=b"")
        with TemporaryDirectory() as d:
            out = Path(d)
            path = fp.tier_authorized_publisher({"doi": self.doi}, "me@example.com", out)
            self.assertIsNone(path)
            self.assertFalse((out / "paper.pdf").exists())

    def test_no_doi_misses(self):
        self.assertIsNone(
            fp.tier_authorized_publisher({}, "me@example.com", Path(".")))


class TierRegistration(unittest.TestCase):
    def test_tier_registered_before_libgen(self):
        names = [n for n, _ in fp.TIERS]
        self.assertIn("authorized_publisher", names)
        self.assertLess(names.index("authorized_publisher"), names.index("libgen"))

    def test_no_institutional_skips_tier(self):
        # When allow_institutional=False, fetch_one must not invoke the tier.
        called = {"hit": False}

        def _spy(meta, email, out_dir):
            called["hit"] = True
            return None

        orig = fp.tier_authorized_publisher
        orig_tiers = fp.TIERS
        try:
            fp.tier_authorized_publisher = _spy  # type: ignore
            fp.TIERS = [("authorized_publisher", _spy)]
            with TemporaryDirectory() as d:
                # Stub metadata resolution so no network is hit.
                _ROUTES.clear()
                meta = {"doi": "10.1234/x", "title": "t", "authors": ["A B"], "year": 2020}
                fp.resolve_metadata = lambda *a, **k: meta  # type: ignore
                fp.fetch_one("10.1234/x", Path(d), "me@example.com",
                             allow_libgen=False, allow_institutional=False)
            self.assertFalse(called["hit"], "tier ran despite --no-institutional")
        finally:
            fp.tier_authorized_publisher = orig  # type: ignore
            fp.TIERS = orig_tiers


if __name__ == "__main__":
    unittest.main(verbosity=2)
