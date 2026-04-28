# 综合决策树：从输入到推荐 API

## 目标

根据你的输入类型（DOI、标题、作者、关键词、URL 等），快速路由到最优 API 或抓取方案。

## 总览决策树

```
你的输入是什么？
│
├── DOI（10.xxxx/...）
│   ├── 要元数据？
│   │   ├── CrossRef API  →  GET /works/{DOI}（最快，引用数+期刊信息）
│   │   └── OpenAlex API  →  GET /works/https://doi.org/{DOI}（含主题分类+引用网络）
│   ├── 要免费 PDF？
│   │   └── Unpaywall API →  GET /v2/{DOI}?email=xxx（查开放获取状态）
│   └── 要引用网络？
│       └── OpenAlex API  →  referenced_works + cited_by
│
├── arXiv ID（2301.xxxxx / 2301.xxxxxv1）
│   ├── 要元数据+摘要？
│   │   └── arXiv API  →  GET /api/query?id_list={ID}
│   └── 要 PDF？
│       └── 直接下载  →  https://arxiv.org/pdf/{ID}.pdf
│
├── PMID（纯数字，如 12345678）
│   └── Europe PMC  →  GET /search?query=EXT_ID:{PMID}
│
├── 关键词 / 主题短语
│   ├── 要结构化数据（引用数、年份、DOI）？
│   │   └── OpenAlex API  →  filter=title_and_abstract.search:{q}
│   ├── 要物理/CS/数学预印本？
│   │   └── arXiv API  →  search_query=all:{q}
│   ├── 要生物医学？
│   │   └── Europe PMC  →  GET /search?query={q}
│   └── 要 Google Scholar 排名？
│       └── curl+BS 抓 Scholar（每会话 ≤1 次，429 则降级 OpenAlex）
│
├── 作者名
│   ├── 要 h-index / 论文数 / 影响力？
│   │   └── OpenAlex Authors  →  filter=display_name.search:{name}
│   ├── 要该作者的所有论文？
│   │   └── OpenAlex Works  →  filter=author.id:{openalex_id}
│   └── 要 Scholar 个人页？
│       └── curl+BS 抓 /citations?user={ID}
│
├── 论文标题
│   ├── 精确匹配？
│   │   └── OpenAlex  →  filter=title.search:{title}
│   ├── 模糊搜索？
│   │   └── OpenAlex  →  filter=title_and_abstract.search:{title}
│   └── 要找 DOI？
│       └── CrossRef  →  query={title}
│
├── URL
│   ├── .pdf 结尾？
│   │   └── curl -L 下载 → PyMuPDF 提取文本
│   ├── arxiv.org/abs/...
│   │   └── 提取 ID → 下载 PDF
│   ├── scholar.google.com/...
│   │   └── curl+BS（Tier 2）→ 失败则 camoufox（Tier 3）
│   ├── nature.com / springer.com
│   │   ├── 提取 DOI（meta[name="citation_doi"]）→ 走 DOI 流程
│   │   └── camoufox 渲染（domcontentloaded，不用 networkidle）
│   ├── 主流付费出版商（Wiley/Elsevier/Science）
│   │   └── API 是唯一路径，放弃直接抓取
│   └── 其他 URL
│       └── web_read → curl+BS → camoufox（按 tier 递进）
│
└── 已有论文列表
    ├── 要格式化引用？
    │   └── citation-tracking pipeline → APA / BibTeX / IEEE
    ├── 要分析趋势？
    │   └── scholar-analysis pipeline → 趋势图 + 空白识别
    └── 要生成综述？
        └── citation-tracking pipeline → compile_literature_review()
```

## API 速查表

| API | 免费 | 需 Key | 最佳场景 | 速率限制 |
|-----|------|--------|---------|---------|
| OpenAlex | ✅ | 否 | 全能：搜索、元数据、引用网络、作者分析 | ~10 req/s |
| CrossRef | ✅ | 否 | DOI 元数据解析、引用数 | ~1 req/s |
| arXiv | ✅ | 否 | 物理/CS/数学预印本 | 较宽松 |
| Unpaywall | ✅ | email | 查开放获取状态和免费 PDF | ~10 req/s |
| Europe PMC | ✅ | 否 | 生物医学文献、PubMed | ~5 req/s |
| Semantic Scholar | ✅ | 建议申请 | 引用网络（向前+向后） | 无 Key 限速严 |
| Google Scholar | — | — | 学术搜索排名（需抓取） | IP 级限速，易 429 |

## 抓取方案速查表

| 方案 | 速度 | 稳定性 | 适用场景 |
|------|------|--------|---------|
| web_read 工具 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | 快速浏览，英文页元数据可能缺失 |
| curl + BeautifulSoup | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | Scholar 列表、Nature og meta、静态页面 |
| camoufox | ⭐⭐ | ⭐⭐⭐⭐⭐ | JS 渲染页面、反检测需求（推荐） |
| playwright-stealth v2 | ⭐⭐ | ⭐⭐⭐⭐ | JS 渲染页面（基于 Chromium） |

## 常见场景快速路由

| "我想…" | 推荐 API/方案 | 参考 Pipeline |
|---------|--------------|--------------|
| 搜索某主题的高引论文 | OpenAlex `sort=cited_by_count:desc` | discovery |
| 查某 DOI 的详细信息 | CrossRef → OpenAlex | obtain-pdf |
| 找某论文的免费 PDF | Unpaywall | obtain-pdf |
| 下载 arXiv 论文 | 直链 `/pdf/{ID}.pdf` | obtain-pdf |
| 看某领域的年度趋势 | OpenAlex 逐年查询 | scholar-analysis |
| 找某作者的所有论文 | OpenAlex `filter=author.id:{id}` | scholar-analysis |
| 查某论文被谁引用了 | OpenAlex `filter=cites:{doi}` | scholar-analysis |
| 生成 APA 参考文献 | citation-tracking pipeline | citation-tracking |
| 导出 BibTeX | citation-tracking pipeline | citation-tracking |
| 抓 Scholar 搜索结果 | curl+BS（≤1次/会话） | discovery |
| 抓 Nature 全文 | camoufox + domcontentloaded | obtain-pdf |

## 关键注意事项

1. **Google Scholar 每会话最多 1 次** — 429 风险极高，429 后降级 OpenAlex
2. **Nature/Springer 永远用 `domcontentloaded`** — `networkidle` 会无限加载超时
3. **主流付费出版商 403** — Wiley/Elsevier/Science/PNAS 几乎全部 403，API 是唯一路径
4. **arXiv PDF 无直链** — 页面内无直接 PDF 链接，从 ID 推导 `/pdf/{ID}.pdf`
5. **Playwright stealth 已过时** — 使用 camoufox 或 playwright-stealth v2 替代旧版 API
6. **扫描版 PDF** — PyMuPDF 无法提取，需 OCR（tesseract / ocrmypdf）

## Pipeline 间关系

```
discovery（发现论文）
    ↓ 论文列表 + DOI
obtain-pdf（获取全文）
    ↓ PDF 文件 + 元数据
scholar-analysis（分析趋势）  →  citation-tracking（格式化引用 + 综述生成）
```
