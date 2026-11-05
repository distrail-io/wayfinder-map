# Handoff: build `wayfinder-view` (our own Wayfinder map viewer for Herdr + Zed)

You are starting in a new, (near-)empty repo the user created for this. Your job: **run the detailed investigation for building our own Wayfinder map viewer**, then turn it into a plan/prototype. A previous session (in `banked/ask-barry`) surveyed ~30 existing tools and chose this option. Do not re-run the landscape survey; do verify the open questions below.

## Goal

One small, Banked-owned tool that reads a Wayfinder map from GitHub Issues and shows it:

1. **In Herdr**: a split pane (via a *linked* local Herdr plugin) showing the map as a live tree/graph (done / claimed / frontier / blocked), optionally a sidebar token or tab-bar count and a toast when the frontier changes.
2. **In Zed**: the same tool writes a `MAP.md` containing a Mermaid graph; Zed's built-in Markdown preview renders it beside code. A global Zed task re-runs the generator on a loop.

## User constraints and preferences

- Security-conscious (fintech). No installing random third-party software; anything reused must be audited, vendored, or forked under our control. Prefer zero or tiny, auditable dependencies.
- Use the user's existing `gh` auth. Don't store tokens.
- Read-only against GitHub. Never edit map/ticket bodies. (A community tool that writes Mermaid into the map body risks lost updates and the body size cap.)
- Precedent for ownership: the user already runs a Banked-owned Herdr plugin, `banked/worktreeinclude-herdr-plugin` (installed at `~/.config/herdr/plugins/github/banked.worktreeinclude-*/herdr-plugin.toml`). Read it as a working manifest example.
- Environment: macOS (Apple silicon), Herdr 0.9.5 via mise, Zed stable (v1.27.0 was latest on 2026-11-04). BSD userland, so no GNU `sed` assumptions.

## The data model (Wayfinder on GitHub)

- Skill: https://github.com/mattpocock/skills/blob/main/skills/engineering/wayfinder/SKILL.md
- How maps are stored on GitHub is defined in `banked/ask-barry` → `docs/agents/issue-tracker.md` ("Wayfinding operations"). In short:
  - Map = issue labelled `wayfinder:map`. Body sections: Destination, Notes, Decisions so far, Not yet specified, Out of scope.
  - Tickets = GitHub **sub-issues** of the map, labelled `wayfinder:research|prototype|grilling|task`.
  - Claimed = has an assignee.
  - Edges = GitHub **native issue dependencies** (`blocked_by`).
  - **Frontier** = open, unassigned tickets with no open blockers.
  - Tickets are referred to by title, not number.
- Live test maps in `banked/ask-barry`: **#214** (8 tickets with a real dependency chain, good fixture), #175, #152.

### Verified: one GraphQL call gets the whole map

Ran successfully on #214 with plain `gh` auth:

```sh
gh api graphql -f query='
query { repository(owner:"banked", name:"ask-barry") { issue(number:214) { title subIssuesSummary { total completed }
  subIssues(first:50) { nodes { number title state labels(first:5){nodes{name}} assignees(first:3){nodes{login}}
    blockedBy(first:10){ nodes { number state } } } } } } }'
```

Also available, but not yet tried by us: `issueDependenciesSummary { blockedBy totalBlockedBy }`, where `blockedBy` counts only *open* blockers. GitHub limits: 100 sub-issues per parent, 50 dependencies per relationship type.

### Verified: GitHub can compute the frontier server-side

```
repo:banked/ask-barry parent-issue:banked/ask-barry#214 is:open no:assignee -is:blocked
```

It returned exactly #215 and #216, which is correct. Two caveats:

- It only works on the **advanced** search backend (`search(type: ISSUE_ADVANCED)` in GraphQL, or the web UI). The legacy `type: ISSUE` silently ignores `-is:blocked`.
- The search API is rate-limited to 30 requests/min. Prefer computing the frontier locally from the single query above.

## Herdr facts (from herdr v0.9.5 docs/source: github.com/herdrdev/herdr, docs `plugins.mdx`, `socket-api.mdx`)

