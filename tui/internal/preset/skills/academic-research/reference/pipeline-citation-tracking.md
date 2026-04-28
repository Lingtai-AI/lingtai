# Pipeline: 参考文献管理与格式化（Citation Tracking）

## 目标

将发现的论文元数据格式化为标准引用格式（APA、BibTeX 等），批量构建参考文献库，生成结构化文献综述文档。

## 工作流步骤

1. **收集元数据**：从 discovery/obtain pipeline 获取论文列表，或通过 OpenAlex 批量查询
2. **字段标准化**：将 CrossRef / OpenAlex / 手动输入的字段统一为内部格式 `{title, authors, year, journal, volume, issue, pages, doi}`
3. **格式化引用**：按目标格式（APA / BibTeX / IEEE）生成引用字符串
4. **批量处理**：一次性处理数十篇论文，输出为 Markdown 或 .bib 文件
5. **生成综述文档**：自动生成含高影响力论文排行、时间趋势、完整参考文献的综述

## 决策树

```
需要什么？
├── 单篇引用格式化
│   ├── APA 7 → format_apa(paper)
│   ├── BibTeX → to_bibtex(paper)
│   └── 其他格式 → 基于 APA 模板微调
│
├── 批量构建参考文献库
│   ├── 已有论文列表 → 批量格式化 → 输出文件
│   └── 只有查询词 → OpenAlex 搜索 → 格式化 → 输出文件
│
└── 生成文献综述文档
    ├── 已有论文列表 → compile_literature_review(papers, topic)
    └── 需先搜索 → discovery pipeline → 格式化 → 综述
```

## 代码示例

### APA 7 格式化

```python
def format_apa(paper):
    """APA 7 格式"""
    authors = paper.get("authors", [])
    year = paper.get("year", "n.d.")
    title = paper.get("title", "")
    journal = paper.get("journal", paper.get("venue", ""))
    volume = paper.get("volume", "")
    issue = paper.get("issue", "")
    pages = paper.get("pages", "")
    doi = paper.get("doi", "")

    if len(authors) == 0:
        author_str = "Unknown"
    elif len(authors) == 1:
        author_str = f"{authors[0].get('family','')}, {authors[0].get('given','')[0]}."
    elif len(authors) == 2:
        a1, a2 = authors[0], authors[1]
        author_str = (
            f"{a1.get('family','')}, {a1.get('given','')[0]}. & "
            f"{a2.get('family','')}, {a2.get('given','')[0]}."
        )
    else:
        author_str = f"{authors[0].get('family','')}, {authors[0].get('given','')[0]}., et al."

    parts = [f"{author_str} ({year}). {title}."]
    if journal:
        parts.append(f"*{journal}*")
    if volume:
        parts[-1] += f", {volume}"
    if issue:
        parts[-1] += f"({issue})"
    if pages:
        parts[-1] += f", {pages}"
    if doi:
        parts.append(f"https://doi.org/{doi}")

    return " ".join(parts)
```

### BibTeX 导出

```python
def to_bibtex(paper):
    """导出为 BibTeX"""
    key_parts = []
    if paper.get("authors"):
        key_parts.append(paper["authors"][0].get("family", "unknown"))
    key_parts.append(str(paper.get("year", "nd")))
    key = "".join(key_parts).lower().replace(" ", "")

    fields = {
        "title": paper.get("title", ""),
        "author": " and ".join(
            f"{a.get('family','?')}, {a.get('given','')}"
            for a in paper.get("authors", [])
        ),
        "year": str(paper.get("year", "")),
        "journal": paper.get("journal", paper.get("venue", "")),
        "volume": paper.get("volume", ""),
        "number": paper.get("issue", ""),
        "pages": paper.get("pages", ""),
        "doi": paper.get("doi", ""),
    }

    entries = [f"  {k} = {{{v}}}" for k, v in fields.items() if v]
    return f"@article{{{key},\n" + ",\n".join(entries) + "\n}"
```

### 批量构建参考文献库

