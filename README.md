# Journ-all

A self-hosted journaling app. Write, tag, browse by calendar, export everything.

## Quick start

```bash
cp .env.example .env
# edit .env — set SESSION_SECRET
docker compose up -d
```

Open http://localhost:8080 — sign up, start writing.

## Unraid

1. Copy `docker-compose.yml`, change image to `ghcr.io/<you>/journ-all:latest`
2. Set `SESSION_SECRET` in environment
3. Map `/mnt/user/appdata/journ-all` → `/data`
4. Container runs as uid 99 gid 100 (Unraid `nobody`) — no permission issues

## Features

- Multi-user with signup/login
- Markdown editor with live WYSIWYG rendering (typing `# Title` becomes a heading in place) and full formatting toolbar (H1-H6, bold, italic, strikethrough, code, lists, task lists, quote, link, image, table, divider)
- Calendar view with entry indicators
- Tags and filtering
- Full-text search
- Export to JSON or Markdown
- Single SQLite database — easy backup

## Tech

Go 1.22 · SQLite (modernc, pure Go) · htmx · Newsreader + Inter

## Env vars

| Var | Default | Notes |
|-----|---------|-------|
| `PORT` | `8080` | |
| `DATABASE_PATH` | `/data/journall.db` | |
| `SESSION_SECRET` | — | **Required** |

## Build

```bash
docker build -t journ-all .
docker run -p 8080:8080 -v $(pwd)/data:/data -e SESSION_SECRET=changeme journ-all
```
