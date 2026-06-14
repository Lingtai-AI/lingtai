# Authorized Institutional Publisher Access (Tier 5b)

> **Read this when** OA routes (arXiv / Unpaywall / Europe PMC / CORE) and the
> Tier-5 publisher-page extractor all miss, the paper is *not* open access, and
> the user is running inside a network that **already** has legitimate
> subscription access to the publisher (a university/library IP range, an
> authenticated library proxy session, etc.).
>
> This tier sits **after** OA and publisher-extract, and **before** LibGen. It
> recovers PDFs that are paywalled-but-licensed-to-you, with heavy provenance,
> so an agent can honestly say: *"I tried OA first; where the user had
> authorized institutional access I used the official publisher URL with full
> provenance; I did not use LibGen / Sci-Hub / any shadow library."*

---

## What this tier IS

A conservative probe that:

1. Resolves `https://doi.org/<doi>` with a normal HTTP client and a polite
   User-Agent, following redirects to the **official publisher landing page**.
2. Parses that landing page for a publisher-provided PDF URL:
   - `<meta name="citation_pdf_url" content="...">` (the Highwire/Google
     Scholar convention most publishers emit), and/or
   - `.pdf` anchors that live on the **same official host / subdomain**.
3. Validates *before saving*:
   - final HTTP status is `200`;
   - the final (post-redirect) URL is still on the landing host, its `www.`
     alias, or a **subdomain** of it — never a sibling public-suffix domain;
   - `Content-Type` starts with `application/pdf`;
   - the response body starts with the `%PDF-` magic bytes;
   - size sanity (non-empty, under a hard cap).
4. Records full provenance: DOI, landing URL, candidate PDF URL, final URL,
   HTTP status, content type, byte length, timestamp, and `route:
   authorized_publisher`.

This is exactly the access path your browser would take if you clicked the
"PDF" button on the article page while on your institution's network. The probe
adds nothing to that — it only *follows official links you can already reach*.

## What this tier is NOT — hard boundaries

This tier **must never**:

- bypass or solve a paywall, CAPTCHA, or Cloudflare/anti-bot challenge;
- capture, store, replay, guess, or forge credentials, cookies, session
  tokens, or bearer headers;
- inject institutional-proxy URL rewriting (e.g. EZproxy host-mangling) that
  the user has not already established in their own session;
- guess sibling/alternate domains or "mirror" hosts for a publisher;
- fall back to LibGen, Sci-Hub, or any shadow library — those are a *different*
  tier with their own (stricter) gate.

If access isn't already present in the environment, this tier simply **misses**
and the ladder falls through. It is not a way to *obtain* access; it is a way to
*use access you already have*, auditably.

### Why no cookie/session handling

It is tempting to "just forward the user's library cookies." Don't build that
into the skill:

