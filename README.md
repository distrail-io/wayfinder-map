# wayfinder-map

A read-only terminal viewer that draws a Wayfinder map from GitHub Issues as a graph of its tickets and their blockers. It runs in Ghostty, or in a Herdr pane, and draws with Kitty graphics.

```sh
wayfinder-map [MAP] [-R owner/repo] [--interval 30s]
```

MAP is `123`, `#123` or `owner/repo#123`. Without a MAP, the repo's most recently updated open map opens.

Each ticket is a card: its fill is its state (grey done, blue claimed, green frontier, red blocked) and its icon is its type. Edges run from a blocker to what it blocks, and fade to dashes once the blocker is closed. Below the graph are the selected ticket's details and the map's warnings: a closed map, more tickets or blockers than one query fetches, blockers in another repo (shown as `owner/repo#N`), tickets without a `wayfinder:*` type (shown as `?`) and cycles.

| Key | |
|---|---|
| `←↑↓→` | select the nearest ticket that way |
| `tab` / `shift+tab` | select the next or previous ticket |
| `o` / `enter` | open the selected ticket in the browser |
| `r` | refresh now |
| `q` | quit |

The map reloads every `--interval` (30s by default, 10s at least) and is redrawn only when it changes.

The repo comes from the first of these that is set: the `owner/repo` in MAP, `-R`, `GH_REPO`, the Herdr pane's checkout, then the current directory.

## Install

```sh
./install.sh            # the latest release
./install.sh v0.1.0     # a given release
```

`install.sh` downloads the binary for your platform with `gh`, checks its SHA-256 against the release's `SHA256SUMS`, and writes it to `bin/wayfinder-map` in this checkout. It writes nothing else. It prints the `ln -s` command to put the binary on your PATH.

To build from source instead: `go build -o bin/wayfinder-map ./cmd/wayfinder-map`.

## Herdr

```sh
herdr plugin link /path/to/this/checkout
```

The "Open Wayfinder map" action opens a right split showing the newest open map of the focused pane's repo. To bind it to a key, add this to your Herdr config:

```toml
[[keys.command]]
key = "prefix+m"
type = "plugin_action"
command = "distrail-io.wayfinder-map.open"
```

## Security

- **Read-only:** it runs two fixed GraphQL queries through your existing `gh` login. It stores no tokens and never edits issues. The only other program it starts is your browser opener (`open` on macOS, `xdg-open` on Linux), with a `https://github.com/<owner>/<repo>/issues/<n>` URL, when you press `o`.
- **Untrusted titles:** control characters (including the ESC that starts every escape sequence), format characters (bidi overrides, zero-width and invisible tag characters), line separators and invalid UTF-8 are removed before display. GraphQL errors are shown, sanitised, rather than read as an empty map.
- **Dependencies** are vendored, checked with `govulncheck` in CI, and kept current by Dependabot:
  - Bubble Tea v2 (MIT): the terminal app.
  - `golang.org/x/image`, `x/sys` and `x/text` (BSD-3-Clause, Go team): drawing, the Go fonts and terminal size.
- **Icons** are [Lucide](https://lucide.dev) icons (ISC; `search` is from Feather, MIT), hand-converted to drawing code in `internal/draw/icons.go`, which carries both notices.
- **Releases** are built by `release.yml` from a release-please tag, with `CGO_ENABLED=0 -trimpath`, and carry `SHA256SUMS`. These catch a corrupted download but not a tampered release; build provenance attestations are paused until [#14](https://github.com/distrail-io/wayfinder-map/issues/14).

## Development

```sh
go test -race ./...    # no test calls gh or the network
go test ./internal/draw -update    # rewrite the golden images after a deliberate change
```

`fixtures/map-edge-cases.json` is `map-214.json` edited to hold every edge case the viewer flags; `go run ./fixtures/edgecases` regenerates it.

Commits to `main` use conventional PR titles (`feat:`, `fix:`) with squash merges; release-please turns them into release PRs. The repo needs "Allow GitHub Actions to create and approve pull requests" turned on.
