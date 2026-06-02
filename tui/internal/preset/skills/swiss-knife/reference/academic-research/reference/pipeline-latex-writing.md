# Pipeline: LaTeX Academic Paper Writing

> Academic paper writing workflow — project setup, compilation, bibliography, figures, debugging, and submission verification. This is a **workflow guide**, not a LaTeX syntax tutorial.

---

## 1. Project Structure

Standard academic paper layout:

```
paper/
├── main.tex              # Root document — \input{} all sections
├── sections/             # Optional: split into files
│   ├── introduction.tex
│   ├── methodology.tex
│   ├── results.tex
│   └── conclusion.tex
├── references.bib        # BibTeX bibliography
├── figures/              # All figures — PDF preferred, PNG at ≥150 DPI
│   ├── fig1-overview.pdf
│   └── fig2-results.pdf
├── main.pdf              # Output (gitignore this)
└── main.aux, main.log    # Build artifacts (gitignore these)
```

**Root template** (`main.tex`):

```latex
\documentclass[12pt]{article}
\usepackage[T1]{fontenc}
\usepackage{amsmath,amssymb}
\usepackage{graphicx}
\usepackage[style=numeric,sorting=none,backend=biber]{biblatex}
\addbibresource{references.bib}
\usepackage{hyperref}  % load LAST
\begin{document}
\input{sections/introduction}
\input{sections/methodology}
\input{sections/results}
\printbibliography
\end{document}
```

---

## 2. Compilation Toolchain

### Recommended: `latexmk`

```bash
# One-command build (runs pdflatex + biber + pdflatex × N automatically)
latexmk -pdf main.tex

# Force full rebuild from scratch
latexmk -gg -pdf main.tex

# Clean all build artifacts
latexmk -C

# Live preview (recompile on save)
latexmk -pdf -pvc main.tex
```

**Why latexmk**: It detects which passes are needed ( bibliography → references → labels ) and runs them automatically. No more guessing "did I run pdflatex enough times?"

### Engine Selection

| Engine | When to Use | Notes |
|--------|------------|-------|
| **pdflatex** | Default — English, standard fonts | Fastest, most compatible |
| **xelatex** | CJK text (Chinese/Japanese/Korean), OpenType fonts | Use with `fontspec`, not `fontenc` |
| **lualatex** | Advanced font features, Lua scripting | Slower but most flexible |

```bash
# Use xelatex instead of pdflatex:
latexmk -xelatex main.tex
```

### Manual Build (if latexmk unavailable)

```bash
pdflatex main.tex
biber main          # process bibliography
pdflatex main.tex   # resolve citations
pdflatex main.tex   # resolve cross-references
```

---

## 3. Bibliography Management

### biblatex + biber (Recommended)

```latex
% In preamble:
\usepackage[style=numeric,sorting=none,backend=biber]{biblatex}
\addbibresource{references.bib}
```

**Key distinction**:
- **BibTeX** = old bibliography engine (`.bst` style files, `bibtex` command)
- **biblatex** = modern LaTeX package (uses `biber` backend by default)
- **biber** = the bibliography processor that biblatex calls

**Do NOT mix**: If you use `biblatex`, you must use `biber` (or explicitly set `backend=bibtex`). If you use `\bibliographystyle{}`, you're using old-style BibTeX, not biblatex.

### In-text Citations

```latex
As shown by \cite{vaswani2017}, attention mechanisms...
Multiple works \cite{smith2020,jones2021} confirm...
\textcite{vaswani2017} showed that...   % "Vaswani et al. (2017) showed..."
\parencite{vaswani2017}                 % "(Vaswani et al., 2017)"
```

### .bib File Format

```bibtex
@article{vaswani2017,
  title   = {Attention Is All You Need},
  author  = {Vaswani, Ashish and Shazeer, Noam and Parmar, Niki},
  journal = {NeurIPS},
  year    = {2017}
}
```

### Getting BibTeX from Academic APIs

| API | Method | See |
|-----|--------|-----|
| CrossRef | `GET /works/{DOI}/transform/application/x-bibtex` | [api-crossref.md](api-crossref.md) |
| NASA ADS | `GET /export/bibtex` | [api-nasa-ads.md](api-nasa-ads.md) |
| INSPIRE-HEP | `GET /literature/{id}?format=bibtex` | [api-inspire-hep.md](api-inspire-hep.md) |
| Semantic Scholar | No native BibTeX — generate from metadata | [api-semantic-scholar.md](api-semantic-scholar.md) |
| OpenAlex | No native BibTeX — generate from metadata | [api-openalex.md](api-openalex.md) |

**Workflow**: Use the citation-tracking pipeline to get BibTeX → append to `references.bib` → `latexmk` recompiles.

### Citation Styles

```latex
% Numeric [1], [2] — common in physics/engineering
\usepackage[style=numeric,backend=biber]{biblatex}

% Author-year — common in humanities/social science
\usepackage[style=authoryear,backend=biber]{biblatex}

% APA 7th edition
\usepackage[style=apa,backend=biber]{biblatex}
```

---

## 4. Figures

### Single Figure

```latex
\begin{figure}[htbp]
  \centering
  \includegraphics[width=0.8\textwidth]{figures/myfig.pdf}
  \caption{Description of the figure.}
  \label{fig:overview}
\end{figure}
```

### Multi-panel with subcaption

