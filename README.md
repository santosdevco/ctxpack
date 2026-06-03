# ctxpack

A CLI tool that bundles your source code into a single XML (or Markdown) file optimized for AI consumption. Instead of letting the AI agent read files freely and iterate unpredictably, you hand it exactly the context it needs — nothing more, nothing less.

> **Result:** up to 60% fewer tokens used, faster responses, and a more focused agent.

---

## The problem it solves

When you give an AI agent access to your codebase without boundaries, this is what happens:

1. The agent reads one file → finds a reference → reads another file → finds another → reads another...
2. Each read costs tokens. Each iteration compounds the previous ones.
3. By the time it gives you an answer, it has consumed tokens **exponentially** — and may have drifted far from your original question.

**ctxpack breaks that loop.** You decide what goes into context. The agent works only with what you gave it.

```
Without ctxpack:   agent reads → reads → reads → reads → answers  (high token cost, unpredictable)
With ctxpack:      you pack → agent reads XML once → answers       (low token cost, focused)
```

---

## How it works

ctxpack reads your source files and produces a single structured XML (or Markdown) file with:
- A metadata summary that instructs the AI how to use the file
- A directory tree of everything included
- Each file's content wrapped in CDATA, with **exact line numbers** preserved

The AI reads one file instead of many. Line numbers stay accurate, so it can propose changes at precise locations without re-reading anything.

---

## Installation

