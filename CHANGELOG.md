# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [v0.1.0] - 2026-02-05

### Initial Release

GopherWiki is a Go translation of [An Otter Wiki](https://github.com/redimp/otterwiki), a Python-based wiki application.

### Features

- **Core Wiki Functionality**
  - View, create, edit, and delete wiki pages
  - Markdown rendering with goldmark
  - WikiLinks support (`[[Page]]` and `[[Page|Title]]`)
  - Page attachments with image thumbnails
  - Full-text search across pages

- **Git-Based Storage**
  - All content stored in Git repository
  - Full page history with diff view
  - Blame view showing line-by-line authorship
  - Revert to previous revisions
  - Git HTTP server for clone/pull/push

- **User Management**
  - User registration and authentication
  - Configurable access control (ANONYMOUS, REGISTERED, APPROVED)
  - Admin panel for user management
  - Password recovery via email

- **Extended Markdown**
  - Tables (GFM style)
  - Task lists (`- [x] done`)
  - Footnotes
  - Syntax highlighting with Chroma
  - Mermaid diagram support
  - GitHub-style alerts (`> [!NOTE]`)
  - Highlighted text (`==marked==`)
  - Table of contents generation

- **Editor Features**
  - CodeMirror-based editor
  - Draft autosave
  - Live preview

- **Additional Features**
  - RSS and Atom feeds
  - Sitemap generation
  - Dark mode support
  - Customizable sidebar
  - Health check endpoint
  - Single binary deployment with embedded assets

### Technology Stack

- Go with Chi router
- goldmark for Markdown
- Chroma for syntax highlighting
- go-git for Git operations
- SQLite with sqlc
- gorilla/sessions for session management
