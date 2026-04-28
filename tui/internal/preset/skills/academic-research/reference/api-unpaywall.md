# Unpaywall API Reference

## API 概述

Unpaywall 是一个开放获取（Open Access）文献查找服务。通过 DOI 查询，返回该论文的免费 PDF 链接（如果存在）。Unpaywall 聚合了来自出版社、预印本服务器、机构知识库等多个来源的 OA 信息。

| 属性 | 说明 |
|------|------|
| 基础 URL | `https://api.unpaywall.org/v2/` |
| 认证 | `email` 参数（**必需**）—— 用于标识你的应用，非占位符 |
| 速率限制 | 无官方硬限制，建议每秒不超过 10 请求 |
| 返回格式 | JSON |
| 数据源 | Crossref、DOAJ、PubMed Central、arXiv 等 |

> **关于 email 参数**：这是 Unpaywall API 唯一需要的"认证"。请使用真实邮箱或机构邮箱，Unpaywall 以此识别你的应用并在出现问题时联系你。不要使用 `test@example.com` 等虚假地址——这可能导致请求被拒绝。

## 端点与参数

### 查询单篇论文

```
GET https://api.unpaywall.org/v2/{DOI}?email={your_email}
```

| 参数 | 位置 | 说明 | 示例 |
|------|------|------|------|
| `DOI` | 路径 | 论文 DOI | `10.1038/nature12373` |
| `email` | 查询 | 你的邮箱（标识应用） | `my@university.edu` |

### 批量查询

Unpaywall 无官方批量端点。推荐循环逐个调用，控制速率：

```python
import time

for doi in doi_list:
    result = find_free_pdf(doi, email="my@university.edu")
    time.sleep(0.1)  # 每秒不超过 10 次
```

## 代码示例

### 查找开放获取 PDF

```python
import requests

def find_free_pdf(doi, email="my@university.edu"):
    """通过 Unpaywall 查找论文的免费 PDF。

    Args:
        doi: DOI 字符串，如 '10.1038/nature12373'
        email: 你的真实邮箱，用于标识应用（非占位符）

    Returns:
        dict: {
            'doi': str,
            'is_oa': bool,
            'pdf_url': str|None,
            'landing_url': str|None,
            'version': str|None,
            'license': str|None,
            'oa_locations': list
        }
    """
    url = f"https://api.unpaywall.org/v2/{doi}"
    params = {"email": email}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    data = r.json()

    result = {
        "doi": data.get("doi"),
        "is_oa": data.get("is_oa", False),
        "pdf_url": None,
        "landing_url": None,
        "version": None,
        "license": None,
        "oa_locations": data.get("oa_locations", []),
    }

    best = data.get("best_oa_location")
    if best:
        result["pdf_url"] = best.get("url_for_pdf")
        result["landing_url"] = best.get("url_for_landing_page")
        result["version"] = best.get("version")
        result["license"] = best.get("license")

    return result

# 使用示例
result = find_free_pdf("10.1038/nature12373", email="researcher@university.edu")
if result["is_oa"]:
    print(f"免费 PDF: {result['pdf_url']}")
    print(f"版本: {result['version']}")
    print(f"许可: {result['license']}")
else:
    print("无可用的免费版本")
```

### 下载 PDF

```python
def download_free_pdf(doi, output_path, email="my@university.edu"):
    """通过 Unpaywall 查找并下载免费 PDF。"""
    result = find_free_pdf(doi, email)
    if not result["pdf_url"]:
        print(f"无免费 PDF: {doi}")
        return None

    headers = {"User-Agent": f"Academic Research Tool (mailto:{email})"}
    r = requests.get(result["pdf_url"], timeout=30, headers=headers)
    if r.status_code == 200:
        with open(output_path, "wb") as f:
            f.write(r.content)
        print(f"已下载: {output_path}")
        return output_path
    else:
        print(f"下载失败 (HTTP {r.status_code}): {result['pdf_url']}")
        return None

# 使用示例
download_free_pdf("10.1038/nature12373", "/tmp/paper.pdf", "researcher@university.edu")
```

### 从多个 OA 来源中选择最佳