- Manifest `herdr-plugin.toml` supports `[[build]]`, `[[startup]]`, `[[actions]]`, `[[events]]`, `[[panes]]` and `[[link_handlers]]`. Commands are argv arrays (no shell).
- **`[[panes]]`** entries open a plugin-owned terminal pane. Placement can be `overlay` (default), `split`, `tab`, `zoomed`, or `popup` (popup is in the docs and socket API but not in the 0.9.5 CLI help).
  - Open one with: `herdr plugin pane open --plugin <id> --entrypoint <id> --placement split --direction right --no-focus`.
- `herdr plugin link <path>` registers a local directory and runs no build step. **Use this, not `plugin install`.**
- No sandbox: plugins run as the user with the user's environment.
- Plugin commands get `HERDR_BIN_PATH`, `HERDR_SOCKET_PATH`, `HERDR_PLUGIN_ID`, `HERDR_PLUGIN_ROOT`, `HERDR_PLUGIN_CONFIG_DIR`, `HERDR_PLUGIN_STATE_DIR`, `HERDR_PLUGIN_CONTEXT_JSON`, plus workspace, tab and pane IDs.
- **No timer events**, so the pane process must poll on its own. Event hooks include `workspace.*`, `worktree.*`, `tab.*` and `pane.*`.
- **Sidebar tokens:** `herdr workspace report-metadata <ws> --source wayfinder --token wf="3 ready" --ttl-ms 120000`, then reference `$wf` in `[ui.sidebar.*]` config.
- **Tab bar:** `ui.tab_bar_right` with `{type="command", command=..., interval_seconds=...}`.
- **Toasts:** `herdr notification show <title> --body ...`.
- **Images:** Herdr renders the Kitty graphics protocol (no Sixel). There is an open bug where images don't show when the outer terminal is iTerm2 (#3941). Ask the user which outer terminal they use before relying on images.
- Draft manifest from the previous session (unverified against the schema; check with `herdr --help` / docs):

  ```toml
  id = "banked.wayfinder-view"
  name = "Wayfinder view"
  version = "0.1.0"
  min_herdr_version = "0.9.1"
  platforms = ["macos", "linux"]
  [[panes]]
  id = "map"
  title = "Wayfinder"
  placement = "split"
  command = ["bash", "wayfinder-watch.sh"]
  [[actions]]
  id = "open"
  title = "Open Wayfinder map"
  contexts = ["workspace"]
  command = ["sh", "-c", "exec \"$HERDR_BIN_PATH\" plugin pane open --plugin banked.wayfinder-view --entrypoint map --placement split --direction right --no-focus"]
  ```

## Zed facts

