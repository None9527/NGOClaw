# Web Research Skill

## Description
Perform comprehensive web research using a local SearXNG instance and a custom Python agent (`research.py`).
This skill is designed for "Deep Dives" — when you need more than just surface-level snippets.

## Capabilities
- **Multi-Source Search**: Aggregates results from Google, Bing, DDG, etc. via SearXNG.
- **Deep Scraping**: Asynchronously fetches and extracts full article content (Markdown) using `trafilatura`.
- **Anti-Bot Fallback**: "Two-Legged" approach — attempts fast Python scraping first; falls back to Headless Browser if blocked.

## Usage Instructions

### 1. The "Fast Leg" (Default)
Run the `research.py` script for most queries. It handles concurrency and content cleaning.

```bash
/home/none/clawd/skills/web-research/.venv/bin/python3 /home/none/clawd/skills/web-research/research.py "Your Query Here" --deep
```

**Options:**
- `--deep`: Fetches full content of top results (Recommended for complex questions).
- `--day / --week / --month / --year`: Filters results by time.

### 2. The "Steady Leg" (Fallback)
**IF AND ONLY IF** `research.py` returns an empty JSON list `[]` or an error indicating anti-bot blocking (e.g., 403, Cloudflare challenge):

**DO NOT give up.** Switch to the `browser` tool immediately.

1.  **Search**: Use `browser` to visit a search engine directly (e.g., `https://www.google.com/search?q=...` or the local SearXNG URL).
2.  **Browse**: Click on promising results.
3.  **Read**: Use `browser` snapshot/text actions to read the content.

## Example Workflow
> User: "Analyze the latest DeepSeek technical report."

1.  **Agent**: Runs `research.py "DeepSeek technical report" --deep`
2.  **Scenario A (Success)**: Script returns JSON with full markdown. Agent summarizes.
3.  **Scenario B (Blocked)**: Script returns `[]` (empty).
    *   **Agent**: "Script blocked. Switching to browser..."
    *   **Agent**: Calls `browser` tool to navigate to Google/SearXNG manually.
