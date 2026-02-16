const lancedb = require('vectordb');
const { pipeline } = require('@xenova/transformers');
const fs = require('fs');
const path = require('path');
const { globSync } = require('glob');
const md5 = require('md5');

// Configuration
const BASE_DIR = '/home/none/clawd';
const MEMORY_DIR = path.join(BASE_DIR, 'memory');
const DB_PATH = path.join(BASE_DIR, '.lancedb');
const STATE_FILE = path.join(DB_PATH, 'sync_state.json');
const TABLE_NAME = 'memories';

let extractor;
let db;
let table;

/**
 * Initialize Embedding Model and Database
 */
async function init() {
    if (!fs.existsSync(DB_PATH)) fs.mkdirSync(DB_PATH, { recursive: true });
    
    // Load local embedding model (free, private, fast)
    if (!extractor) {
        extractor = await pipeline('feature-extraction', 'Xenova/all-MiniLM-L6-v2');
    }

    db = await lancedb.connect(DB_PATH);
    
    const tableNames = await db.tableNames();
    if (!tableNames.includes(TABLE_NAME)) {
        // Dummy data to initialize schema
        const dummyData = [{
            vector: Array(384).fill(0),
            text: 'init',
            source: 'system',
            hash: '0'
        }];
        table = await db.createTable(TABLE_NAME, dummyData);
    } else {
        table = await db.openTable(TABLE_NAME);
    }
}

/**
 * Generate Embedding for text
 */
async function getEmbedding(text) {
    const output = await extractor(text, { pooling: 'mean', normalize: true });
    return Array.from(output.data);
}

/**
 * Sync Memory Files to Database
 */
async function sync() {
    await init();
    
    const files = globSync(path.join(MEMORY_DIR, '**/*.md'));
    let state = {};
    if (fs.existsSync(STATE_FILE)) {
        state = JSON.parse(fs.readFileSync(STATE_FILE, 'utf-8'));
    }

    let updated = false;
    for (const file of files) {
        const content = fs.readFileSync(file, 'utf-8');
        const currentHash = md5(content);
        const relativePath = path.relative(BASE_DIR, file);

        if (state[relativePath] === currentHash) continue;

        console.log(`Syncing ${relativePath}...`);
        
        // Simple paragraph-based chunking
        const chunks = content.split(/\n\n+/).filter(p => p.trim().length > 10);
        const data = [];

        for (const chunk of chunks) {
            const vector = await getEmbedding(chunk);
            data.push({
                vector,
                text: chunk,
                source: relativePath,
                hash: currentHash
            });
        }

        if (data.length > 0) {
            // Remove old version of this file from DB
            await table.delete(`source = '${relativePath}'`);
            // Add new chunks
            await table.add(data);
            state[relativePath] = currentHash;
            updated = true;
        }
    }

    if (updated) {
        fs.writeFileSync(STATE_FILE, JSON.stringify(state, null, 2));
    }
    return { status: 'success', syncedFiles: Object.keys(state).length };
}

/**
 * Search Memory
 */
async function search(query, limit = 5) {
    await init();
    const queryVector = await getEmbedding(query);
    const results = await table
        .search(queryVector)
        .limit(limit)
        .execute();
    
    return results.map(r => ({
        text: r.text,
        source: r.source,
        score: r._distance // Lower is better (L2 distance)
    }));
}

// Simple CLI interface for OpenClaw to call
const action = process.argv[2];
const param = process.argv[3];

if (action === 'sync') {
    sync().then(res => console.log(JSON.stringify(res))).catch(err => { console.error(err); process.exit(1); });
} else if (action === 'search') {
    search(param).then(res => console.log(JSON.stringify(res))).catch(err => { console.error(err); process.exit(1); });
} else {
    console.log('Usage: node index.js [sync|search] "query"');
}