```latex
\usepackage{subcaption}  % in preamble

\begin{figure}[htbp]
  \centering
  \begin{subfigure}{0.48\textwidth}
    \includegraphics[width=\textwidth]{figures/panel-a.pdf}
    \caption{Panel A}
    \label{fig:panel-a}
  \end{subfigure}
  \hfill
  \begin{subfigure}{0.48\textwidth}
    \includegraphics[width=\textwidth]{figures/panel-b.pdf}
    \caption{Panel B}
    \label{fig:panel-b}
  \end{subfigure}
  \caption{Overall caption.}
  \label{fig:panels}
\end{figure}
```

### File Format Selection

| Format | Best For | Notes |
|--------|----------|-------|
| **PDF** | Plots, diagrams, vector graphics | Preferred — scalable |
| **EPS** | Legacy systems | Convert with `epstopdf` |
| **PNG** | Photos, screenshots | ≥150 DPI for print |
| **JPEG** | Photos only | Lossy — avoid for diagrams |

---

## 5. Document Class Selection

| Class | Use When | Notes |
|-------|----------|-------|
| `article` | Journal papers, short papers | Most common |
| `amsart` | Mathematics journals | AMS-specific formatting |
| `revtex4-2` | APS/AIP journals (Phys Rev, etc.) | `\documentclass{revtex4-2}` |
| `IEEEtran` | IEEE conferences/journals | `\documentclass{IEEEtran}` |
| `beamer` | Presentations/slides | `\documentclass{beamer}` |
| Custom (e.g. `aastex631`) | Journal-specific | Download from journal website |

---

## 6. Common Errors & Fixes

### "Citation `key` undefined"

**Cause**: Bibliography not processed, or key doesn't exist in `.bib` file.
**Fix**:
```bash
# Check key exists
grep "vaswani2017" references.bib
# Rebuild
latexmk -pdf main.tex
```

### "Missing package" / `File 'xxx.sty' not found`

**Fix**:
```bash
# macOS (MacTeX)
sudo tlmgr install <package-name>

# Or search for the package:
tlmgr search --global <keyword>
```

### Overfull hbox warnings

**Cause**: Text overflows margins.
**Fix**: Use `\sloppy`, reduce figure width, or rephrase text.

### "PDF file did not include all fonts" / font embedding issues

**Fix**: Ensure all fonts are embedded:
```bash
pdffonts main.pdf  # check which fonts are embedded (should say "yes")
```
If fonts are missing, use `pdflatex` (not `xelatex`) and avoid system fonts, or embed with `gs`:
```bash
gs -dNOPAUSE -dBATCH -sDEVICE=pdfwrite -dEmbedAllFonts=true -sOutputFile=main-embedded.pdf main.pdf
```

### CJK / Chinese text fails with pdflatex

**Fix**: Switch to `xelatex` with `ctex` or `xeCJK`:
```latex
% Use xelatex, NOT pdflatex
\documentclass{article}
\usepackage{ctex}  % or \usepackage{xeCJK}
```
```bash
latexmk -xelatex main.tex
```

### "Label(s) may have changed. Rerun"

**Fix**: Run `latexmk -pdf main.tex` again — it handles this automatically.

### biber vs bibtex confusion

| Symptom | Cause | Fix |
|---------|-------|-----|
| `biblatex` + `bibtex` command | Wrong backend | Use `biber main` or set `backend=biber` |
| `\bibliographystyle{plain}` with biblatex | Mixing old/new systems | Remove `\bibliographystyle`, use `\printbibliography` |

---

## 7. Package Management

### Finding and Installing Packages

```bash
# Search CTAN for a package
tlmgr search --global <keyword>

# Install a package
sudo tlmgr install <package-name>

# Update all packages
sudo tlmgr update --all

# List installed packages
tlmgr list --only-installed
```

### macOS TeX Installation

```bash
# Full installation (~5GB)
brew install --cask mactex

# Minimal installation (~100MB) + manual installs
brew install --cask basictex
sudo tlmgr install biblatex biber amsmath graphicx hyperref subcaption

# Verify
pdflatex --version && latexmk --version && biber --version
```

---

## 8. Verification Before Submission

```bash
# Page count (must match journal/conference limit)
pdfinfo main.pdf | grep Pages

# Check all fonts embedded
pdffonts main.pdf | grep "no"   # should return nothing

# Check figure resolutions
file figures/*.pdf figures/*.png

# Look for overfull warnings
grep "Overfull" main.log

# Spell check (macOS)
aspell -t -c main.tex
```

### Quick Reference

| Command | Action |
|---------|--------|
| `latexmk -pdf main.tex` | Build PDF |
| `latexmk -gg -pdf main.tex` | Force full rebuild |
| `latexmk -C` | Clean artifacts |
| `latexmk -pdf -pvc main.tex` | Build + watch |
| `latexmk -xelatex main.tex` | Build with XeLaTeX |
| `biber main` | Process bibliography |
| `pdffonts main.pdf` | Check font embedding |
| `pdfinfo main.pdf` | Page count + metadata |

---

## See Also

- [pipeline-citation-tracking.md](pipeline-citation-tracking.md) — BibTeX generation from academic APIs
- [api-crossref.md](api-crossref.md) — BibTeX export via DOI
- [api-nasa-ads.md](api-nasa-ads.md) — BibTeX export for astrophysics
- [error-handling.md](error-handling.md) — general API error patterns
