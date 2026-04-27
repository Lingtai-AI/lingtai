# Tier 3 — Playwright Stealth

> Part of the [web-browsing](../SKILL.md) skill.
> See also: [stealth.md](./stealth.md) for comprehensive anti-detection techniques.

**When it applies:** JS-rendered pages, login-gated content, sites blocking simple requests.
**Tools:** `playwright` + `playwright-stealth` (**already installed**).
**Speed:** ~3-5s per page.
**⚠️ CRITICAL:** For Nature / Springer, use `domcontentloaded`, NOT `networkidle` (it hangs forever).

```python
from playwright.sync_api import sync_playwright

# playwright-stealth v2.0.3+ compatibility
try:
    from playwright_stealth import Stealth
    _apply_stealth = lambda page: Stealth().use_sync(page)
except ImportError:
    from playwright_stealth import stealth_sync
    _apply_stealth = lambda page: stealth_sync(page)

def tier3(url, wait_time=3):
    """Playwright stealth — JS-rendered or protected pages."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()
        _apply_stealth(page)

        # Block images/styles/fonts for speed
        def block_resources(route):
            if route.request.resource_type in ["image", "stylesheet", "font", "media"]:
                route.abort()
            else:
                route.continue_()
        page.route("**/*", block_resources)

        # CRITICAL: do NOT use networkidle (Nature / Springer hang forever)
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(wait_time * 1000)

        content = page.inner_text("body")
        html = page.content()
        title = page.title()
        browser.close()

        return {
            "url": page.url,
            "method": "tier3-playwright-stealth",
            "title": title,
            "body_preview": content[:5000],
            "html_len": len(html),
        }
```

### Advanced Stealth Techniques

**Custom init scripts** (override fingerprinting):
```python
# Override navigator.webdriver
page.add_init_script("Object.defineProperty(navigator, 'webdriver', {get: () => undefined})")

# Override chrome runtime
page.add_init_script("""
window.chrome = { runtime: {}, loadTimes: function(){}, csi: function(){}, app: {} };
""")

# Override languages
page.add_init_script("""
Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
""")
```

**Persistent sessions** (for login-gated sites):
```python
context = p.chromium.launch_persistent_context(
    user_data_dir="./browser_data",
    headless=True,
)
# ... work ...
context.storage_state(path="cookies.json")  # Save session
```

**Smart rate limiting:**
```python
import time, random
def smart_delay(base=2.0, jitter=1.0):
    """Random delay to avoid pattern detection."""
    time.sleep(base + random.random() * jitter)
```

**User-Agent rotation:**
```python
import random
USER_AGENTS = [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120.0.0.0",
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120.0.0.0",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/17.2",
]
```

**Use when:** Tier 1 / 1.5 / 2 fail, or the page genuinely requires JS rendering or bypasses anti-bot.

---

## Extended Stealth Techniques (from stealth-browsing sub-skill)

### How Websites Detect Bots

Understanding detection methods helps you choose the right countermeasure.

#### 1. Browser Fingerprinting

Websites build a unique fingerprint from:
- **Canvas/WebGL:** GPU rendering differences create unique hashes
- **Audio context:** Audio processing differences
- **Font enumeration:** Installed fonts list
- **Screen:** Resolution + color depth
- **Hardware:** CPU cores, device memory
- **Platform:** OS + browser version consistency

#### 2. WebDriver Detection

Automated browsers leak signals:
- `navigator.webdriver === true` (Playwright/Selenium)
- Missing `window.chrome` object in headless
- CDP (Chrome DevTools Protocol) detection
- `$cdc_` variable injection by ChromeDriver
- Missing plugins/mimeTypes

#### 3. TLS/JA3 Fingerprinting

- Python `requests` has a distinctive TLS fingerprint
- Headless browsers may differ from real ones

#### 4. Behavioral Analysis

- Mouse movement patterns (too perfect = bot)
- No scroll behavior
- Click timing (instant = bot)
- Request frequency patterns

#### 5. IP/Network Level

- Datacenter IP detection
- Proxy detection headers
- Rate limiting per IP
- Geo-inconsistency

### Additional Manual Stealth Overrides

For sites that detect `playwright-stealth`, apply additional overrides:

**Override permissions API:**
```python
page.add_init_script("""
    const originalQuery = window.navigator.permissions.query;
    window.navigator.permissions.query = (parameters) =>
        parameters.name === 'notifications'
            ? Promise.resolve({ state: Notification.permission })
            : originalQuery(parameters);
""")
```

**Override plugins:**
```python
page.add_init_script("""
    Object.defineProperty(navigator, 'plugins', {
        get: () => [1, 2, 3, 4, 5],
    });
""")
```