- **Credential surface.** A skill that reads, stores, or replays session
  cookies turns every fetch into a credential-handling operation. That is a
  security liability (cookies leak into manifests, logs, subagent prompts) and
  a policy liability (you are now acting as the user's authenticated agent
  against the publisher's ToS in ways the user may not have intended).
- **It is usually unnecessary.** Most institutional access is **IP-based**: if
  the process runs on the campus/library network, `requests` already resolves
  the licensed PDF with no cookie at all. That is the only mode this tier
  supports.
- **Proxy sessions are the user's to own.** If a user genuinely needs an
  authenticated EZproxy/OpenAthens session, they should establish it at the OS
  / browser / network layer and let the IP or transparent proxy carry it. The
  skill stays cookie-agnostic and never persists session state.

The single allowed concession: the probe honors an **already-exported**
`$HTTPS_PROXY` / `$HTTP_PROXY` (standard `requests` behavior). That is the
user's network configuration, not a credential the skill manufactured. The
skill does not read cookie jars, does not write them, and does not log any
`Set-Cookie` / `Authorization` values into provenance.

---

## Where it fits in the ladder

```
arXiv → Unpaywall → Europe PMC → CORE → publisher-extract (Tier 5)
                                          → authorized-publisher (Tier 5b)   ← this file
                                          → LibGen (last resort)
```

- **vs. Tier 5 (publisher-page extraction, `Download_paper`):** Tier 5 renders
  the *article HTML* into Markdown-with-LaTeX and needs Chromium + pandoc; it is
  great for math-heavy papers but currently brittle to install (issue #136).
  Tier 5b instead grabs the **publisher's own PDF** via a plain HTTP client — no
  browser, no pandoc — and is the right tool when you just need the canonical
  PDF and you already have access.
- **vs. LibGen:** LibGen is a shadow library of unclear copyright status; Tier 5b
  is the *licensed* publisher copy. Always prefer 5b. The two never share code
  or fall into each other automatically except through the normal ladder order.

---

## Using it via `fetch_paper.py`

The tier is **on by default** but only ever succeeds where licensed access
already exists, so it is safe to leave enabled:

```bash
# Normal run — authorized-publisher probe runs automatically between
# publisher-extract and LibGen.
python3 <skill-path>/scripts/fetch_paper.py 10.1007/s11214-020-00743-1

# Legal-sensitive / off-network environment: turn the probe off explicitly.
python3 <skill-path>/scripts/fetch_paper.py <doi> --no-institutional

# Belt-and-braces conservative run: OA only, no institutional probe, no LibGen.
python3 <skill-path>/scripts/fetch_paper.py <doi> --no-institutional --no-libgen
```

A successful hit writes `manifest.json` with:

```json
{
  "status": "ok",
  "tier": "authorized_publisher",
  "source": "paper.pdf",
  "route": "authorized_publisher",
  "provenance": {
    "doi": "10.1007/s11214-020-00743-1",
    "landing_url": "https://link.springer.com/article/10.1007/s11214-020-00743-1",
    "pdf_url": "https://link.springer.com/content/pdf/10.1007/s11214-020-00743-1.pdf",
    "final_url": "https://link.springer.com/content/pdf/10.1007/s11214-020-00743-1.pdf",
    "http_status": 200,
    "content_type": "application/pdf",
    "bytes": 4123456,
    "host_check": "same-host"
  }
}
```

Cookie/session values are **never** written to provenance.

---

## Host-validation rules (the core safety check)

Given the landing host `H` (the host of the final DOI-resolved URL), a candidate
PDF URL's final host `F` is accepted only if:

| Relationship | Example (`H = link.springer.com`) | Accept? |
|---|---|---|
| Exact same host | `link.springer.com` | ✅ |
| `www.` alias either direction | `www.link.springer.com` ↔ `link.springer.com` | ✅ |
| Subdomain of `H` | `cdn-pdf.link.springer.com` | ✅ |
| Registrable parent / sibling | `springer.com`, `springeropen.com` | ❌ |
| Unrelated host | `dl.example-cdn.net` | ❌ |

The rule is deliberately strict: we accept the host the user actually landed on
and things strictly *under* it, and reject everything else — including the
registrable parent — because a publisher's licensed PDF is virtually always
served from the same host that served the article page. Being conservative here
costs an occasional miss (fall through to the next tier) and buys a strong
guarantee that we never wander onto an unlicensed or spoofed host.

> Implementation note: the `_same_publisher_host()` helper in `fetch_paper.py`
> compares `H` and `F` after stripping a leading `www.` from each, then accepts
> iff `F == H` or `F.endswith("." + H)`.

---

## Manual workflow (when escaping the script)

If you need to run the probe by hand (debugging, custom queue, batch over a
project-local acquisition store):

1. **Select candidates.** Only DOIs that already failed every OA tier. Don't
   re-probe rows already `fetched`.
2. **Resolve landing.** `GET https://doi.org/<doi>`, follow redirects, keep the
   final URL → `landing_url`, its host → `H`.
3. **Find a PDF URL.** Prefer `meta[name=citation_pdf_url]`; else same-host
   `.pdf` anchors. Skip anything that fails the host rule.
4. **Dry-run first.** Record `landing_url`, `pdf_url`, status, content type,
   source — **mutate nothing**.
5. **Download only on explicit opt-in.** Re-GET the PDF URL, follow redirects,
   then gate on: status 200 **and** host rule **and** `application/pdf`
   **and** `%PDF-` magic bytes **and** size sane. Only then write the file.
6. **Promote atomically.** Write to a temp path, fsync/rename into place, and
   update queue/CSV mirrors *after* the bytes are on disk — never mark a row
   `fetched` before the PDF exists.
7. **Log provenance per attempt**, never including cookies or auth headers.

This mirrors the validated project-local pattern from the HelioSI acquisition
store (`scripts/probe_authorized_fulltext.py`) that motivated issue #140.

---

## Failure modes

| Symptom | Cause | What to do |
|---|---|---|
| Landing resolves but no `citation_pdf_url` and no same-host `.pdf` | Publisher emits the PDF link only behind JS | Miss; fall through. Optionally Tier 5 (`Download_paper`) for the article HTML. |
| PDF URL returns HTML, not `application/pdf` | You are *not* on a licensed network — got an interstitial/login page | Correct behavior: magic-byte check rejects it, tier misses. Do **not** try to parse the login page. |
| `403` / `401` on the PDF URL | No institutional access in this environment | Miss; fall through. This tier never tries to defeat the 403. |
| Final URL is a sibling/parent domain | Publisher served PDF off a different registrable domain | Host rule rejects; miss. (Conservative by design — accept the occasional false negative.) |
| `%PDF-` present but body tiny (<1 KB) | Truncated / error stub | Size check deletes it; miss. |

---

## See also

- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — the full acquisition ladder
- [publisher-page-extraction.md](publisher-page-extraction.md) — Tier 5, the HTML→Markdown extractor (issue #136 install caveat)
- [zotero-institutional-fulltext-handoff.md](zotero-institutional-fulltext-handoff.md) — Tier 6a, the human-in-the-loop Zotero handoff to try *after* this probe misses on a batch
- [libgen-fallback.md](libgen-fallback.md) — the tier *below* this one, with its own (stricter) legal gate
- [api-unpaywall.md](api-unpaywall.md) — always try OA first
