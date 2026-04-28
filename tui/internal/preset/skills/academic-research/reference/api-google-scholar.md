# Google Scholar Reference

## API 概述

Google Scholar 没有官方公开 API。本参考文档提供两种互补方式获取 Scholar 数据：

1. **页面抓取（Scraping）**——通过 `web_read` 工具或 curl 获取 Scholar 页面原始内容
2. **HTML 解析（Parsing）**——用 BeautifulSoup 从原始 HTML 中提取结构化文献元数据

两种方式各有适用场景：抓取解决"获取数据"，解析解决"理解数据"。

| 属性 | 说明 |
|------|------|
| 基础 URL | `https://scholar.google.com/` |
| 认证 | 无需 API key |
| 反爬机制 | Google 有较强的反爬策略，高频请求会触发 CAPTCHA |
| 适用场景 | 引用数据、作者 profile、跨学科搜索 |
| 替代方案 | 大规模需求建议用 Semantic Scholar 或 OpenAlex → 参见 [api-pubmed.md](api-pubmed.md) |

---

## 第一部分：页面抓取（Scraping）

### Profile 页面

获取指定学者的论文列表、引用数、发表年份。

**URL 格式**：

| 用途 | URL |
|------|-----|
| 学者主页 | `https://scholar.google.com/citations?user={USER_ID}&hl=en` |
| 按发表日期排序 | `https://scholar.google.com/citations?hl=en&user={USER_ID}&view_op=list_works&sortby=pubdate` |
| 按引用数排序（默认） | `https://scholar.google.com/citations?hl=en&user={USER_ID}&view_op=list_works&sortby=citationcount` |

`{USER_ID}` 是 Google Scholar 用户 ID（如 `rcQwoOoAAAAJ`）。

### 搜索结果页面

通过关键词搜索学术论文。

```
https://scholar.google.com/scholar?q={关键词}&hl=en
```

### 抓取方法一：web_read 工具（推荐）

```python
# 在 lingtai 环境中使用 web_read 工具
from lingtai import web_read

# 获取学者论文列表（按日期排序）
url = "https://scholar.google.com/citations?hl=en&user=rcQwoOoAAAAJ&view_op=list_works&sortby=pubdate"
content = web_read(url=url)
print(content[:2000])
```

**优点**：`web_read` 内置反爬处理，自动处理 User-Agent 和页面渲染。

### 抓取方法二：curl

```bash
curl -s "https://scholar.google.com/citations?user=rcQwoOoAAAAJ&hl=en" \
  -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36" \
  -H "Accept: text/html,application/xhtml+xml" \
  -H "Accept-Language: en-US,en;q=0.5" \
  --max-time 15 \
  -o /tmp/scholar.html
```

**用于后续 BeautifulSoup 解析的场景**——先 curl 保存 HTML，再 Python 解析。

### Profile 页面数据格式

`web_read` 返回的文本是 tab 分隔格式：

```
The Evolution of the 1/f Range within a Single Fast-solar-wind Stream...
N Davis, BDG Chandran, TA Bowen...
The Astrophysical Journal 950 (2), 154, 2023 | 38 | 2023
```

每条记录通常跨 2-3 行：标题行、作者/期刊行、引用/年份行（含 `|` 分隔符）。

### 简单解析（web_read 场景）

```python
import lingtai

USER_ID = "rcQwoOoAAAAJ"

url = f"https://scholar.google.com/citations?hl=en&user={USER_ID}&view_op=list_works&sortby=pubdate"
content = lingtai.web_read(url=url)

lines = content.strip().split('\n')
papers = []
i = 0
while i < len(lines):
    line = lines[i].strip()
    if line and '|' in line:
        parts = [p.strip() for p in line.split('|')]
        if len(parts) >= 3:
            paper = {
                'title': parts[0],
                'citations': parts[1] if parts[1] else '0',
                'year': parts[2],
                'authors_venue': lines[i-1].strip() if i > 0 else '',
                'raw': lines[i-2].strip() if i > 1 else ''
            }
            papers.append(paper)
    i += 1

print(f"找到 {len(papers)} 篇论文")
for p in papers[:3]:
    print(f"标题: {p['title']}")
    print(f"年份: {p['year']}, 引用: {p['citations']}")
    print("---")
```

