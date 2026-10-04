# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
