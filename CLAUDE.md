# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Two products in one repo

1. **Static CLI (`cmd/newsletter`) — FROZEN.** The original static-site generator. Kept working for personal export; no longer evolved. Everything under "Architecture" below describes it.
2. **Dynamic product (`cmd/ingest` + `cmd/server`) — ACTIVE.** A serious, SEO-first newsletter web app backed by Postgres. This is where new work happens. See "Dynamic product" below.

## Build & Run

```bash
# Build the binary
go build -o newsletter ./cmd/newsletter

# Run directly without building
go run ./cmd/newsletter <command>

# Lint / vet
go vet ./...

# Run tests (none yet — add with go test ./...)
go test ./...
```

The `newsletter` binary must be run from the project root — it reads `config/`, `templates/`, and writes to `output/` and `.cache/` using relative paths.

```bash
# Dynamic product (needs Postgres with the pgvector extension via DATABASE_URL)
go run ./cmd/ingest    # RSS pipeline → persists articles/editions to Postgres
go run ./cmd/server    # SSR website rendered from Postgres (PORT, SITE_* env vars)
```

## Dynamic product (active)

`Ingest → filter → summarize` reuses the frozen core packages, but the **sink is Postgres, not HTML** — the DB is the source of truth; articles dedup on URL and are never deleted (archives persist).

```
cmd/ingest/            Runs the pipeline, persists articles (+ pgvector embeddings) and editions
cmd/server/            Stdlib net/http SSR site (env config: PORT, DATABASE_URL, SITE_NAME/BASE_URL/DESCRIPTION)
internal/store/        Store interface + pgx impl + embedded SQL migrations (pgvector). Vectors passed/read as text, cast ::vector
internal/web/          Renderer (embedded templates), handlers, SEO (JSON-LD, sitemap.xml, robots.txt, feed.xml)
internal/dotenv/       Shared .env loader for the new binaries
templates/embed.go     go:embed of prompts/, themes/, web/ (self-contained binaries)
templates/web/*.html   Server-side layout + partials + pages (Tailwind classes, Medium-style)
web/tailwind/input.css Tailwind v4 source → compiled to templates/web/app.css (served at /static/app.css)
```

**Styling = Tailwind CSS v4 + typography plugin (no Node).** Templates use Tailwind utility classes;
article bodies use `prose`. The standalone CLI lives at `bin/tailwindcss` (gitignored; `make tailwind`
downloads it). Edit templates or `web/tailwind/input.css`, then run **`make css`** to rebuild
`templates/web/app.css` (committed + embedded, so `go build`/Docker need no Tailwind step).

Routes: `/` (home, grouped by topic — latest edition + top articles per topic, never a mixed cross-topic feed), `/editions/{slug}` and `/topics/{topic}` both render an **edition view** (its articles + a "Past editions" `<details>` dropdown selector + prev/next) — `/topics/{topic}` shows that topic's **latest** edition, its canonical being the dated permalink; `/articles/{slug}`; `/search`; plus `/feed.xml`, `/sitemap.xml`, `/robots.txt`, `/static/app.css`, `/healthz`. `renderEditionView` is the shared renderer. There is no "Archive" and no numbered pagination — older editions are reached via the Past-editions selector / prev-next. The `message` template backs empty states + 404. Deploy: `Dockerfile` (server) + `fly.toml`; `.github/workflows/product.yml` runs `cmd/ingest` on a cron and deploys the server. Postgres is managed externally (Neon/Supabase). Roadmap (see plan): Phase 2 email subscription, Phase 3 accounts/premium, Phase 4 BYO-AI RAG chat.

## Architecture (frozen static CLI)

The pipeline runs in this order: **Ingest → Pre-filter → Embed → Score → Summarize → Render**, and it runs **once per profile** — each profile is an independent newsletter edition with its own RSS sources and its own `profile.md`.

```
cmd/newsletter/main.go          CLI dispatcher — wires components; loops over profiles in `generate`
internal/config/                YAML config + per-profile profile.md loader; RunProfileWizard writes profile.md
internal/ingestion/             Concurrent RSS fetch via gofeed → []Article
internal/filtering/             3-pass filter: recency, keyword exclusion, cosine similarity ranking
internal/embedding/             Embedder interface + Gemini/HuggingFace/Ollama impls + SHA256 JSON cache
internal/llm/                   Provider interface + Groq/Gemini/Ollama impls
internal/generation/            Renders summarize.tmpl, calls LLM, parses "TL;DR:" / "Why it matters:" lines
internal/site/                  Writes output/<profile>/YYYY-MM-DD/index.html + per-profile index; WriteHome writes the root output/index.html landing page
templates/prompts/summarize.tmpl  LLM prompt — edit this to change summary style
templates/home.html             Root landing page listing every newsletter edition
templates/themes/*.css          One file per theme; embedded inline into HTML at render time
```

## Key design rules

- **LLM and Embedder switching is config-only.** Both use Go interfaces (`llm.Provider`, `embedding.Embedder`). Adding a new provider = implement the interface + register one `case` in the factory. No other files change.
- **Multiple profiles = multiple newsletters.** `config/newsletter.yaml` holds a `profiles:` list; each entry has a `name`, its own `sources.rss`, and a `profile` path to a `config/profiles/<slug>.md`. `newsletter generate` builds all of them (or one with `--profile <name>`), writing each to `output/<slug>/`. Embedding/LLM/filtering/output blocks are global and shared. Backward-compat: a top-level `sources:` block with no `profiles:` is treated as a single `default` profile pointing at `config/profile.md`.
- **All per-edition preferences live in its `profile.md`**, not in `newsletter.yaml`. Each `profile.md` drives recency window, theme, language, level, and interests. Its full text is also that profile's embedding reference vector.
- **Embedding cache** (`.cache/embeddings.json`) is loaded at startup and flushed on exit; it is shared across profiles because entries are keyed by content SHA256 — each profile's reference vector hashes independently.
- **Cosine similarity** is implemented in stdlib (`internal/filtering/semantic.go`) — no vector DB dependency.
- **CSS is embedded inline** in every generated HTML file so the output is fully self-contained (no CDN, works offline). Each profile bakes in its own theme.

## Config files

| File | Purpose |
|---|---|
| `config/newsletter.yaml` | `profiles:` list (name + per-profile RSS sources + profile path), filtering thresholds, embedding/LLM provider selection, output root |
| `config/profiles/<slug>.md` | One per profile: interests, level, theme, language, recency window — generated by `newsletter profile setup --profile <name>` |
| `templates/prompts/summarize.tmpl` | Go text/template for the LLM summarization prompt |

The binary expects `newsletter.yaml` plus each referenced `profile.md` to exist. A missing profile file → error pointing to `newsletter profile setup --profile <name>`. Profile files hold personal data and are gitignored (injected from secrets in CI).