**Full Stealth Page (All Overrides):**
```python
def create_stealth_page(browser):
    """Create a fully stealthed page with all overrides."""
    page = browser.new_page()
    _apply_stealth(page)
    
    # Additional overrides beyond stealth_sync
    page.add_init_script("""
        // WebGL vendor/renderer override
        const getParameter = WebGLRenderingContext.prototype.getParameter;
        WebGLRenderingContext.prototype.getParameter = function(param) {
            if (param === 37445) return 'Intel Inc.';
            if (param === 37446) return 'Intel Iris OpenGL Engine';
            return getParameter.call(this, param);
        };
        
        // Hardware concurrency override
        Object.defineProperty(navigator, 'hardwareConcurrency', {
            get: () => 8
        });
        
        // Device memory override
        Object.defineProperty(navigator, 'deviceMemory', {
            get: () => 8
        });
    """)
    
    return page
```

### Proxy Strategies

**HTTP/HTTPS Proxy:**
```python
# With requests
proxies = {"http": "http://user:pass@proxy:8080",
           "https": "http://user:pass@proxy:8080"}
r = requests.get(url, proxies=proxies)

# With Playwright
context = browser.new_context(proxy={"server": "http://proxy:8080",
                                      "username": "user",
                                      "password": "pass"})
```

**SOCKS5 Proxy:**
```python
# With requests (requires pip install requests[socks])
proxies = {"http": "socks5://user:pass@proxy:1080",
           "https": "socks5://user:pass@proxy:1080"}

# With Playwright
context = browser.new_context(proxy={"server": "socks5://proxy:1080"})
```

**Proxy Pool Rotation:**
```python
from itertools import cycle

proxies = [
    "http://user:pass@proxy1:8080",
    "http://user:pass@proxy2:8080",
    "http://user:pass@proxy3:8080",
]
proxy_pool = cycle(proxies)

def request_with_rotation(url):
    """Make a request with rotating proxy."""
    proxy = next(proxy_pool)
    return requests.get(url, proxies={"http": proxy, "https": proxy},
                       headers={"User-Agent": random_ua()})
```

**When to use proxies:** Getting IP-blocked, need geo-diversity, heavy scraping.
**When NOT to:** Simple one-off requests, free API queries.

### Session Management (Cookie Persistence)

For sites that require login or maintain session state:

```python
from playwright.sync_api import sync_playwright

# playwright-stealth v2.0.3+ compatibility
try:
    from playwright_stealth import Stealth
    _apply_stealth = lambda page: Stealth().use_sync(page)
except ImportError:
    from playwright_stealth import stealth_sync
    _apply_stealth = lambda page: stealth_sync(page)

def persistent_session(url, user_data_dir="./browser_session", save_cookies=True):
    """Use persistent browser context to maintain cookies/sessions."""
    with sync_playwright() as p:
        context = p.chromium.launch_persistent_context(
            user_data_dir=user_data_dir,
            headless=True,
            user_agent=random_ua(),
        )
        page = context.new_page()
        _apply_stealth(page)
        
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(3000)
        
        content = page.inner_text("body")
        
        if save_cookies:
            context.storage_state(path=f"{user_data_dir}/storage_state.json")
        
        context.close()
        return content

def load_session(url, storage_state_path="./browser_session/storage_state.json"):
    """Load a previously saved session."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(storage_state=storage_state_path)
        page = context.new_page()
        _apply_stealth(page)
        
        page.goto(url, wait_until="domcontentloaded")
        content = page.inner_text("body")
        
        browser.close()
        return content
```

**When to use:** Sites requiring login (LinkedIn, some forums), sites with CSRF tokens.

### CAPTCHA Decision Tree

```
CAPTCHA encountered ──────┐
                         │
    ┌─────────────────────┼─────────────────────┐
    │                    │                    │
  reCAPTCHA/          Simple/              Cloudflare
  hCaptcha            Image CAPTCHA        Challenge
    │                    │                    │
    ▼                    ▼                    ▼
  STOP.              Try refreshing       Try nodriver
  Do NOT attempt     the page. If         (if installed).
  to solve.          persistent,          If still blocked,
  Switch to          lower request        STOP.
  another            frequency.           Switch to API
  data source.                            or different
  (API, RSS,                              data source.
  different site)
```

**Key Rules:**

1. **Never try to solve reCAPTCHA/hCaptcha programmatically** — it's unethical and usually futile
2. **Cloudflare challenge** → try `nodriver` library or switch to API
3. **Simple image CAPTCHA** → refresh the page, try again with different fingerprint
4. **Rate-triggered CAPTCHA** → slow down! You're going too fast

### Rate Limiting

**RateLimiter Class:**
```python
import time, random
from collections import defaultdict

class RateLimiter:
    """Per-domain rate limiter with jitter.
    
    Usage:
        limiter = RateLimiter(min_interval=2.0)
        limiter.wait("example.com")
        r = requests.get(url)
    """
    def __init__(self, min_interval=2.0):
        self.last_request = defaultdict(float)
        self.min_interval = min_interval
    
    def wait(self, domain):
        """Wait if needed before making a request to this domain."""
        elapsed = time.time() - self.last_request[domain]
        if elapsed < self.min_interval:
            wait_time = self.min_interval - elapsed + random.random()
            time.sleep(wait_time)
        self.last_request[domain] = time.time()
    
    def set_interval(self, domain, interval):
        """Override interval for a specific domain."""
        self.min_interval = interval

# Global instance
rate_limiter = RateLimiter(min_interval=2.0)
```

