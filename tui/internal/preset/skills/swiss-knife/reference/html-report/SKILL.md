---
name: html-report
description: >
  Nested swiss-knife reference for standalone HTML deliverables. Produce polished, self-contained HTML deliverables (research memos,
  literature maps, dashboards, audit reports, side-by-side comparisons)
  that humans open in a browser or receive as Telegram/email attachments.
  Covers a standalone HTML skeleton, math rendering with MathJax (LaTeX
  in `<pre>`/`<code>` will NOT render), artifact hygiene, and a
  validation checklist. Read when the human asks for an HTML report,
  HTML memo, HTML deliverable, or any standalone .html artifact —
  especially anything containing equations.
version: 1.0.0
tags: [html, report, mathjax, deliverable, artifact]
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# HTML Report — Standalone Artifacts for Humans

You are writing an HTML file a human will open in a browser or receive as an attachment. This skill exists because agents repeatedly ship HTML with unrendered LaTeX inside `<pre>`/`<code>` blocks and other recoverable presentation gaps. Follow the checklist below before declaring the file ready.

## When to use HTML

Reach for HTML — instead of plain text, Markdown, or a PDF — when the human will benefit from:

- Sectioned research memos or literature maps with anchor navigation.
- Dashboards or audit reports with tables/cards/callouts.
- Side-by-side comparisons (two columns, diff-style tables).
- Anything that contains rendered mathematics.
- Artifacts they will forward to others — HTML opens everywhere with no toolchain.

If the content is short and equation-free, plain text or Markdown is usually enough.

## Standalone HTML skeleton

Start from the bundled template — it already wires up typography reset, cards,
callouts, tables, anchor nav, print styles, and MathJax. Copy it, fill in the
`<!-- TODO -->` markers, drop unused sections, and save under a project-local
path (`paper/drafts/<topic>-$(date -u +%Y-%m-%d).html`):

```bash
cp ~/.lingtai-tui/utilities/swiss-knife/reference/html-report/assets/template.html \
   paper/drafts/<your-topic>-$(date -u +%Y-%m-%d).html
```

Key rules for any HTML you produce, whether from the template or freshly authored:

- **One file, no external assets except CDN scripts.** The human will move it around.
- **Inline all CSS** in a single `<style>` block in `<head>`.
- **Set `<meta charset="utf-8">` first.** Math copy-paste from LaTeX sources frequently contains non-ASCII.
- **Set a sensible `<title>`** — the human will see it in browser tabs and saved-file names.
- **Use semantic tags** (`<section>`, `<article>`, `<aside>`, `<nav>`) — they reflow better on mobile and print.
- **Print stylesheet matters.** A research memo often gets printed. Add `@media print { ... }` to hide navigation and force readable margins.

## Math rendering checklist

`<pre>` and `<code>` render LaTeX as **literal text**. They do not render math. This is the most common failure mode for HTML deliverables.

If the artifact contains equations:

1. Add MathJax in `<head>` — once, near the top:

   ```html
   <script>
   window.MathJax = {
     tex: {
       inlineMath: [['\\(', '\\)']],
       displayMath: [['\\[', '\\]'], ['$$', '$$']],
       processEscapes: true
     },
     svg: { fontCache: 'global' }
   };
   </script>
   <script defer src="https://cdn.jsdelivr.net/npm/mathjax@3/es5/tex-svg.js"></script>
   ```

2. Write inline math as `\( ... \)` and display math as `\[ ... \]`. Avoid `$...$` — it conflicts with currency in prose. Avoid `$$...$$` unless you really want a centered display block and trust the source has no stray `$`.

3. **Do not** wrap equations in `<pre>` or `<code>`. Put them in `<p>`, `<div>`, or directly in `<section>`. MathJax scans the DOM body for `\(...\)` and `\[...\]` and replaces them with rendered SVG.

4. Escape LaTeX backslashes correctly if you are templating from a programming language — `\\(` in a Python f-string becomes `\(` in the output.

5. **Online vs offline tradeoff.** CDN MathJax is one line and works everywhere with network. For fully offline artifacts (air-gapped reader, archival), either:
   - Vendor MathJax under a local asset folder and reference it locally, or
   - Pre-render equations to SVG/PNG at generation time and embed them as `<img>` (most robust, no JS needed).

   Pick the offline path explicitly if the human said "must work offline." Otherwise CDN is the default.

## Artifact hygiene

- **Write to a project-local path.** `paper/drafts/<topic>-<YYYY-MM-DD>.html` or similar. Never write HTML to `/tmp` and forget about it — the human cannot find it later.
- **Include a header block** with title, generation timestamp (use `date -u +%Y-%m-%dT%H:%M:%SZ` or equivalent), source paths the report draws from, and a one-line caveat ("draft", "automated extract", etc.).
- **Cite paths explicitly.** If the report summarizes files in the repo, link them as `<a href="../path/to/file.md">file.md</a>` so the human can jump.
- **External links must be absolute.** `https://arxiv.org/abs/2401.01234`, not bare DOIs or `arxiv:` schemes.
- **Verify the file exists** after writing — `ls -lh paper/drafts/...` — and read the head + tail to make sure the write was not truncated.

## Validation checklist (before sending)

Run through these every time, in order. They are cheap and the failure modes are common.

1. **File exists and is non-empty.** `ls -lh <path>` — sanity check the byte count is plausible (a research memo is typically 10–80 KB).
2. **If math is present, MathJax is wired in.** `grep -l "MathJax" <path>` must match. If it does not, math will not render — fix it now.
3. **No equations stuck in `<pre>` or `<code>`.** `grep -nE '<(pre|code)>.*\\\\[\(\[]' <path>` — any hits are bugs.
4. **No stray control characters from programmatic generation.** `LC_ALL=C grep -nP '[\x00-\x08\x0B\x0C\x0E-\x1F]' <path>` should be empty. If you generated HTML from a Python string with raw user input, this can silently break rendering.
5. **HTML parses.** Optional but cheap: `python3 -c "from html.parser import HTMLParser as H; p=H(); p.feed(open('<path>').read())"` — raises on malformed tags.
6. **External links are absolute.** `grep -nE 'href="(arxiv:|doi:|//)' <path>` — relative or scheme-less hrefs break when the file is downloaded.
7. **Open it locally** if a browser is available: `open <path>` (macOS) or `xdg-open <path>` (Linux). Eyeball the equations and headings.
8. **When sending via Telegram/email**, attach the actual HTML file (not paste its contents) and include the path in the message so the human can find the source later.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
