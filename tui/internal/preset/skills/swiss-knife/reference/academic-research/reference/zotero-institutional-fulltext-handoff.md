# Zotero Institutional Full-Text Handoff (Tier 6a — human-in-the-loop)

> **Read this when** every automated tier has missed — OA routes (arXiv /
> Unpaywall / Europe PMC / CORE), the Tier-5 publisher-page extractor, *and* the
> Tier-5b authorized-publisher HTTP probe — you have a **batch** of papers still
> `fetch_failed`, and the user has a **Zotero Desktop** running on a network with
> legitimate institutional access. Zotero's UI **Find Full Text / 查找全文** can
> often resolve these (it uses a broader resolver set than any HTTP endpoint an
> agent can drive), but the agent cannot robustly invoke that UI path. The
> reliable route is a **human-in-the-loop handoff**: the agent stages metadata
> into Zotero with provenance tags, the **human** clicks Find Full Text in the
> Zotero UI, and the agent harvests the resulting PDFs back into the acquisition
> store.
>
> This tier sits **after** authorized-publisher (5b) and **before** LibGen. It
> recovers paywalled-but-licensed papers using the user's own Zotero
> install — with heavy provenance — so the agent can honestly say: *"I tried OA
> and authorized-publisher first; for the rest I handed a tagged batch to the
> human's Zotero, which used its institutional access; I never bypassed a
> paywall, never drove the UI by force, and never touched credentials."*

---

## Why this is a handoff and not a tool

An agent **cannot** reliably reach Zotero's Find Full Text capability. The
boundaries below were mapped against **Zotero Desktop 9.0.3** (local API at
`127.0.0.1:23119`) and are the reason this workflow is human-in-the-loop rather
than fully automated:

| Surface | What it can do | Why it is *not* Find Full Text |
|---|---|---|
| **Local API** (`/api/users/0/items`) | Read items (GET). | Item routes are GET-only; `POST` returns `400 Bad Request` / *"Endpoint does not support method"*. You cannot create or mutate items here. |
| **Connector** `/connector/saveItems` | Create item **metadata** from DOI/URL, with tags. | Creates the row; does **not** fetch full text. This is the staging primitive we *do* use. |
| **Connector** `/connector/saveAttachmentFromResolver` + `/connector/hasAttachmentResolvers` | Resolve attachments via `Zotero.Attachments.getFileResolvers(item, ['oa','custom'], true)`. | Only `oa` + automatic `custom` resolvers — **narrower** than UI Find Full Text. In practice it returned `500 Failed to save an attachment` on samples the UI later resolved. |
| **UI Find Full Text** (`zoteroPane.js`) | Calls `Zotero.Attachments.addAvailableFiles(getSelectedItems())`, whose default `getFileResolvers()` includes `doi`, `url`, `oa`, **and** `custom`. | This is the broad path that actually finds PDFs — but it is only reachable from the **UI**, gated on selected items. No HTTP endpoint exposes it. |
| **Add by Identifier** (`lookup.js`) | `Zotero.Translate.Search().setIdentifier(...)`. | Internal call; no HTTP endpoint either. |

**Do not** try to bridge that gap with macOS UI automation. Direct `osascript`
menu reads fail with *"osascript is not allowed assistive access (-1719)"*, and
even a helper app that passes selftest/activate hits
*"ERROR -1743: Not authorized to send Apple events to System Events"* during menu
traversal. Fighting TCC/Accessibility burned many turns in a production sprint
for no durable gain. **The human clicking one menu item is faster, more robust,
and within policy.** See "Hard boundaries" below.

---

## Hard boundaries — what this workflow must NEVER do

This tier is staging + harvesting around a **human's** UI action. It **must
never**:

- drive the Zotero UI by force — no AppleScript/Accessibility/Apple-events menu
  traversal, no synthetic clicks, no TCC/Automation-permission bypass;
- bypass or solve a paywall, CAPTCHA, or anti-bot challenge;
- capture, store, replay, guess, or forge credentials, cookies, session tokens,
  or proxy auth — Zotero's institutional access is the **user's** session, owned
  at the OS/network layer, never handled by the skill;
- scrape an authenticated publisher session the user happens to have open;
- reach into "hidden" institutional access the user has not themselves
  established;
- fall through to LibGen/Sci-Hub automatically — that is a different, stricter
  tier.

If the human declines to run Find Full Text, or their Zotero is off-network,
this tier simply **stalls/misses** and the ladder falls through. It is a way to
*use the access the user already has, with the user in the loop* — not a way to
*obtain* access.

---

## Where it fits in the ladder

```
arXiv → Unpaywall → Europe PMC → CORE → publisher-extract (Tier 5)
                                          → authorized-publisher (Tier 5b)
                                          → Zotero institutional handoff (Tier 6a)   ← this file
                                          → LibGen (last resort)
```

