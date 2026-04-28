---
name: academic-research
description: "Deep-dive academic research skill — 9 API references (arXiv, CrossRef, OpenAlex, Semantic Scholar, CORE, PubMed, Unpaywall, Google Scholar, DOI Resolver) + 5 pipeline workflows (discovery, PDF acquisition, citation tracking, scholar analysis, decision tree). Use this when you need detailed API parameters, code examples, and fallback chains for scholarly search. If you arrived from web-browsing, this is the 'how to use each API' layer below the routing tier."
version: 1.0.0
tags: [academic, research, arxiv, crossref, openalex, semantic-scholar, core, pubmed, unpaywall, google-scholar, doi, pdf, citation, pipeline]
parent: web-browsing
---

# Academic Research

> 如果你从 web-browsing skill 跳转过来——web-browsing 回答"用哪个 tier"，本 skill 回答"用具体 API 时怎么用"。

## 何时使用

- 已有 DOI/标题/作者，需要获取论文元数据或全文
- 需要系统性地检索学术文献
- 需要追踪引用网络、分析研究趋势
- 需要 PDF 全文但不确定从哪个来源获取

## 决策入口

**不知道从哪个 API 开始？** → 读 [reference/decision-tree.md](reference/decision-tree.md)

它根据你的输入（DOI？arXiv ID？关键词？学科领域？）路由到最合适的 API。

## API Reference（9 个）

每个 reference 包含：端点参数表、可运行代码、返回格式、速率限制、失败处理、交叉引用。

| API | Reference | 最佳场景 | 需要 Key？ |
|-----|-----------|----------|-----------|
| arXiv | [api-arxiv.md](reference/api-arxiv.md) | 预印本检索、CS/物理/数学 | 否 |
| CrossRef | [api-crossref.md](reference/api-crossref.md) | DOI 元数据、基金查询、今日新增 | 否（推荐 mailto） |
| DOI Resolver | [api-doi-resolver.md](reference/api-doi-resolver.md) | 单个/批量 DOI 解析为结构化引用 | 否 |
| OpenAlex | [api-openalex.md](reference/api-openalex.md) | 大规模论文发现、机构/概念分析 | 否 |
| Semantic Scholar | [api-semantic-scholar.md](reference/api-semantic-scholar.md) | 引用网络、TLDR 摘要、作者画像 | 否 |
| CORE | [api-core.md](reference/api-core.md) | 开放获取全文下载 | 可选 |
| PubMed | [api-pubmed.md](reference/api-pubmed.md) | 生物医学文献检索、PMC 全文 | 否 |
| Unpaywall | [api-unpaywall.md](reference/api-unpaywall.md) | 查找论文的 OA 版本/PDF | email 参数（非占位符） |
| Google Scholar | [api-google-scholar.md](reference/api-google-scholar.md) | 学科覆盖最广、引用计数（需爬取） | 否（需 stealth） |

## Pipeline Workflow（5 个）

每个 pipeline 包含：工作流步骤、决策树、代码示例、失败回退。

| Pipeline | Reference | 功能 |
|----------|-----------|------|
| 论文发现 | [pipeline-discovery.md](reference/pipeline-discovery.md) | 从关键词到候选论文集 |
| PDF 获取 | [pipeline-obtain-pdf.md](reference/pipeline-obtain-pdf.md) | 从元数据到全文 PDF（含 stealth） |
| 引用追踪 | [pipeline-citation-tracking.md](reference/pipeline-citation-tracking.md) | 前向/后向引用网络 |
| 学者分析 | [pipeline-scholar-analysis.md](reference/pipeline-scholar-analysis.md) | 影响力、趋势、h-index |
| 综合决策树 | [decision-tree.md](reference/decision-tree.md) | "我有 X，去哪个 API？" |

## 快速路径

```
我有 DOI          → api-doi-resolver.md → api-crossref.md → api-unpaywall.md 找 PDF
我有 arXiv ID     → api-arxiv.md（PDF 直链）
我有 PMID         → api-pubmed.md
我只有关键词       → decision-tree.md → 按学科选 API
我需要引用网络     → api-semantic-scholar.md 或 api-openalex.md
我需要全文 PDF     → pipeline-obtain-pdf.md（Unpaywall → CORE → Europe PMC 链）
```

## 与 web-browsing 的关系

- **web-browsing**：routing layer — "该用哪个 tier？"（PDF direct? API metadata? trafilatura? Playwright?）
- **academic-research**：deep-dive layer — "用 OpenAlex 时 filter 参数怎么写？用 Unpaywall 时 email 怎么填？"
- 两者互补，不重叠。

## 已知注意事项

- Google Scholar 需要 stealth 浏览（camoufox 或 playwright-stealth v2），不可用旧版 `playwright_stealth` API
- Unpaywall 的 email 参数是**必填**且**必须真实**——这是唯一的"认证"
- arXiv 强制 HTTPS，HTTP 自动 301 重定向
- CORE 有 key 可获更高限额，无 key 也可用
