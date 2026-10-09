# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Changed
- Search now uses an FTS5 index instead of `LIKE '%q%'`, so it stays fast as the collection grows and understands word prefixes (`"loa"*`) and quoted phrases
- Search matches whole words rather than substrings: `loaf` finds "Rye loaf", `af` no longer matches it. A search that FTS5 cannot parse (a stray quote, `AND` on its own, or a hyphenated word like `e-mail`) falls back to the old substring scan rather than erroring
- The admin page reports failures instead of showing success: a failed user create, disable, or signup toggle now shows an error, and a failed user list load renders the page with the message instead of a 500

### Added
- Tests covering the migrations themselves, including that an existing database's entries are backfilled into the search index on upgrade

## [1.4.1] - 2026-10-09
### Fixed
- Date filter on the entries page silently matched nothing for any date with more than 100 newer entries — filtering happened after the newest 100 entries were fetched. It now filters in the database, so the limit applies to the matching entries
- Loading a list page ran one extra query per entry to fetch tags; tags are now fetched for the whole page at once (~7x faster on a 100-entry page)

### Changed
- All SQL for entries and tags moved out of the handler layer into `internal/store`, which is now covered by tests
- `GetEntries` and `GetEntriesByTag` were the same query with a different filter and are now one query builder

## [1.4.0] - 2026-10-05
### Added
- Version number shown in the app footer
- Export now defaults to a zip of one Markdown file per entry (JSON and single-file Markdown still available via `?format=`)
- Import page accepts a zip of separate .md files, one entry per file
- Admin panel: create users, enable/disable accounts, and toggle public signups; the first account is admin
### Changed
- Markdown formatting styles (headings H1-H6, tables, code, quotes, lists) now use theme colours and fonts
- Disabled accounts can no longer log in, and active sessions are invalidated

## [1.3.0] - 2026-10-05
### Added
- Clickable calendar: each day links to the entries for that date, with an "All entries" way back
- Light/dark theme selection with a toggle in the nav, persisted to localStorage and defaults to the OS preference

## [1.2.0] - 2026-10-04

### Added
- Full Markdown format toolbar: H1-H6, bold, italic, strikethrough, inline code, bullet/numbered/task lists, blockquote, link, image, table, fenced code block, divider, line break
- Toolbar buttons are generated from a single format spec, so adding a format is one entry

### Fixed
- Toolbar rendered on `document`, not `document.body` — `DOMContentLoaded` never reached the old listener, leaving an empty toolbar
- Numbered list no longer duplicates the line text
- Toolbar moved above the editor grid so the write and preview panes share a top edge
- Calendar always renders whole weeks (was pulling in the next month's first day and leaving a short final row)
- Grid pages (editor, calendar) get 72rem instead of being squeezed into the 68ch reading column
- Static assets send `Cache-Control: no-cache` so browsers pick up new app.js/app.css

## [1.1.0] - 2026-10-04

### Added
- Markdown formatting toolbar in the entry editor (H1/H2/H3, bold, italic, inline code, bullet and numbered lists, quote, link, divider) for people who don't know Markdown syntax

## [1.0.0] - 2026-10-04

### Added
- Multi-user authentication (signup, login, logout)
- Markdown editor with live preview
- Entry CRUD with title and body
- Calendar view with entry indicators
- Tags and tag filtering
- Full-text search
- Export to JSON and Markdown
- Self-hostable Docker container (Unraid-ready, uid 99:100)
- GitHub Actions workflow for multi-arch Docker builds

### Fixed
- Date parsing for SQLite datetime format
- Calendar template uses OtherMonth field
- Copy web templates and static into docker image
- Copy migrations into docker image
- Bump golang to 1.27 in Dockerfile

## [0.1.0] - 2026-10-04

### Added

- Initial release
- Multi-user authentication (signup, login, logout)
- Markdown editor with live preview
- Entry CRUD with title and body
- Calendar view with entry indicators
- Tags and tag filtering
- Full-text search
- Export to JSON and Markdown
- Self-hostable Docker container (Unraid-ready, uid 99:100)
- GitHub Actions workflow for multi-arch Docker builds