- **No extension API for panels or webviews.** The RFC (zed-industries/zed discussions #53403) is still a draft with no timeline. Don't build a Zed extension.
- **The Markdown preview renders Mermaid natively** (PR #49064, Feb 2026; renderer crate `mermaid_render` built on `merman`). Open it with `markdown::OpenPreviewToTheSide` (cmd-k v). The Zed CLI has no flag to open a preview.
- **Global tasks** live in `~/.config/zed/tasks.json` (project `.zed/tasks.json` needs worktree trust).
  - A loop such as `while :; do gen; sleep 30; done` can run in the terminal panel.
  - `"reveal_target": "center"` puts a TUI in the editor area.
  - The only task hook is `create_worktree`, so there are no file-save or timer triggers.
- The Agent panel also renders Mermaid, which could matter later for an MCP angle; that is out of scope for v1.

## Reference code (licence matters)

| Repo | Licence | Use |
|---|---|---|
| github.com/johnLitoBardinas/wayfinder-map (`render.py`, ~118 LOC stdlib Python) | MIT | Best seed: tree layout with ✅ closed / 🔵 claimed / 🟢 frontier / ⛔ blocked markers. Makes one REST call per ticket; replace with the single GraphQL query. Audit before copying. |
| github.com/fasouto/termaid | MIT, pure Python, zero deps | Optional: Mermaid → Unicode in the terminal, so one Mermaid string serves both Herdr and Zed. |
| github.com/blooop/wayfinder (`wf`) | MIT | Ideas only: "leverage view" sorts tickets by how much they unblock; PR badges. Do not adopt (agent launcher, ~32k LOC). |
| github.com/CircuitBoardGames/wayfinder-starmap | MIT | Ideas for a visual star-map and lint checks (cycles, unindexed tickets). |
| github.com/rengwu/wayfinder-maps | MIT | Ideas only. Its `scripts/wayfinder-pull` has a macOS path-traversal → `rm -rf` bug (GNU-only `sed \+` in `slugify`) and a pagination bug. Its viewer has an attribute-injection XSS. Don't reuse as-is. |
| rengwu/chartr | GPL-3.0 | Do not copy code. Rejected (separate desktop app, own Herdr sidecar, reads local markdown only). |
| zac-dougo/copse, kareblak/ticketmaster | **No licence** | Read-only inspiration; legally can't fork. |

Avoid: `Yellowdeliciousgrandcanyon551/wayfinder-maps` (looks like a malware lure).

## Open questions to investigate first

1. Does Zed's Markdown preview refresh live when `MAP.md` is rewritten on disk? Source suggests yes on `FileHandleChanged`, but this is untested.
2. Does Zed's `merman` renderer support `classDef` / `style` colours and `click` links? Test a sample graph.
3. Which outer terminal does the user run Herdr in? That decides whether Kitty-graphics images are viable or whether to stay text/Unicode.
4. Implementation language: stdlib Python (matches `render.py` and `termaid`), Go (a `gh` extension via go-gh, single binary), or TS/tsx (matches the user's other repos). Grill the user on this.
5. Where `MAP.md` lives: inside the target repo (gitignored?) or a scratch dir opened with `zed -a`. Also whether to support several maps or repos at once.
6. Confirm the exact Herdr manifest schema for `[[panes]]` (field names, `command` vs `argv`) against the 0.9.5 docs, and test `herdr plugin link`.
7. Refresh strategy: poll interval vs GitHub rate limits (GraphQL points); cache; only redraw on change.
8. Prompt-injection hygiene: issue titles and bodies are untrusted. Only render titles, and escape them for Mermaid (quotes, brackets, `]`, `|`) and for the terminal (strip control chars and ANSI escapes).

## Suggested first steps

1. Read this doc, then confirm scope with the user (grill on Q3–Q5).
2. Spike: run the GraphQL query against `banked/ask-barry#214` and save the JSON as a test fixture.
3. Build the pure core: JSON → model → frontier/claimed/blocked classification → (a) terminal tree, (b) Mermaid. Test it against the fixture.
4. Wire up Herdr (linked plugin + split pane loop), then Zed (global task + `MAP.md` + preview). Verify Q1, Q2 and Q6 by hand.
5. Write a short README with a security notes section (read-only, uses `gh` auth, no network beyond api.github.com, no deps / pinned deps).

## Suggested skills

- `mattpocock-skills:grilling`: settle the language, scope, multi-map and output-location decisions with the user before building.
- `mattpocock-skills:research`: verify Zed preview live refresh, `merman` Mermaid feature support, and the Herdr `[[panes]]` schema against primary sources, recorded as a markdown file in the repo.
- `mattpocock-skills:prototype`: throwaway spike of the Mermaid output in Zed preview and the tree in a Herdr split, to judge what the view should look like.
- `mattpocock-skills:tdd`: build the JSON → classification → render core test-first against the #214 fixture.
- `mattpocock-skills:codebase-design`: keep a clean seam between fetch (gh), model (frontier logic) and renderers (terminal / Mermaid).
- `mattpocock-skills:writing-for-agents`: when writing the new repo's CLAUDE.md / AGENTS.md.
- `/wayfinder` (user-invoked): if the user wants this effort itself charted as a Wayfinder map, which would also give a live fixture.
- `appsec-scan-nodejs`: only if the implementation ends up in Node/TS (it lives in the ask-barry repo's skills, so it may not be available in the new repo).
