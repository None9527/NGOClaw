# mem-lancedb

Local RAG memory for OpenClaw using LanceDB and Xenova Transformers.
This skill provides local, private, and fast semantic search for your memory files.

## Tools

### memory_sync
Syncs all Markdown files in the `memory/` directory to the local LanceDB database.
- Uses `all-MiniLM-L6-v2` for local embeddings (384 dimensions).
- Automatically chunks text by paragraphs.
- Only re-syncs files that have changed (MD5 check).

**Usage:**
`exec "node /home/none/clawd/skills/mem-lancedb/index.js sync"`

### memory_search
Performs a semantic search against the synced memory.
- Returns the top relevant snippets with their sources.

**Usage:**
`exec "node /home/none/clawd/skills/mem-lancedb/index.js search 'your query'"`

## Implementation Details
- **Database**: LanceDB (stored in `/home/none/clawd/.lancedb`)
- **Model**: Local Transformers (via `@xenova/transformers`)
- **No API Key Required**: Everything runs on your CPU.
