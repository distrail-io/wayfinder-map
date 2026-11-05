# Research: Zed preview, Zed Mermaid, Herdr manifest

Sources: source at pinned tags (Zed `v1.27.0`, merman `=0.8.0-alpha.5`, herdr `v0.9.5`) and official docs, read 2026-11-05. Not yet confirmed by hand unless noted.

## Zed Markdown preview live refresh

Yes, if the buffer is not dirty.

- Worktree watcher → `Buffer::file_updated` emits `ReloadNeeded` only when `!was_dirty` (`crates/language/src/buffer.rs` L1795-1826).
- `Project::on_buffer_event` reloads (skipped for collab guests) (`crates/project/src/project.rs` L4121-4125).
- Reload applies a diff → `BufferEdited` → preview re-parse, 200ms throttle (`crates/markdown_preview/src/markdown_preview_view.rs` L63, L561-577, L648-721).
- Mermaid blocks are cached by source, so unchanged diagrams don't re-render.
- Caveat: unsaved edits to `MAP.md` in Zed put the buffer in conflict and stop the updates. Never edit it by hand.

## Zed Mermaid (merman 0.8.0-alpha.5, Mermaid 11.16.1 parity)

- The SVG is **rasterised** (resvg) and shown as an image.
- Only closed ```` ```mermaid ```` fences with a known diagram keyword render. Parse errors fall back to showing the source as a code view.
- Flowchart parity is full: subgraphs, all node shapes, quoted labels, `#quot;`-style entities.
- **Node colours from `classDef` / `class` / `:::` / `style` are overridden.**
  - Zed adds `zed-accent-N` classes to every node and strips `!important` from merman's output.
  - It then injects its own `!important` fill/stroke rules (`crates/mermaid_render/src/postprocess/accent_colors.rs`, `inject_css.rs`).
  - Tracked in zed-industries/zed#66485 (open).
- `click` is parsed but **not clickable**, because the diagram is a raster image.
- `linkStyle` edge stroke probably survives (medium confidence).
- Emoji probably render through font fallback; box widths may be off.
- Inline HTML in labels does not render.

Implication: encode status with emoji and text in labels, grouping (subgraphs) and edge style, not with node colour. Put issue links in a Markdown list under the diagram.

## Herdr 0.9.5 plugin manifest

`[[panes]]` (`src/app/api/plugins/manifest.rs` L73-87, unknown fields silently ignored):

- Fields:
  - `id` (required; `[A-Za-z0-9:_-]`, no dots)
  - `title` (required)
  - `description`
  - `platforms`
  - `placement` (`overlay` default, `popup`, `split`, `tab` or `zoomed`)
  - `width` / `height` (popup only)
  - `command` (argv array, no shell)
- There is no `cwd`, `env` or `direction` field in the manifest. Those are per-open parameters on `plugin pane open`.
- `command[0]` containing `/` resolves against the plugin root. A bare name is looked up on `PATH`. The default cwd is the plugin root.
- Every open spawns a new process. Closing the pane kills it. On session restore, panes come back as plain shells (no relaunch).
- The CLI `plugin pane open --placement` lacks `popup` in 0.9.5, but the manifest and socket accept it.

Sidebar tokens (`src/config/sidebar.rs`):

- Rows reference a custom token as `$name`.
- `herdr workspace report-metadata <ws> --source ID --token name=value --ttl-ms N` feeds spaces rows.
- Values are at most 80 chars, with at most 16 keys per report. Tokens are lost on server restart.

`ui.tab_bar_right` (`src/config/tab_bar.rs`):

- Format: `{type="command", command="<shell string>", interval_seconds, timeout_seconds}`.
- The command runs through `/bin/sh -lc`, and the last line of output is shown.