- **vs. Tier 5b (authorized-publisher HTTP probe):** 5b is a *single* plain-HTTP
  GET of the publisher's own PDF — fast, fully automated, but it only sees what
  one polite request sees. Tier 6a engages Zotero's **broad** resolver set
  through the human, recovering papers 5b's single GET cannot. Prefer 5b first;
  hand off the leftovers here.
- **vs. LibGen:** LibGen is a shadow library of unclear copyright status. Tier 6a
  is the *licensed* publisher copy via the user's institution. Always prefer 6a.
- **Batch-shaped, not per-paper:** the human cost is "select N items, click once,"
  so this tier is worth it only when you have a **list** of failures to hand off
  at once — not for a single DOI (use 5b or just ask the user for that one).

---

## The workflow

### 0. Preconditions

- Zotero Desktop is **running** and on a network with institutional access.
  Confirm: `GET http://127.0.0.1:23119/connector/ping` → *"Zotero is running"*.
- You have a queue of **already-failed** rows (every OA tier + 5b missed), each
  with at least a DOI or a landing URL, plus a stable local id.
- Pick a unique, dated **batch tag**, e.g. `LingTai bulk fulltext queue 20260609`.
  It is the contract between staging and harvest.

### 1. Generate the handoff manifest

Before touching Zotero, write a manifest the human and the harvester both read.
Keep it boring and inspectable (CSV or JSON). One row per paper:

```
local_id,doi,url,title,batch_tag,staged_zotero,staged_ts,harvested,harvest_path
heliosi-04412,10.1007/s11214-020-00743-1,,On Coronal...,LingTai bulk fulltext queue 20260609,false,,false,
```

This manifest — not Zotero's internal DB — is the source of truth for *what was
asked for* and *what came back*. It also bounds the batch: the human is asked to
run Find Full Text on **exactly these tagged items**, nothing else.

### 2. Stage metadata into Zotero (agent, automated)

Create one Zotero item per row via the **Connector** (the only write path that
works), tagging each with the batch tag and a provenance note:

```bash
curl -s http://127.0.0.1:23119/connector/saveItems \
  -H 'Content-Type: application/json' \
  -H 'X-Zotero-Connector-API-Version: 3' \
  --data @- <<'JSON'
{
  "sessionID": "lingtai-handoff-20260609",
  "items": [
    {
      "itemType": "journalArticle",
      "title": "On Coronal ...",
      "DOI": "10.1007/s11214-020-00743-1",
      "url": "https://doi.org/10.1007/s11214-020-00743-1",
      "tags": [{"tag": "LingTai bulk fulltext queue 20260609"}],
      "notes": [{"note": "Staged by academic-research handoff; local_id=heliosi-04412; OA+5b missed; awaiting human Find Full Text."}]
    }
  ]
}
JSON
```

Notes:
- `/connector/saveItems` creates **metadata only** — no full text yet. That is
  expected; full text comes from the human's UI click in step 3.
- Tag **every** staged item with the same batch tag so the human can select the
  whole batch with one saved search / tag filter.
- After a successful stage, set `staged_zotero=true` + `staged_ts` in the
  manifest. Never re-stage a row already `staged_zotero=true` (idempotent).
- This is the same primitive that staged 5222 `fetch_failed` HelioSI records
  under tag `HelioSI bulk fulltext queue 20260521` in the sprint that motivated
  issue #150.

### 3. Hand off to the human (the part the agent cannot do)

Surface a short, explicit request to the user. Example:

> I've staged **N** papers into your Zotero under the tag
> **`LingTai bulk fulltext queue 20260609`** (metadata only — no PDFs yet).
> In **Zotero Desktop**, please:
> 1. Click the tag **`LingTai bulk fulltext queue 20260609`** in the tag selector
>    (bottom-left) to filter to just these items.
> 2. Select all of them (Cmd-A / Ctrl-A in the items list).
> 3. Right-click → **Find Full Text** (查找全文).
> 4. Let it finish; PDFs attach to items it could resolve.
> Tell me when it's done and I'll harvest the PDFs back with provenance.

Wait for the human to confirm. Do **not** attempt to click these menus for them.

### 4. Harvest returned PDFs (agent, automated)

After the human confirms, read back the tagged items via the Local API and copy
any **new** PDF attachments into the acquisition store with provenance:

```bash
# List items carrying the batch tag (read-only GET — the path that works).
curl -s "http://127.0.0.1:23119/api/users/0/items?tag=LingTai%20bulk%20fulltext%20queue%2020260609&limit=200"
```

For each item, walk its child attachments; for any attachment whose
`contentType` is `application/pdf` and that has a local file, locate that file
under the Zotero **storage** directory (`<Zotero data dir>/storage/<key>/`),
then for each harvested PDF:

1. **Validate before accepting** — file exists, starts with `%PDF-` magic bytes,
   size is sane (non-empty, under a hard cap). A non-PDF or stub is a miss, not
   a save.
2. **Copy, don't move** — leave the user's Zotero library intact. Write to a temp
   path, then atomically rename into `papers/{slug}/paper.pdf`.
3. **Record provenance** in the per-paper `manifest.json`:

```json
{
  "status": "ok",
  "tier": "zotero_institutional_handoff",
  "source": "paper.pdf",
  "route": "zotero_find_full_text_human",
  "provenance": {
    "doi": "10.1007/s11214-020-00743-1",
    "batch_tag": "LingTai bulk fulltext queue 20260609",
    "zotero_item_key": "ABCD1234",
    "zotero_attachment_key": "EFGH5678",
    "resolved_by": "human Find Full Text (Zotero Desktop institutional access)",
    "content_type": "application/pdf",
    "bytes": 4123456,
    "harvested_ts": "2026-06-09T12:00:00Z"
  }
}
```

4. **Update the handoff manifest** — set `harvested=true` + `harvest_path` only
   *after* the bytes are on disk. Rows the human's Zotero could not resolve stay
   `harvested=false` and fall through to LibGen (last resort) or stay unfetched.

No cookie, session token, proxy auth, or `Authorization`/`Set-Cookie` value is
ever read from Zotero or written into provenance. `resolved_by` records that a
**human** ran Find Full Text — that is the honest attribution.

---

## Provenance & limitations

- **Attribution is "human Find Full Text," not "agent fetch."** The PDF was
  resolved by the user's Zotero using the user's institutional access, with the
  user clicking the button. Record exactly that.
- **Coverage is partial and silent.** Find Full Text resolves *some* items, not
  all. The harvester must reconcile against the manifest and **`log()` what did
  not come back** — never imply the whole batch succeeded.
- **No transformation of content.** The PDF is the publisher's licensed copy,
  copied verbatim; the skill does not re-render, OCR, or strip it.
- **The user's library is read-only to us.** We copy attachments out; we never
  delete, move, or rewrite the user's Zotero items or storage.

---

## Validation checklist

Before marking any row `harvested`:

- [ ] The harvested file starts with `%PDF-` and is non-empty / under the cap.
- [ ] The `local_id` ↔ `zotero_item_key` ↔ DOI mapping is consistent with the
      manifest (you harvested the paper you asked for, not a tag collision).
- [ ] `manifest.json` provenance names the **batch tag**, the Zotero keys, and
      `resolved_by: human Find Full Text`.
- [ ] The copy is atomic (temp + rename) and the original Zotero file is
      untouched.
- [ ] Rows Zotero could not resolve are still `harvested=false` and surfaced to
      the user, not silently dropped.

---

## Failure modes

| Symptom | Cause | What to do |
|---|---|---|
| `/connector/ping` not "Zotero is running" | Zotero Desktop closed or different port | Ask the user to open Zotero; do not proceed. |
| `POST /api/users/0/items` → `400 / "Endpoint does not support method"` | Local API item routes are GET-only | Use `/connector/saveItems` to stage; never POST to the Local API. |
| `/connector/saveAttachmentFromResolver` → `500 Failed to save an attachment` | Connector resolver set (`oa`+`custom`) is narrower than UI | Expected — that's *why* this is a human handoff. Don't loop on it; hand off to the human. |
| `osascript ... -1719` / `-1743` on menu automation | macOS TCC / Accessibility / Apple-events blocks | **Stop.** Do not pursue UI automation. The human clicks Find Full Text. |
| Item tagged but no PDF child after the human ran it | Find Full Text could not resolve that paper | Leave `harvested=false`; surface it; fall through to LibGen or leave unfetched. |
| Harvested file is HTML/stub, not `%PDF-` | Login/interstitial saved instead of a PDF | Magic-byte check rejects it; treat as unresolved. |

---

## Optional future toolization (not in scope here)

The clean long-term fix is a small Zotero **plugin or local endpoint** that
exposes `Zotero.Attachments.addAvailableFiles(items)` over HTTP for *tagged,
batched* items — turning step 3's human click into an audited API call **without
any UI automation or TCC bypass**. Until that exists, the human-in-the-loop
handoff above is the supported route. (Tracked as the "optional future MCP/tool"
in issue #150.)

---

## See also

- [authorized-publisher-access.md](authorized-publisher-access.md) — Tier 5b, the automated HTTP probe to try *before* this handoff
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — the full acquisition ladder
- [libgen-fallback.md](libgen-fallback.md) — the tier *below* this one, with its own (stricter) legal gate
- [api-unpaywall.md](api-unpaywall.md) — always try OA first