**Exponential Backoff:**
```python
def exponential_backoff(attempt, base=1.0, max_delay=60.0):
    """Exponential backoff with jitter for retries.
    
    Use when getting 429 or 5xx errors.
    """
    delay = min(base * (2 ** attempt) + random.random(), max_delay)
    time.sleep(delay)
    return delay
```

**Site-Specific Rate Limits:**

| Site | Max Rate | Notes |
|------|----------|-------|
| Google Scholar | ~10 req/IP then CAPTCHA | Use SerpAPI instead |
| Reddit | 60 req/min | Must set User-Agent |
| Wikipedia | Reasonable use | Be nice, cache results |
| GitHub | 60/hr (5000/hr with token) | Use token for heavy use |
| arXiv | Be reasonable | No explicit limit |
| Nature/Springer | Aggressive blocking | Use API when possible |
| Twitter/X | Very aggressive | Requires login |

### Site-Specific Stealth Notes

**Google Scholar:**
- Max ~10 requests from same IP before CAPTCHA
- Playwright stealth works briefly but not reliably
- **Better approach:** Use SerpAPI/Serper for Scholar results
- If you must scrape: residential proxy + long delays (10s+)

**Twitter/X:**
- Requires login for most content now
- API requires paid tier
- **Alternative:** Nitter instances (if available): `https://nitter.net/user`
- Do NOT attempt to bypass login wall

**LinkedIn:**
- Most aggressive bot detection of any major site
- Requires persistent session + residential proxy
- **Better approach:** Use LinkedIn API if available
- Do NOT scrape LinkedIn profiles at scale

**Reddit:**
- `old.reddit.com` is easier to scrape than new UI
- JSON API: append `.json` to any URL + User-Agent header
- Rate limit: 60 req/min
- **Best approach:** Use JSON API, not HTML scraping

**Medium:**
- Client-side paywall (trafilatura often gets full text)
- `?source=friends_link` sometimes bypasses paywall
- Jina Reader can often extract full articles
- **Try:** trafilatura → Jina Reader → give up

**News Sites:**
- Most use client-side paywalls (JS-based)
- trafilatura often extracts before paywall triggers
- RSS feeds give headlines + summaries for free
- **Strategy:** RSS first → trafilatura → Jina Reader → Wayback Machine

### nodriver: Maximum Stealth (Alternative to Playwright)

For sites that detect Playwright specifically:

```python
# nodriver (formerly undetected-chromedriver)
# pip install nodriver

import nodriver as uc

async def nodriver_fetch(url):
    """Fetch using nodriver — most stealthy Python option.
    
    nodriver patches Chrome at a lower level than playwright-stealth,
    making it harder to detect. Slower startup but more reliable
    against advanced bot detection.
    """
    browser = await uc.start()
    page = await browser.get(url)
    await page.sleep(3)  # Wait for JS
    content = await page.get_content()
    return content
```

**When to use:** Playwright stealth is detected (Cloudflare, some banking sites).
**When NOT to:** Standard scraping — Playwright is faster and more reliable.

### Complete Stealth Workflow

```python
def stealth_workflow(url, max_retries=2):
    """Complete stealth browsing workflow with fallback.
    
    1. Try requests + random UA (lightest)
    2. Try trafilatura (fast extraction)
    3. Try Playwright stealth (JS rendering)
    4. Try Jina Reader (cloud fallback)
    """
    rate_limiter.wait(urlparse(url).netloc)
    
    # Step 1: Simple request
    try:
        r = requests.get(url, headers={"User-Agent": random_ua()}, timeout=15)
        if r.status_code == 200 and len(r.text) > 500:
            return {"method": "requests", "content": r.text[:5000]}
    except Exception:
        pass
    
    # Step 2: trafilatura
    try:
        import trafilatura
        html = trafilatura.fetch_url(url)
        if html:
            text = trafilatura.extract(html)
            if text and len(text) > 200:
                return {"method": "trafilatura", "content": text[:5000]}
    except Exception:
        pass
    
    # Step 3: Playwright stealth
    try:
        result = stealth_fetch(url)
        if result.get("body") and len(result["body"]) > 200:
            return {"method": "playwright-stealth", "content": result["body"][:5000]}
    except Exception:
        pass
    
    # Step 4: Jina Reader
    try:
        r = requests.get(f"https://r.jina.ai/{url}", timeout=45,
                         headers={"Accept": "text/markdown"})
        if r.status_code == 200 and len(r.text) > 200:
            return {"method": "jina-reader", "content": r.text[:5000]}
    except Exception:
        pass
    
    return {"method": "failed", "error": "All stealth methods failed"}
```

### Dependencies

```bash
# Core (already installed in LingTai)
pip install playwright playwright-stealth
playwright install chromium

# Optional
pip install nodriver          # Maximum stealth alternative
pip install requests[socks]   # SOCKS5 proxy support
```