Download the binary for your platform from the [latest release](https://github.com/santosdevco/ctxpack/releases/latest), make it executable, and move it to a directory in your `PATH`.

| Platform | File |
|---|---|
| Linux x86_64 | `ctxpack-linux-amd64` |
| Linux ARM64 | `ctxpack-linux-arm64` |
| macOS x86_64 | `ctxpack-darwin-amd64` |
| macOS Apple Silicon | `ctxpack-darwin-arm64` |
| Windows x86_64 | `ctxpack-windows-amd64.exe` |

Each binary ships with a `.sha256` checksum file. Verify before running:

```bash
sha256sum -c ctxpack-linux-amd64.sha256
```

---

## Usage

### Mode 1 — Pack a directory (no config needed)

```bash
ctxpack --path ./my-project
ctxpack --path ./src --out context.xml
```

Scans the entire directory, auto-ignoring `node_modules`, `.git`, `dist`, `build`, `vendor`, and dangerous files (`.env`, private keys, credentials).

---

### Mode 2 — Pack specific files via JSON config

```bash
ctxpack --config include.json --base ../my-project
```

The config file controls exactly which files (or line ranges) are included.

---

### All flags

| Flag | Default | Description |
|---|---|---|
| `--path` | — | Directory to pack directly, no config needed |
| `--config` | `config.json` | JSON config file (optional if `--path` is used) |
| `--base` | `.` | Base directory for relative paths |
| `--out` | `context.xml` | Output file path |
| `--format` | `xml` | Output format: `xml` or `md` |
| `--lines` | `true` | Add line numbers to every line |
| `--strip-empty` | `true` | Remove empty lines to save tokens |
| `--mask-secrets` | `false` | Mask detected secrets with `[MASKED]` |
| `--respect-gitignore` | `true` | Apply `.gitignore` patterns automatically |
| `--no-default-ignore` | `false` | Disable hardcoded ignores (`node_modules`, `.git`, etc.) |
| `--copy-prompt` | `false` | Print and copy to clipboard the AI chat intro message |

---

## Config file formats

ctxpack supports three styles in the same `include` array — mix them freely.

### Simple: full files by path or glob

```json
{
    "include": [
        "app/services/postgresql.js",
        "app/logic/Message.js",
        "app/config/*.js"
    ],
    "exclude": [
        "*.test.js",
        "*.css",
        "*.md"
    ]
}
```

### Ranges: surgical extraction — one or multiple ranges per file

Useful when a file is large but you only need specific functions or sections.
Line numbers in the output remain exact to the original file.

```json
{
    "include": [
        {
            "path": "app/services/database.js",
            "ranges": [
                { "start": 45,  "end": 90  },
                { "start": 120, "end": 155 }
            ]
        },
        {
            "path": "app/logic/Payment.js",
            "ranges": [
                { "start": 10,  "end": 60  },
                { "start": 200, "end": 250 }
            ]
        }
    ],
    "exclude": ["*.test.js"]
}
```

> **Note:** Multiple ranges from the same file must be declared in a single entry as an array. A second entry for the same file would be skipped (deduplication).

### Mixed: combine full files, ranges, and globs

```json
{
    "include": [
        "app/index.js",
        {
            "path": "app/controllers/UserController.js",
            "ranges": [
                { "start": 1,  "end": 20  },
                { "start": 85, "end": 130 }
            ]
        },
        "app/middleware/auth.js",
        "app/config/*.js"
    ],
    "exclude": ["*.test.js", "*.css"]
}
```

### Directory: pack entire folders

```json
{
    "include": [
        "app/src",
        "app/lib"
    ],
    "exclude": [
        "*.test.js",
        "*.spec.js",
        "*.css",
        "*.md"
    ]
}
```

---

## The `--copy-prompt` flag

After packing, this flag prints a ready-to-paste intro message and **copies it to your clipboard** automatically.

```bash
ctxpack --config include.json --base .. --copy-prompt
```

Output:
```
--- copy this to the start of your AI chat ---
@context.xml contains the verbatim source code of these files, with exact line numbers:
  - app/services/postgresql.js
  - app/logic/Mfa.js
  - app/logic/Message.js
  - app/logic/ExternalRoom.js

The XML is identical to what is on disk — use it as your working source, not as a reference.
Do not use file reading tools. Everything you need is already inside the XML.
For any change, cite the exact file path and line number from the XML.
----------------------------------------------
(copied to clipboard)
```

Paste this at the start of your AI chat before any request. The agent will use the XML as its working source instead of trying to read files on its own.

**Clipboard support** is handled automatically on macOS and Windows. On Linux, install any one of: `xclip`, `xsel`, or `wl-copy` (Wayland).

```bash
sudo apt install xclip   # Debian/Ubuntu
sudo dnf install xclip   # Fedora
```

---

## Complete workflow example

**Scenario:** You need to add rate limiting to your authentication service.

**Step 1 — Create a config with the relevant files:**

```json
{
    "include": [
        "app/middleware/auth.js",
        "app/services/RateLimiter.js",
        {
            "path": "app/routes/api.js",
            "ranges": [{ "start": 1, "end": 50 }]
        }
    ],
    "exclude": ["*.test.js"]
}
```

**Step 2 — Pack and copy the prompt:**

```bash
ctxpack --config auth-context.json --base ../my-project --copy-prompt
```

**Step 3 — Start a new chat, paste the prompt, then ask:**

```
@context.xml contains the verbatim source code of these files, with exact line numbers:
  - app/middleware/auth.js
  - app/services/RateLimiter.js
  - app/routes/api.js

The XML is identical to what is on disk — use it as your working source, not as a reference.
Do not use file reading tools. Everything you need is already inside the XML.
For any change, cite the exact file path and line number from the XML.

---

Add rate limiting to the /api/login route. Limit to 5 requests per minute per IP.
Use the existing RateLimiter service. Show only the changed lines.
```

The agent reads the XML once, knows exactly where the relevant code is, and gives you a surgical answer referencing exact line numbers — no wandering, no re-reads.

---

## Best practices

### Always start in a new chat

Context accumulates. An agent carrying 10 previous exchanges is slower and more likely to drift. For each task, start fresh:

1. Run ctxpack with the files relevant to that task
2. Paste the `--copy-prompt` output
3. Ask your question

### Give the minimum sufficient context

More is not better. If you're fixing a bug in one service, don't pack the entire project. The agent works best when it has exactly what it needs. Surgical configs with ranges are especially effective for large files.

### Use ranges for large files

If a file has 500 lines but the relevant function is at lines 120–180, use a range. The output will show lines 120–180 with their original numbers — the agent can propose changes at the exact right location, and you save ~90% of that file's token cost.

### Re-pack when the code changes

The XML is a snapshot. If you make changes and start a new chat, re-run ctxpack first. Stale context leads to incorrect suggestions.

### Use `--mask-secrets` on shared or uploaded contexts

If you're sharing a packed context with a team or uploading to a service, run with `--mask-secrets` to automatically redact API keys, tokens, passwords, and credential URLs.

---

## Why this matters: the token cost of an unconstrained agent

When an AI agent has access to filesystem tools and no guidance, a typical session looks like this:

| Step | Action | Tokens used |
|---|---|---|
| 1 | Read `index.js` | ~800 |
| 2 | Read `router.js` (found a reference) | ~600 |
| 3 | Read `auth.js` (found another) | ~1,200 |
| 4 | Read `config.js` (found another) | ~400 |
| 5 | Read `middleware.js` (found another) | ~900 |
| 6 | Generate answer (with all that context in window) | ~1,500 |
| **Total** | | **~5,400 tokens** |

With ctxpack (only `auth.js` and the relevant section of `router.js`):

| Step | Action | Tokens used |
|---|---|---|
| 1 | Read `context.xml` | ~1,800 |
| 2 | Generate answer | ~600 |
| **Total** | | **~2,400 tokens** |

**~55% reduction** — and the answer is more accurate because the agent didn't drift through unrelated code.

---

## Secret protection

ctxpack automatically excludes these file types regardless of config:

- `.env`, `.env.local`, `.env.production`, `.env.staging`
- `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.crt`
- `id_rsa`, `id_ed25519`, `id_dsa`, `id_ecdsa`
- `credentials.json`, `secrets.json`, `serviceAccountKey.json`
- `.netrc`, `.npmrc`

With `--mask-secrets`, it also scans file content and masks:
- Variables named `api_key`, `token`, `secret`, `password`, `bearer`, etc.
- GitHub, OpenAI, and AWS tokens by prefix pattern
- URLs with embedded credentials (`https://user:pass@host`)

---

## Output formats

### XML (default)

Structured, with metadata. Best for most AI models.

```bash
ctxpack --path ./src --out context.xml
```

### Markdown

Cleaner for models that handle markdown well, or for pasting directly into a chat.

```bash
ctxpack --path ./src --format md --out context.md
```

---

## License

MIT
