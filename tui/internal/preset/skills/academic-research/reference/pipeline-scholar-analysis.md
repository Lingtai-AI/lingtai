# Pipeline: 学术分析与趋势追踪（Scholar Analysis）

## 目标

从论文列表出发，构建引用网络、追踪研究趋势、发现研究空白、评估学者影响力，生成结构化分析报告。

## 工作流步骤

1. **构建引用网络**：从单篇 DOI 出发，获取向前引用（该论文引用了谁）和向后引用（谁引用了该论文）
2. **趋势分析**：按年份统计某主题的论文发表量、平均引用数，生成时间线
3. **研究空白识别**：分析概念标签频率，高频 = 充分研究，低频 = 潜在空白
4. **学者影响力评估**：综合论文数、总引用数、h-index 等多维度指标
5. **综述自动生成**：将分析结果整合为结构化文献综述文档

## 决策树

```
需要什么分析？
├── 引用网络
│   ├── 向前引用（该论文引用了谁）
│   │   └── OpenAlex: referenced_works 字段
│   └── 向后引用（谁引用了该论文）
│       └── OpenAlex: cited_by API
│
├── 主题趋势
│   └── 逐年查询 OpenAlex → 统计发表量 + 引用数
│       └── ASCII 趋势图可视化
│
├── 研究空白
│   └── OpenAlex concepts 字段 → 概念频率分析
│       ├── 高频概念 → 已充分研究
│       └── 低频概念 → 潜在研究空白
│
├── 学者影响力
│   └── OpenAlex authors API → h-index / 论文数 / 引用数
│
└── 综合综述
    └── 整合以上所有分析 → 生成 Markdown 文档
```

## 代码示例

### 引用网络构建

```python
import requests

def build_citation_network(doi, max_refs=10):
    """用 OpenAlex 构建引用网络"""
    clean_doi = doi.replace("https://doi.org/", "")
    url = f"https://api.openalex.org/works/https://doi.org/{clean_doi}"
    r = requests.get(url, timeout=10).json()

    paper = {
        "title": r.get("display_name"),
        "year": r.get("publication_year"),
        "citations": r.get("cited_by_count", 0),
        "references": [w for w in r.get("referenced_works", [])[:20]],
    }

    # 获取参考文献详情（向前引用）
    refs = []
    for ref_url in paper["references"][:max_refs]:
        try:
            ref_r = requests.get(ref_url, timeout=5).json()
            refs.append({
                "title": ref_r.get("display_name"),
                "year": ref_r.get("publication_year"),
                "doi": ref_r.get("doi"),
            })
        except Exception:
            pass

    return {"target": paper, "references": refs}


def get_citing_papers(doi, limit=20):
    """获取引用某 DOI 的论文（向后引用）"""
    clean_doi = doi.replace("https://doi.org/", "")
    r = requests.get(
        "https://api.openalex.org/works",
        params={"filter": f"cites:https://doi.org/{clean_doi}", "per_page": limit},
        timeout=10
    ).json()
    return [
        {"title": w.get("display_name"), "year": w.get("publication_year"),
         "citations": w.get("cited_by_count", 0)}
        for w in r.get("results", [])
    ]
```

### 研究趋势分析

```python
import requests
import time

def analyze_topic_trends(topic, year_range=(2015, 2024)):
    """分析某主题的时间趋势"""
    yearly_stats = {}
    for year in range(year_range[0], year_range[1] + 1):
        r = requests.get(
            "https://api.openalex.org/works",
            params={
                "filter": f"title_and_abstract.search:{topic},publication_year:{year}",
                "per_page": 100,
                "select": "id,title,publication_year,cited_by_count"
            },
            timeout=10
        ).json()

        papers = r.get("results", [])
        yearly_stats[year] = {
            "count": len(papers),
            "total_citations": sum(p.get("cited_by_count", 0) for p in papers),
            "avg_citations": sum(p.get("cited_by_count", 0) for p in papers) / max(len(papers), 1),
        }
        time.sleep(0.3)  # 避免限流

    return yearly_stats


def print_trend_chart(stats):
    """ASCII 趋势图"""
    max_count = max(v["count"] for v in stats.values()) if stats else 1
    for year, v in sorted(stats.items()):
        bar_len = int(v["count"] / max_count * 40)
        print(f"{year} | {'█' * bar_len} {v['count']:3d}篇  均引 {v['avg_citations']:.1f}")
```

### 研究空白发现

```python
from collections import Counter

def find_research_gaps(topic, years=(2018, 2024)):
    """识别某主题的研究空白"""
    import requests

    r = requests.get(
        "https://api.openalex.org/works",
        params={
            "filter": f"title_and_abstract.search:{topic},publication_year:{years[0]}:{years[1]}",
            "per_page": 200,
            "select": "concepts"
        },
        timeout=15
    ).json()

    concept_counts = Counter()
    for w in r.get("results", []):
        for c in w.get("concepts", []):
            if c.get("level", 0) >= 1:
                concept_counts[c["display_name"]] += 1

    total = sum(concept_counts.values())
    print("=== 高频概念（充分研究）===")
    for concept, count in concept_counts.most_common(10):
        print(f"  {concept}: {count} 篇 ({count/total*100:.1f}%)")

    print("\n=== 低频概念（潜在空白）===")
    for concept, count in concept_counts.most_common()[-10:]:
        if count < 5:
            print(f"  {concept}: {count} 篇")
```

### 学者影响力分析

```python
def analyze_author_impact(author_name):
    """综合分析学者影响力"""
    import requests

    r = requests.get(
        "https://api.openalex.org/authors",
        params={"filter": f"display_name.search:{author_name}", "per_page": 3},
        timeout=10
    ).json()

    profiles = []
    for author in r.get("results", []):
        stats = author.get("summary_stats", {})
        profiles.append({
            "name": author.get("display_name"),
            "institution": author.get("last_known_institution", {}).get("display_name"),
            "works_count": stats.get("works_count", 0),
            "cited_by_count": stats.get("cited_by_count", 0),
            "h_index": stats.get("h_index"),
        })
    return profiles
```

## 失败回退

| 失败场景 | 回退策略 |
|---------|---------|
| OpenAlex 引用数据为空 | 该论文可能过新未被索引，改用 Semantic Scholar |
| 逐年查询超时 | 减小年份范围，增加 sleep 间隔 |
| 概念标签为空 | 改用标题关键词提取替代 |
| 学者重名过多 | 加上 institution 过滤条件 |
| 引用数偏低（近年论文） | OpenAlex 有时间滞后，近年引用数偏低属正常 |

## 注意事项

- OpenAlex 引用数据有时间滞后，近年论文引用数偏低
- 不同学科的平均引用差异巨大，跨学科比较需谨慎
- 递归引用网络获取需加延迟（`time.sleep(0.3)`）避免限流
- 概念 level ≥ 1 过滤掉过于宽泛的顶层概念

## 相关 Pipeline

- [pipeline-discovery.md](pipeline-discovery.md) — 发现论文（上游）
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — 获取全文（上游）
- [pipeline-citation-tracking.md](pipeline-citation-tracking.md) — 参考文献格式化（下游）
- [decision-tree.md](decision-tree.md) — 综合决策路由