```python
def find_best_oa(doi, email="my@university.edu"):
    """从所有 OA 来源中选择最佳 PDF。

    优先级：publishedVersion > acceptedVersion > submittedVersion
    """
    url = f"https://api.unpaywall.org/v2/{doi}"
    r = requests.get(url, params={"email": email}, timeout=10)
    r.raise_for_status()
    data = r.json()

    if not data.get("is_oa"):
        return None

    locations = data.get("oa_locations", [])
    priority = {"publishedVersion": 3, "acceptedVersion": 2, "submittedVersion": 1}

    best = None
    best_score = 0
    for loc in locations:
        pdf = loc.get("url_for_pdf")
        if not pdf:
            continue
        score = priority.get(loc.get("version"), 0)
        if score > best_score:
            best_score = score
            best = loc

    return best

# 使用示例
best = find_best_oa("10.1038/nature12373", "researcher@university.edu")
if best:
    print(f"最佳版本: {best['version']}")
    print(f"PDF URL: {best['url_for_pdf']}")
```

### 直接 curl 示例

```bash
curl -s "https://api.unpaywall.org/v2/10.1038/nature12373?email=my@university.edu" | python3 -m json.tool
```

## 返回格式

### 完整响应结构

```json
{
  "doi": "10.1038/nature12373",
  "title": "The geodesic response of the Gulf Stream...",
  "year": 2013,
  "is_oa": true,
  "best_oa_location": {
    "url_for_pdf": "https://www.nature.com/articles/nature12373.pdf",
    "url_for_landing_page": "https://www.nature.com/articles/nature12373",
    "evidence": "oa repository (via pmcid lookup)",
    "license": null,
    "version": "publishedVersion",
    "host_type": "publisher",
    "updated": "2024-01-15T00:00:00"
  },
  "oa_locations": [
    {
      "url_for_pdf": "https://www.nature.com/articles/nature12373.pdf",
      "url_for_landing_page": "https://www.nature.com/articles/nature12373",
      "evidence": "oa repository (via pmcid lookup)",
      "license": null,
      "version": "publishedVersion",
      "host_type": "publisher"
    }
  ],
  "journal_name": "Nature",
  "publisher": "Springer Nature"
}
```

### 版本类型说明

| 版本 | 说明 | 典型来源 |
|------|------|----------|
| `publishedVersion` | 出版社正式版（最佳） | 出版社官网、PMC |
| `acceptedVersion` | 同行评审后、排版前 | 机构知识库、arXiv |
| `submittedVersion` | 投稿原稿 | 预印本服务器 |

### 关键字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `is_oa` | bool | 是否存在任何 OA 版本 |
| `best_oa_location` | object\|null | Unpaywall 判定的最佳 OA 来源 |
| `oa_locations` | array | 所有已知 OA 来源 |
| `url_for_pdf` | string\|null | 直链 PDF 地址 |
| `url_for_landing_page` | string\|null | OA 着陆页地址 |
| `host_type` | string | `publisher`（出版社）或 `repository`（仓储） |
| `evidence` | string | OA 状态的判定依据 |

## 速率限制

| 场景 | 建议 |
|------|------|
| 单次查询 | 无需延迟 |
| 批量查询 | `time.sleep(0.1)`（每秒 ~10 次） |
| 大规模（>1000 篇） | `time.sleep(0.5)`，分批处理 |
| 结果缓存 | Unpaywall 自带缓存，重复查询很快 |

## 失败处理

| 场景 | 处理方式 |
|------|----------|
| HTTP 404 | DOI 不存在或未被 Unpaywall 收录，跳过 |
| `is_oa: false` | 该论文无免费版本，尝试机构订阅或馆际互借 |
| `best_oa_location: null` 但 `is_oa: true` | 有 OA 版本但无直接 PDF 链接，检查 `oa_locations` 中的 `url_for_landing_page` |
| PDF 链接返回 403/404 | OA 链接可能过期，尝试 `oa_locations` 中其他来源 |
| HTTP 429 | 降低请求频率，增加 `time.sleep` |
| 无效 DOI 格式 | 检查 DOI 是否包含 `10.` 前缀，去掉 URL 中的 `https://doi.org/` |

## 相关 API

- **PubMed**：获取 DOI 后传给 Unpaywall 查找 PDF → 参见 [api-pubmed.md](api-pubmed.md)
- **Google Scholar**：搜索结果中可能包含直接 PDF 链接（尤其 arXiv 论文）→ 参见 [api-google-scholar.md](api-google-scholar.md)
- **交叉工作流**：PubMed 获取 `elocationid`（DOI）→ Unpaywall 查找 PDF → 下载
