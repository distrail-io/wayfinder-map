# wayfinder-map

A read-only terminal viewer that draws a Wayfinder map from GitHub Issues as a graph of its tickets and their blockers. It runs in Ghostty, or in a Herdr pane, and draws with Kitty graphics. In a terminal without Kitty graphics, it lists the tickets as text instead.

```sh
wayfinder-map [MAP] [-R owner/repo] [--look cards|compact] [--dir auto|lr|tb] [--interval 30s] [--list]
```

MAP is `123`, `#123`, `owner/repo#123` or an issue URL. Without a MAP, a picker lists the repo's open maps, most recently updated first. Each row shows how many of the map's tickets are done, and how many are ready, claimed and blocked. `enter` opens the map under the cursor. The picker lists 50 maps; if there are more, open one directly with `wayfinder-map <number>`.

Each ticket is a card with its title, or in the compact look a circle with its number. Its fill is its state (grey done, blue claimed, green frontier, red blocked) and its icon is its type. Edges run from a blocker to what it blocks, and fade to dashes once the blocker is closed. Below the graph are the selected ticket's details and the map's warnings: a closed map, more tickets or blockers than one query fetches, blockers in another repo (shown as `owner/repo#N`), tickets without a `wayfinder:*` type (shown as `?`) and cycles.

| Key | |
|---|---|
| `←↑↓→` | select the nearest ticket that way |
| `tab` / `shift+tab` | select the next or previous ticket |
| `o` / `enter` | open the selected ticket in the browser |
| `c` | switch between the card and compact looks |
| `d` | switch the graph between left to right and top to bottom |
| `b` / `esc` | go back to the picker |
| `r` | refresh now |
| `q` | quit |

The map, or the picker, reloads every `--interval` (30s by default, 10s at least). The map is redrawn only when it changes.

`--look` sets the look the map opens in. `--dir` sets the graph's direction: left to right, top to bottom, or `auto` (the default). `auto` lays the map out both ways and draws the one that comes out larger in the pane, and picks again whenever the pane is resized, until you press `d`. On a resize, the graph is laid out and placed again to fit the new size.

Each flag has an environment variable: `WAYFINDER_MAP_LOOK`, `WAYFINDER_MAP_DIR`, `WAYFINDER_MAP_INTERVAL` and `WAYFINDER_MAP_LIST` (`1` or `0`). A flag beats its variable, which beats the default. An invalid value stops it at startup with an error that names the variable.

The repo comes from the first of these that is set: the `owner/repo` in MAP, `-R`, `GH_REPO`, the Herdr pane's checkout, then the current directory.

## Terminals without Kitty graphics

Before it starts, wayfinder-map asks the terminal whether it shows Kitty graphics. If the terminal doesn't answer yes within a second, as in Terminal.app, or in tmux without passthrough, it shows the list instead of the graph. The list has the tickets grouped as frontier, claimed, blocked, then done, with the same details area below. `↑↓`, `j`/`k` or `tab`/`shift+tab` move the selection, and `c` and `d` do nothing.

`--list` always shows the list. `--list=false` always draws the graph, without asking. Use that if the terminal shows Kitty graphics but doesn't answer the query. Herdr inside iTerm2 shows no images ([herdr #3941](https://github.com/herdrdev/herdr/issues/3941)) but may still answer yes, so use `--list` there.

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

The "Open Wayfinder map" action opens a right split with the picker for the focused pane's repo. To bind it to a key, add this to your Herdr config:

```toml
[[keys.command]]
key = "prefix+m"
type = "plugin_action"
command = "distrail-io.wayfinder-map.open"
```

## Security

- **Read-only:** it runs two fixed GraphQL queries through your existing `gh` login. It stores no tokens and never edits issues. The only other program it starts is your browser opener (`open` on macOS, `xdg-open` on Linux), with a `https://github.com/<owner>/<repo>/issues/<n>` URL, when you press `o`.
- **Network:** it opens no connections itself. All its traffic is `gh`'s, to `api.github.com` (or your `GH_HOST`), and `install.sh` downloads only through `gh release download`.
- **Terminal:** it writes the map image and one capability query to the terminal it runs in, and reads only the terminal's reply to that query. The image is a PNG it draws itself; nothing from GitHub reaches the terminal except sanitised text.
- **Untrusted titles:** control characters (including the ESC that starts every escape sequence), format characters (bidi overrides, zero-width and invisible tag characters), line separators and invalid UTF-8 are removed before display. GraphQL errors are shown, sanitised, rather than read as an empty map.
- **Dependencies** are vendored, so a build fetches nothing. They are checked with `govulncheck` in CI and kept current by Dependabot. Three are direct, and the rest come with them:
  - Bubble Tea v2 (`charm.land/bubbletea/v2`, MIT): the terminal app's event loop, input and screen.
  - `golang.org/x/image` (BSD-3-Clause, Go team): rasterising the graph, and the Go fonts.
  - `golang.org/x/sys` (BSD-3-Clause, Go team): the terminal's pixel size and raw mode for the capability query.
  - Charm's `ultraviolet`, `colorprofile`, `x/ansi`, `x/term`, `x/termios` and `x/windows` (MIT): Bubble Tea's renderer, input decoding, colour detection and terminal modes.
  - `clipperhouse/displaywidth`, `clipperhouse/uax29`, `mattn/go-runewidth` and `rivo/uniseg` (MIT): how many cells a string takes, for Bubble Tea's layout.
  - `lucasb-eyer/go-colorful` (MIT): colour conversion when Bubble Tea downsamples colours.
  - `muesli/cancelreader` (MIT): stopping Bubble Tea's input reader cleanly on quit.
  - `xo/terminfo` (MIT): Bubble Tea's terminal capability lookup.
  - `golang.org/x/sync` (BSD-3-Clause, Go team): Bubble Tea's goroutine groups.
  - `golang.org/x/text` (BSD-3-Clause, Go team): legacy character maps in the font reader, from `x/image`.
- **Icons** are [Lucide](https://lucide.dev) icons (ISC; `search` is from Feather, MIT), hand-converted to drawing code in `internal/draw/icons.go`, which carries both notices.
- **Releases** are built by `release.yml` from a release-please tag, with `CGO_ENABLED=0 -trimpath`, and carry `SHA256SUMS`. These catch a corrupted download but not a tampered release; build provenance attestations are paused until [#14](https://github.com/distrail-io/wayfinder-map/issues/14).

## Development

```sh
go test -race ./...    # no test calls gh or the network
go test ./internal/draw -update    # rewrite the golden images after a deliberate change
go test -tags contract -run TestContract -count=1 ./internal/github    # the live API check
```

The contract check (`contract.yml`, weekly and on dispatch) runs the two queries against this repo's own map with `GITHUB_TOKEN`. It fails on GraphQL errors, on a result that doesn't parse, and on any field the queries use that GitHub has deprecated.

`fixtures/map-edge-cases.json` is `map-214.json` edited to hold every edge case the viewer flags; `go run ./fixtures/edgecases` regenerates it.

Commits to `main` use conventional PR titles (`feat:`, `fix:`) with squash merges; release-please turns them into release PRs. The repo needs "Allow GitHub Actions to create and approve pull requests" turned on.
