#!/home/none/clawd/skills/web-research/.venv/bin/python3
import asyncio
import aiohttp
import json
import sys
import re
import trafilatura
from datetime import datetime
from urllib.parse import urlencode
from playwright.async_api import async_playwright

# Config
SEARXNG_URL = "http://127.0.0.1:8888/search"
MAX_CONCURRENT_FETCH = 5
DEFAULT_LIMIT = 8
DEEP_FETCH_LIMIT = 5

class WebResearchAgent:
    def __init__(self, searxng_url):
        self.searxng_url = searxng_url
        self.headers = {
            "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
        }

    async def search_searxng(self, session, query, time_range="", limit=10):
        """Async search via SearXNG (Try JSON, fallback to HTML)."""
        # Try JSON first
        params = {
            "q": query,
            "categories": "general",
            "language": "zh-CN",
            "time_range": time_range,
            "format": "json" 
        }
        url = f"{self.searxng_url}?{urlencode(params)}"
        
        try:
            async with session.get(url, headers=self.headers, timeout=10) as resp:
                if resp.status == 200:
                    try:
                        data = await resp.json()
                        return self._parse_searxng_json(data, limit)
                    except:
                        pass # Fallback to HTML if JSON parse fails
                elif resp.status == 403 or resp.status == 400:
                     print(f"JSON API denied ({resp.status}), falling back to HTML...", file=sys.stderr)
                else:
                    print(f"SearXNG Error {resp.status}: {await resp.text()}", file=sys.stderr)
                    return []
        except Exception as e:
            print(f"Error searching JSON '{query}': {e}", file=sys.stderr)

        # Fallback to HTML
        params["format"] = "html"
        url = f"{self.searxng_url}?{urlencode(params)}"
        try:
            async with session.get(url, headers=self.headers, timeout=10) as resp:
                if resp.status == 200:
                    content = await resp.text()
                    return self._parse_searxng_html(content, limit)
        except Exception as e:
            print(f"Error searching HTML '{query}': {e}", file=sys.stderr)
        return []

    def _parse_searxng_json(self, data, limit):
        """Parse structured JSON from SearXNG."""
        results = []
        if "results" not in data:
            return []
        
        for res in data["results"]:
            if not res.get("url"): continue
            
            results.append({
                "title": res.get("title", ""),
                "url": res.get("url"),
                "content": res.get("content", ""),
                "publishedDate": res.get("publishedDate", "")
            })
            if len(results) >= limit:
                break
        return results

    def _parse_searxng_html(self, content, limit):
        """Robustly parse SearXNG HTML results (Regex Fallback)."""
        results = []
        # Find all article containers
        articles = re.findall(r"<article.*?>(.*?)</article>", content, re.DOTALL)
        
        for art in articles:
            try:
                # Extract Title & Link
                title_match = re.search(r"<h3><a href=\"(.*?)\".*?>(.*?)</a></h3>", art, re.DOTALL)
                if not title_match: continue
                url = title_match.group(1)
                title = re.sub(r"<.*?>", "", title_match.group(2)).strip()
                
                # Extract Snippet
                snippet_match = re.search(r"<p class=\"content\">(.*?)</p>", art, re.DOTALL)
                snippet = re.sub(r"<.*?>", "", snippet_match.group(1)).strip() if snippet_match else ""

                if title and url:
                    results.append({
                        "title": title,
                        "url": url,
                        "content": snippet,
                        "publishedDate": "" # Hard to extract date from HTML reliably without more regex
                    })
                    if len(results) >= limit:
                        break
            except Exception:
                continue
        return results

    async def fetch_and_scrape(self, session, url):
        """Async fetch and extract clean markdown using trafilatura (Fast Leg)."""
        try:
            async with session.get(url, headers=self.headers, timeout=15) as resp:
                if resp.status == 200:
                    html_content = await resp.text()
                    # trafilatura extraction
                    downloaded = trafilatura.extract(html_content, output_format='markdown', 
                                                   include_links=True, include_images=False,
                                                   include_tables=True)
                    return downloaded if downloaded else ""
        except Exception as e:
            # print(f"Error fetching {url}: {e}", file=sys.stderr)
            pass
        return ""

    async def fetch_with_playwright(self, url):
        """Fallback: Fetch using Headless Browser (Steady Leg)."""
        print(f"  [Steady Leg] Activating Playwright for {url}...", file=sys.stderr)
        try:
            async with async_playwright() as p:
                browser = await p.chromium.launch(headless=True)
                page = await browser.new_page()
                # Set a realistic user agent
                await page.set_extra_http_headers(self.headers)
                try:
                    await page.goto(url, timeout=30000, wait_until="domcontentloaded")
                    # Wait a bit for JS to render
                    await page.wait_for_timeout(2000) 
                    content = await page.content()
                    downloaded = trafilatura.extract(content, output_format='markdown', 
                                                   include_links=True, include_images=False,
                                                   include_tables=True)
                    return downloaded if downloaded else ""
                finally:
                    await browser.close()
        except Exception as e:
            print(f"  [Steady Leg] Playwright error for {url}: {e}", file=sys.stderr)
        return ""

    async def run(self, queries, time_range, deep_mode=False):
        async with aiohttp.ClientSession() as session:
            # 1. Concurrent Search
            search_tasks = [self.search_searxng(session, q, time_range, DEFAULT_LIMIT) for q in queries]
            search_results = await asyncio.gather(*search_tasks)
            
            # Flatten and deduplicate
            seen_urls = set()
            all_entries = []
            for results in search_results:
                for res in results:
                    url = res.get("url")
                    if url and url not in seen_urls:
                        seen_urls.add(url)
                        all_entries.append({
                            "title": res.get("title"),
                            "url": url,
                            "snippet": res.get("content"),
                            "date": res.get("publishedDate", "")
                        })
            
            # 2. Concurrent Fetch if deep mode
            if deep_mode:
                top_entries = all_entries[:DEEP_FETCH_LIMIT]
                print(f"Deep Mode: Scraping top {len(top_entries)} pages...", file=sys.stderr)
                
                # First pass: Fast Leg (aiohttp)
                fetch_tasks = [self.fetch_and_scrape(session, entry["url"]) for entry in top_entries]
                extracted_texts = await asyncio.gather(*fetch_tasks)
                
                for entry, text in zip(top_entries, extracted_texts):
                    # Check if fast scrape failed or returned too little content
                    # If failed, text is "" or very short
                    if not text or len(text) < 200:
                        # Second pass: Steady Leg (Playwright) - doing sequentially to avoid resource spike
                        # (Ideally we'd also batch this, but for MVP sequential is safer for memory)
                        text = await self.fetch_with_playwright(entry["url"])
                    
                    if text:
                        if len(text) > 200:
                            entry["full_content_md"] = text
                        else:
                            extracted_text_fallback = entry.get("snippet", "")
                            entry["full_content_md"] = f"(Content too short: {len(text)} chars, fallback to snippet): {extracted_text_fallback}"
                    else:
                        extracted_text_fallback = entry.get("snippet", "")
                        entry["full_content_md"] = f"(Scrape failed, fallback to snippet): {extracted_text_fallback}"
                
                return top_entries
            
            return all_entries

def parse_args():
    queries = []
    time_range = ""
    deep_mode = False
    
    args = sys.argv[1:]
    i = 0
    while i < len(args):
        arg = args[i]
        if arg in ["--day", "-d"]: time_range = "day"
        elif arg in ["--week", "-w"]: time_range = "week"
        elif arg in ["--month", "-m"]: time_range = "month"
        elif arg in ["--year", "-y"]: time_range = "year"
        elif arg == "--deep": deep_mode = True
        elif arg.startswith("-"): pass # ignore unknown flags
        else: queries.append(arg)
        i += 1
    return queries, time_range, deep_mode

async def main():
    queries, time_range, deep_mode = parse_args()
    if not queries:
        print(json.dumps({"error": "No query provided"}))
        return

    agent = WebResearchAgent(SEARXNG_URL)
    results = await agent.run(queries, time_range, deep_mode)
    
    # Final Output as JSON
    print(json.dumps(results, ensure_ascii=False, indent=2))

if __name__ == "__main__":
    asyncio.run(main())