```python
import requests

def build_reference_library(query, limit=50, style="apa"):
    """
    从搜索构建参考文库：
    1. OpenAlex 搜索论文
    2. 标准化字段
    3. 格式化引用
    4. 输出为文件
    """
    r = requests.get(
        "https://api.openalex.org/works",
        params={
            "filter": f"title_and_abstract.search:{query}",
            "sort": "cited_by_count:desc",
            "per_page": limit
        },
        timeout=10
    ).json()

    papers = []
    for w in r.get("results", []):
        papers.append({
            "title": w.get("display_name"),
            "year": w.get("publication_year"),
            "authors": [
                {"family": a.get("author", {}).get("display_name", "").split()[-1],
                 "given": " ".join(a.get("author", {}).get("display_name", "").split()[:-1])}
                for a in w.get("authorships", [])
            ],
            "journal": w.get("host_venue", {}).get("display_name", ""),
            "citations": w.get("cited_by_count", 0),
            "doi": w.get("doi", "").replace("https://doi.org/", ""),
        })

    if style == "apa":
        formatted = [format_apa(p) for p in papers]
    elif style == "bibtex":
        formatted = [to_bibtex(p) for p in papers]
    else:
        formatted = [format_apa(p) for p in papers]

    output = f"# References — {query}\n\n" + "\n\n".join(formatted)
    with open("/tmp/references.md", "w") as f:
        f.write(output)
    return "/tmp/references.md", len(papers)
```

### 生成文献综述文档

```python
def compile_literature_review(papers, topic):
    """从论文列表生成结构化综述"""
    from collections import Counter

    by_year = Counter(p.get("year") for p in papers if p.get("year"))
    by_citations = sorted(papers, key=lambda x: x.get("citations", 0), reverse=True)
    top_papers = by_citations[:10]

    doc = f"""# 文献综述：{topic}

## 概述
- **论文总数**：{len(papers)} 篇
- **发表年份**：{min(by_year)} – {max(by_year)}
- **年均发表**：{len(papers) / max(len(by_year), 1):.1f} 篇

## 高影响力论文（Top 10）

| # | 标题 | 年份 | 引用数 |
|---|------|------|--------|
"""
    for i, p in enumerate(top_papers, 1):
        title = p.get("title", "Unknown")[:60]
        year = p.get("year", "?")
        cites = p.get("citations", 0)
        doc += f"| {i} | {title} | {year} | {cites} |\n"

    doc += "\n## 时间趋势\n\n"
    for year in sorted(by_year):
        bar = "▓" * by_year[year]
        doc += f"- **{year}**: {bar} {by_year[year]} 篇\n"

    doc += "\n## 参考文献\n\n"
    doc += "\n\n".join(format_apa(p) for p in by_citations)

    with open("/tmp/literature_review.md", "w") as f:
        f.write(doc)
    return "/tmp/literature_review.md"
```

## 失败回退

| 失败场景 | 回退策略 |
|---------|---------|
| OpenAlex 无结果 | 换用更宽泛关键词，或从 discovery pipeline 获取 |
| 作者名解析异常 | 用全名作为 family name，given 留空 |
| BibTeX key 冲突 | 追加 `_2`, `_3` 后缀 |
| 字段不完整（缺卷/期/页） | 跳过缺失字段，生成不完整但有效的引用 |
| 跨学科引用格式差异 | 默认 APA，如需特定期刊格式需人工调整 |

## 注意事项

- 不同期刊的引用格式有细微差异，务必确认目标期刊的格式要求
- BibTeX key 需唯一，自动生成时可能与已有文献冲突
- CrossRef 和 OpenAlex 的字段名不同，统一化处理后再格式化
- OpenAlex 的 `host_venue` 字段名可能更新为 `primary_location`

## 相关 Pipeline

- [pipeline-discovery.md](pipeline-discovery.md) — 发现论文（上游）
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — 获取全文（上游）
- [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md) — 分析引用网络与趋势
- [decision-tree.md](decision-tree.md) — 综合决策路由