---

## 第二部分：HTML 解析（Parsing）

当 curl 获取了 Google Scholar 搜索结果的原始 HTML 后，使用 BeautifulSoup 提取结构化元数据。

### CSS 选择器参考

| 元素 | 选择器 | 提取内容 |
|------|--------|----------|
| 文献条目 | `.gs_ri` | 整条文献的容器 |
| 标题+链接 | `.gs_rt a` | 论文标题和跳转链接 |
| 作者/期刊/年份 | `.gs_a` | 作者、发表期刊、年份（绿色行） |
| 摘要 | `.gs_rs` | 搜索结果中显示的摘要片段 |
| 引用/相关链接 | `.gs_fl` | 底部链接区（Cited by N、Related articles） |
| 相关文献链接 | `.gs_fl a[href*="related:"]` | "Related articles" 链接 |
| 所有版本链接 | `.gs_fl a[href*="cluster="]` | "All N versions" 链接 |

### 完整解析脚本

```python
from bs4 import BeautifulSoup
import re
import json

def parse_scholar_html(html_path):
    """解析 Google Scholar 搜索结果 HTML，提取文献元数据。

    Args:
        html_path: 保存的 HTML 文件路径

    Returns:
        list[dict]: 解析后的文献列表，每条包含：
            - title (str): 标题
            - link (str): 论文链接
            - authors (list[str]): 作者列表
            - meta (str): 作者+期刊+年份原始文本
            - year (str|None): 发表年份
            - abstract (str): 摘要片段
            - citations (int): 引用数
            - related_link (str): 相关文献链接
            - versions_link (str): 所有版本链接
    """
    with open(html_path, 'r', encoding='utf-8') as f:
        html = f.read()

    soup = BeautifulSoup(html, 'html.parser')
    articles = soup.select('.gs_ri')

    results = []
    for art in articles:
        result = {}

        # 标题与链接
        title_tag = art.select_one('.gs_rt a')
        raw_title = title_tag.get_text() if title_tag else ''
        # 修复被 <b> 标签分割的词：如 "Trans former" → "Transformer"
        result['title'] = re.sub(r'([a-z])([A-Z])', r'\1 \2', raw_title)
        result['link'] = title_tag.get('href', '') if title_tag else ''

        # 作者、期刊、年份
        gs_a = art.select_one('.gs_a')
        if gs_a:
            authors = [a.get_text(strip=True) for a in gs_a.select('a')]
            result['authors'] = authors
            meta_text = gs_a.get_text(strip=True)
            result['meta'] = re.sub(r'([a-z])([A-Z])', r'\1 \2', meta_text)
            year_match = re.search(r'\b(19|20)\d{2}\b', meta_text)
            result['year'] = year_match.group() if year_match else None
        else:
            result['authors'] = []
            result['meta'] = ''
            result['year'] = None

        # 摘要
        snippet_tag = art.select_one('.gs_rs')
        raw_snippet = snippet_tag.get_text() if snippet_tag else ''
        result['abstract'] = re.sub(r'([a-z])([A-Z])', r'\1 \2', raw_snippet)

        # 引用数
        cit_match = re.search(r'Cited by (\d+)', art.get_text())
        result['citations'] = int(cit_match.group(1)) if cit_match else 0

        # 关联链接
        result['related_link'] = ''
        result['versions_link'] = ''
        for link in art.select('.gs_fl a'):
            href = link.get('href', '')
            if 'related:' in href:
                result['related_link'] = 'https://scholar.google.com' + href
            if 'cluster=' in href and 'cites=' not in href:
                result['versions_link'] = 'https://scholar.google.com' + href

        results.append(result)

    return results

# 使用示例
papers = parse_scholar_html('/tmp/scholar.html')
print(json.dumps(papers, ensure_ascii=False, indent=2))

# 提取特定字段
for p in papers[:5]:
    print(f"标题: {p['title']}")
    print(f"作者: {', '.join(p['authors'])}")
    print(f"年份: {p['year']}")
    print(f"引用: {p['citations']}")
    print(f"摘要: {p['abstract'][:100]}...")
    print("---")
```

