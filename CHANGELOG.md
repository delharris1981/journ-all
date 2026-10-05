# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