### 依赖安装

```bash
pip install beautifulsoup4 requests
```

---

## arXiv 论文的 PDF 下载

Google Scholar 搜索结果中的 arXiv 论文有直接 PDF 链接。

### URL 格式

```
https://arxiv.org/pdf/{arXiv_ID}.pdf
```

### 下载示例

```bash
# 直接下载
curl -s "https://arxiv.org/pdf/2512.12585" -o paper.pdf
```

### 通过标题查找 arXiv ID

```python
import requests
import xml.etree.ElementTree as ET

def find_arxiv_pdf_url(title_keywords):
    """通过 arXiv API 搜索论文标题，返回 PDF URL。"""
    url = "https://export.arxiv.org/api/query"
    params = {
        "search_query": f"ti:{title_keywords}",
        "max_results": 1,
    }
    r = requests.get(url, params=params, timeout=10)
    root = ET.fromstring(r.text)
    ns = {"atom": "http://www.w3.org/2005/Atom"}
    entry = root.find("atom:entry", ns)
    if entry is not None:
        arxiv_id = entry.find("atom:id", ns).text.split("/")[-1]
        return f"https://arxiv.org/pdf/{arxiv_id}.pdf"
    return None
```

---

## 速率限制与反爬策略

| 策略 | 说明 |
|------|------|
| 请求间隔 | 每次请求间隔至少 2-3 秒 |
| User-Agent | 使用真实浏览器 User-Agent（curl 场景） |
| CAPTCHA | 高频请求会触发验证码，需要降低频率或更换 IP |
| `web_read` 优先 | lingtai 的 `web_read` 内置反爬处理，优先使用 |
| 大规模需求 | 使用 Semantic Scholar API 或 OpenAlex API 替代 |

### 避免被封的最佳实践

```python
import time
import random

def polite_fetch(url, method="web_read"):
    """礼貌抓取：随机延迟 + User-Agent。"""
    delay = random.uniform(2, 5)
    time.sleep(delay)

    if method == "web_read":
        from lingtai import web_read
        return web_read(url=url)
    else:
        import requests
        headers = {
            "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
            "Accept": "text/html,application/xhtml+xml",
            "Accept-Language": "en-US,en;q=0.5",
        }
        r = requests.get(url, headers=headers, timeout=15)
        r.raise_for_status()
        return r.text
```

## 失败处理

| 场景 | 处理方式 |
|------|----------|
| CAPTCHA 页面 | 停止抓取，等待 10-30 分钟后重试，或使用 `web_read` |
| 空结果 | 检查 USER_ID 是否正确，或尝试不同的搜索关键词 |
| 解析结果为空 | Google 可能更新了 HTML 结构，检查 CSS 选择器是否仍然有效 |
| 标题词间有空格异常 | `<b>` 标签分割导致，使用 `re.sub(r'([a-z])([A-Z])', r'\1 \2', title)` 修复 |
| PDF 链接 403 | 某些 PDF 需要机构访问权限，尝试通过 Unpaywall 查找免费版本 |
| 超时 | 设置 `timeout=15`，重试最多 3 次 |

## 已知限制

1. **标题空格问题**：HTML 中文本被 `<b>` 标签分割，需用正则后处理
2. **反爬限制**：大量请求可能触发验证码，建议加延时
3. **登录用户**：登录后的 HTML 结构可能不同
4. **无官方 API**：结构可能随时变化，解析脚本需维护

## 相关 API

- **Unpaywall**：Scholar 找到论文后，用 DOI 查找免费 PDF → 参见 [api-unpaywall.md](api-unpaywall.md)
- **PubMed**：生物医学领域的官方文献 API，更稳定 → 参见 [api-pubmed.md](api-pubmed.md)
- **arXiv API**：`https://export.arxiv.org/api/query` — arXiv 论文的官方搜索接口
- **替代方案**：大规模需求推荐 Semantic Scholar (`https://api.semanticscholar.org/graph/v1/paper/search`) 或 OpenAlex (`https://api.openalex.org/works`)
