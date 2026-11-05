# Research: Are the candidate Go dependencies safe to vendor?

Sources: module source from the Go module cache (fetched via proxy.golang.org at the versions below), LICENSE files, go.mod files, the OSV API (`api.osv.dev`), the Go vulnerability DB index (`vuln.go.dev/index/modules.json`), NVD, and GitHub repo data via `gh api`. Read 2026-11-05.

Method: one scratch module per candidate in an empty temp dir, with `go get`, `go mod graph`, `go list -m all` and `go list -deps`. A minimal `main.go` per candidate was **compiled only, never run**, with `GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath` (go1.27.3, darwin/arm64, plus a linux/amd64 cross-compile). `govulncheck` was not installed. OSV was queried for every compiled module at its resolved version.

Nothing in this note was confirmed by running the code. Init cost, JSON output shape and macOS signing behaviour are still for the prototype ticket ("What should the map look like in the terminal, and which layout and drawing stack produces it?") to check by hand.

## Verdicts

| Candidate | Version audited | Verdict |
|---|---|---|
| Bubble Tea v2 + bubbles v2 + lipgloss v2 | `charm.land/bubbletea/v2` v2.1.1, `charm.land/bubbles/v2` v2.2.1, `charm.land/lipgloss/v2` v2.0.6 | **Accept** |
| `github.com/goccy/go-graphviz` | v0.2.10 (Graphviz 12.1.2 in WASM) | **Accept with conditions** (the weakest accepted dependency; keep an exit plan) |
| `github.com/tdewolff/canvas` | `v0.0.0-20260923214215-09804640d00c` (no tags exist) | **Reject** |
| `github.com/fogleman/gg` | v1.3.0 | **Accept with conditions** |
| `golang.org/x/image/font/gofont` | x/image v0.47.0 | **Accept** |
| Lucide icons | lucide 1.69.0 (repo, not a Go module) | **Accept with conditions** (licence notice; copy only the paths needed) |

## Key numbers

The "graph" column counts modules from `go list -m all`, excluding the main module. The "compiled" column counts modules that own a package in `go list -deps`, which is what `go mod vendor` would copy. Size deltas are measured against an empty `main` (darwin/arm64: 2.40 MB, or 1.57 MB stripped). MB here means 10^6 bytes.

| Candidate | Graph | Compiled | cgo | Size added (unstripped / `-s -w`) | linux/amd64 size added | OSV at resolved versions |
|---|---|---|---|---|---|---|
| Charm v2 trio | 31 | 20 | no | +3.36 / +2.49 MB | +3.78 MB | none |
| go-graphviz | 13 | 8 | no | +8.72 / +6.45 MB | +9.10 MB | imaging GHSA-q7pp-wcgr-pffx (no fix); x/image v0.21.0: 9 Go advisories; x/text v0.19.0: 2 Go advisories |
| canvas | 104 | 25 | no (cgo formats are behind the `formats` build tag) | +9.98 / +7.15 MB | +10.55 MB | x/net v0.58.0: 7 (2 hit `x/net/html`, which canvas imports) |
| gg (+ truetype + goregular) | 5 | 3 | no | +1.18 / +0.83 MB | +1.25 MB | none |
| gofont (3 faces) | 3 | 1 | no | +0.50 / +0.48 MB | n/a | none at v0.47.0 |
| **Proposed stack** (Charm + go-graphviz + gg + 2 Go fonts, with x/image v0.47.0 and x/text v0.43.0 pinned) | 41 | 28 | no | **+11.38 / +8.52 MB** | n/a | imaging GHSA only (unreachable, see below) |

All candidates compile with `CGO_ENABLED=0` and cross-compile to linux/amd64. `CGO_ENABLED=1 go list -deps -f '{{.CgoFiles}}'` finds no cgo files in any candidate.

## Bubble Tea v2, bubbles v2, lipgloss v2: accept

- **Modules:** 31 in the graph and 20 compiled.
  - Charm: `bubbletea`, `bubbles`, `lipgloss`, `colorprofile`, `ultraviolet`, `x/ansi`, `x/term`, `x/termios`, `x/windows`.
  - Third-party: `atotto/clipboard`, `clipperhouse/displaywidth`, `clipperhouse/uax29/v2`, `lucasb-eyer/go-colorful`, `mattn/go-runewidth`, `muesli/cancelreader`, `rivo/uniseg`, `sahilm/fuzzy` (via `bubbles/list`) and `xo/terminfo`.
  - Go: `golang.org/x/sync` and `golang.org/x/sys`.
- **Licences:** all MIT, except `atotto/clipboard`, which is BSD-3-Clause.
- **Notable:** `charmbracelet/ultraviolet` has no tags. Bubble Tea v2.1.1 pins `v0.0.0-20261008173134-6b8d4baf91b4`, which is fine once go.sum and vendoring fix it.
- **Maintenance:**
  - All repos are owned by the Charmbracelet organisation (a company), and none is archived.
  - Bubble Tea: release v2.1.1 on 2026-10-28; 166 commits in the last 12 months; 45.6k stars.
  - bubbles: release v2.2.1 on 2026-08-24; 59 commits in 12 months.
  - lipgloss: release v2.0.6 on 2026-08-11; 69 commits in 12 months.
  - Bus factor: concentrated but more than one person. In bubbletea's last 100 commits, `aymanbagabas` wrote 43, `meowgorithm` 13, dependabot 10 and `andrinoff` 9.
  - The smaller transitive repos are all active in 2026, except `muesli/cancelreader` (last commit 2023-03) and `rivo/uniseg` (last commit 2024-04). Both are small, stable libraries.
- **Advisories:** OSV has no entries for any of the 20 compiled modules at these versions. It has none for the v1 `github.com/charmbracelet/{bubbletea,bubbles,lipgloss}` paths either. The Go vulnerability DB lists only other Charm products (wish, soft-serve, charm).
- **Init:** only table setup.
  - `go-runewidth` reads its environment variables.
  - `x/ansi` and `ultraviolet` build lookup maps.
  - On Linux/BSD only, `atotto/clipboard` probes `PATH` for `wl-copy`, `xclip` and `xsel` using `exec.LookPath` (`clipboard_unix.go` L51). It does not execute them.
- **Process execution:** none at init.
  - At `Program.Run`, `colorprofile.Detect` (`tea.go` L1115) runs `tmux info` if `$TMUX` is set and the environment does not already declare truecolor (`colorprofile/env.go` L239-262). Ghostty sets `COLORTERM=truecolor`, which skips this.
  - `tea.ExecProcess` and clipboard copy/paste execute other programs only when we call them.
- **Network:** none. The only network-adjacent import is `net/url` in `x/ansi/cwd.go`, which parses OSC 7 cwd URLs.
- **Small condition:** don't import `charm.land/lipgloss/v2/compat`. It runs colour detection at package init (`compat/color.go` L16).

## go-graphviz: accept with conditions

### What it is

- **Layout:** `internal/wasm/graphviz.wasm` holds Graphviz **12.1.2** (`graphviz.version`) plus **expat 2.6.3**, built with wasi-sdk 24, `-Os` and `wasm-opt` (`internal/wasm/build/Dockerfile`, `Makefile`).
  - The blob is **1,507,296 bytes (1.51 MB)**, embedded with `//go:embed` (`bind.go` L23).
  - It runs on `tetratelabs/wazero` v1.10.1 (Apache-2.0, pure Go). The GitHub repo now sits under the `wazero` organisation, but the module path is unchanged.
- **Compiled modules (8):**
  - `goccy/go-graphviz` and `tetratelabs/wazero`
  - `fogleman/gg`, `golang/freetype`, `disintegration/imaging` and `flopp/go-findfont`, all used by the Go PNG/JPEG renderer in `gvc/image_renderer.go`
  - `golang.org/x/image` and `golang.org/x/text`
  - The `gvc` package imports the renderer unconditionally, so these come in even if we only ask for JSON.
- **Upstream replace has no effect:** go-graphviz's own go.mod has `replace flopp/go-findfont => goccy/go-findfont`. A replace directive only applies in the main module, so consumers get upstream `flopp/go-findfont` v0.1.0.

### Layout coordinates without rendering

Yes, as far as the source shows (not run).

- `graphviz.Format` is a plain `string` type (`graphviz.go`).
- `Render(ctx, g, graphviz.Format("json"), w)` calls `Layout` and then `RenderData`, which returns the bytes the WASM renderer produced (`gvc/gvc.go` L61-73).
- The WASM links Graphviz's `plugin/core` (`patch.c` L13-20 preloads `gvplugin_core_LTX_library` along with the dot and neato layout plugins).
- The blob contains the core renderer tables: `gvrender_json_types`, `gvdevice_json_types`, `render_features_json`, `xdot_json:json`, `xdot:xdot`, `xdot1.2:xdot`, `xdot1.4:xdot` and `gvrender_dot_types`.
- So `json`, `json0`, `dot_json`, `xdot_json`, `dot`, `xdot`, `xdot1.4` and `plain` are available. The Go `gg` renderer is used only for `png` and `jpg`.
- Watch out: the exported constant `graphviz.XDOT` is the string `"dot"`, not `"xdot"`. Pass `Format("xdot")` or `Format("json")` explicitly.

### Licences

- **The go-graphviz wrapper:** MIT. The module ships **only** the MIT LICENSE.
  - Its README L148 says the WASM follows Graphviz's licence.
  - Graphviz 12.1.2's `LICENSE` is **EPL-1.0** (gitlab.com/graphviz/graphviz, tag 12.1.2).
  - expat is MIT. Neither licence text is in the module, so we must add both ourselves.
- **wazero:** Apache-2.0, with a 2-line NOTICE ("wazero / Copyright 2020-2023 wazero authors") that must be reproduced.
- **gg, imaging, findfont:** MIT.
- **freetype:** FTL or GPLv2+ (see the gg section).
- **What EPL-1.0 means for a WASM blob embedded in a binary distributed inside a private org** (not legal advice):
  - The trigger is *distribution*. The EPL FAQ says that if you modify an EPL program "but never distribute it to anyone else", you need not make modifications available. Handing the binary to staff of the same legal entity is internal use, not distribution to a third party.
  - Giving it to contractors, affiliates or other legal entities *is* distribution. In that case EPL-1.0 §3 lets us ship object code under our own licence if that licence:
    1. disclaims all warranties for every contributor,
    2. excludes liability for every contributor,
    3. states that any differing terms are ours alone, and
    4. **states that the source is available from us and how to get it**.
  - Per the EPL FAQ, we "do not need to include the source code ... but you do need to include a statement that the source code is available from you and information on how to obtain it."
  - The blob is a *modified* Graphviz: a partial file selection, plus `patch.c` and `bind.c`. The source to point at is the Graphviz 12.1.2 tarball plus go-graphviz's `internal/wasm/build/` at v0.2.10.
  - **Our own Go code is not affected.** Per the EPL FAQ, a separate module distributed alongside does not need to be EPL "as long as the module is not a derivative work of the Program". Talking to the WASM through its exported ABI keeps our code separate.
  - EPL is weak copyleft at file level, not "permissive". It passes the bar only with the notice conditions below.

### Maintenance and ownership

- **Owner:** a single individual (user `goccy`), not an organisation. 833 stars; not archived.
- **Activity:** last release v0.2.10 on 2025-11-29, nearly a year ago. 2 commits in the last 12 months; 127 in total. 20 open issues.
- **Fix latency:** the Go 1.24 "out of bounds memory access" bug (#120, opened 2025-07-29) was fixed by a wazero bump only in v0.2.10, four months later.
- **Security red flag:** issue #126 (2026-07-08) is from a Datadog security engineer. It says they found "what may be several security vulnerabilities" and asks for private reporting. It has **no reply after 4 months**. `GET /repos/goccy/go-graphviz/private-vulnerability-reporting` returns `{"enabled":false}`, and there is no SECURITY.md and no published advisory.
- **Other open bugs that matter to us:**
  - #117: `concurrent map writes` under concurrent use, so the library is not goroutine-safe.
  - #118: after one cancelled context, every later call fails with "module closed with context dead".
- **wazero itself is healthy:** release v1.12.0 on 2026-05-29, 56 commits in 12 months, several maintainers, and no OSV entries.

### Advisories

- go-graphviz and wazero have no OSV or Go vulnerability DB entries.
- **Graphviz C:** NVD lists no CVE against Graphviz core newer than CVE-2023-46045, which was fixed in 10.0.1, so 12.1.2 is unaffected.
- **Bundled expat 2.6.3 has later CVEs:**
  - CVE-2024-50602, fixed in 2.6.4
  - CVE-2024-8176 (recursion depth), fixed in 2.7.0
  - CVE-2025-59375 (memory amplification), fixed in 2.7.2
  - Graphviz uses expat only to parse HTML-like labels (`label=<...>`). These bugs run inside wazero's linear-memory sandbox, so the worst case is a guest trap or a memory blow-up, not host compromise.
- **x/image and x/text pinned by go-graphviz's go.mod:**
  - x/image v0.21.0 has 9 Go advisories (GO-2026-4815, -4961, -4962, -5031, -5032, -5061, -5062, -5066, -6222), affecting tiff, webp, bmp, sfnt and vp8l. All are fixed by v0.45.0.
  - x/text v0.19.0 has GO-2026-5970 (fixed in 0.39.0) and GO-2026-6629 (fixed in 0.41.0).
  - Minimal version selection lets our go.mod override both. The proposed-stack build at x/image v0.47.0 and x/text v0.43.0 is clean.
- **`disintegration/imaging` v1.6.2:**
  - GHSA-q7pp-wcgr-pffx / CVE-2023-36308 is a LOW-severity panic on a crafted TIFF. It has no fix, and the repo has had no commits since 2020-12.
  - It is **not in the Go vulnerability DB**, so `govulncheck` will not report it.
  - It is reachable only through `ImageRenderer.LoadImage` (`gvc/image_renderer.go` L374-398): an `image=` attribute on a node, rendered to PNG or JPEG. Our JSON-only use doesn't reach it.

### Init and I/O

This is heavy work at init.

- **`internal/wasm/bind.go` `init()` (L395-…)**
  - Creates a wazero runtime and **compiles the 1.5 MB WASM to native code** on every process start that imports the package. It runs even if no graph is ever laid out, and it panics on failure.
  - `cgraph/init.go` then calls into the module.
  - The time and memory cost were not measured, because running is out of bounds here. The prototype ticket should time cold and warm start.
- **Compilation cache in temp**
  - `getCompilationCache()` (L3007-3024) uses `$TMPDIR/go-graphviz`. It creates the directory `0755` if missing and **reuses it if it already exists, whoever owns it**.
  - wazero adds a `wazero-<ver>-<arch>-<os>` subdirectory (`0700` if wazero creates it; reused if present). Cache entries are checked only by CRC32 (`engine_cache.go` L164-250), which catches corruption, not tampering.
  - On a host where `TMPDIR` is a shared `/tmp` (typical Linux), another local user could pre-create the directory and plant native code that our process would then execute.
  - On macOS, `TMPDIR` is per-user (`/var/folders/...`), so the risk there is low.
- **Host filesystem access**
  - The WASM guest gets the host filesystem mounted at `/`. `WasmFileSystem.Open` falls back to `os.Open` when no sub-filesystem is set (`bind.go` L36-41).
  - DOT attributes such as `image`, `shapefile` and `fontpath`, or `<IMG>` in HTML labels, can make Graphviz read any file the user can read.
  - `graphviz.SetFileSystem(fs.FS)` is exported to restrict this.
- **Font discovery:** `flopp/go-findfont` walks system font directories only when the Go PNG renderer looks up a font (L190, L231). It doesn't run for JSON output.
- **Network:** none. The only `net` imports are wazero's WASI socket support. The module config adds no sockets, and Graphviz does no networking.
- **macOS signing:** wazero's compiler `mmap`s RW and then `mprotect`s to RX, with no `MAP_JIT`. Whether a hardened-runtime, notarised macOS build needs a JIT or unsigned-executable-memory entitlement is **unverified**. Take this to "How is v1 structured, and how is it built and shipped?".

### Conditions

1. **Notices:** ship the EPL-1.0 text, the expat MIT notice, the wazero NOTICE and an "EPL source is available at …" statement in README/NOTICE. This is cheap, and it covers the day the binary leaves the legal entity.
2. **Sandbox the guest:** call `graphviz.SetFileSystem(fstest.MapFS{})` (or an empty `fs.FS`) before any Graphviz call. Generate the DOT ourselves, with every label a quoted and escaped plain string: no HTML labels, and no `image`, `shapefile`, `fontpath` or `imagepath` attributes.
3. **Kill or relocate the temp cache:** use an internal fork, wired in with a `replace` directive. In the fork, delete `getCompilationCache`, or point it at `os.UserCacheDir()/wayfinder-map`, created `0700` with an owner check. The fork is about 20 lines of diff, and it is also where to raise wazero to v1.12.0 and rebuild with a newer expat if we want.
4. **Serialise all Graphviz calls** through one goroutine, and **never pass a cancellable context** into `Render` (#117, #118). Enforce timeouts outside the call.
5. **Pin `golang.org/x/image` ≥ v0.45.0 and `golang.org/x/text` ≥ v0.41.0** in our go.mod.
6. **Watch #126.** If the reported vulnerabilities land in the Go wrapper rather than the sandboxed C, re-audit. Keep the in-house layout below as the planned fallback.

## tdewolff/canvas: reject

- **No releases:** there are no tags, so it can only be consumed as a pseudo-version.
  - It is actively developed by one person: `tdewolff` wrote 60 of the last 79 commits in 12 months. 1.8k stars; not archived.
  - The root `canvas` package itself drags in the whole tree, so there is no lighter subset.
- **Tree:** 104 modules in the graph and 25 compiled, including:
  - `star-tex.org/x/tex`, a pure-Go TeX engine, which pulls in `codeberg.org/go-pdf/fpdf`
  - `github.com/yuin/goldmark`, a Markdown parser
  - `tdewolff/minify/v2`, `tdewolff/parse/v2` and `andybalholm/brotli`
  - `BurntSushi/xgb` and `xgbutil` (an X11 client) via `srwiley/scanx`
  - `go-text/typesetting`, `benoitkugler/textlayout` and `go-fonts/latin-modern` (embedded LM Math)
  - `golang.org/x/net`
  - The go.mod also lists fyne, gio, glfw, go-gl, go-webp, go-avif and gonum/plot, though those are not compiled without build tags.
- **Licences that fail the bar:**
  - `benoitkugler/textprocessing` is **LGPL-2.1**, imported by `canvas/text` through `fribidi`. Go links statically, so LGPL's relinking obligations bite.
  - `srwiley/scanx` has **no licence file at all**. Its `scan.go` says it is adapted from Freetype-Go, which is FTL or GPL.
  - `BurntSushi/xgbutil` is **WTFPL**.
  - `BurntSushi/freetype-go` is FTL or GPL.
  - `go-fonts/latin-modern` fonts are under the GUST Font License.
- **Advisories:** x/net v0.58.0 carries GO-2026-6860 and GO-2026-6866 (`x/net/html`, fixed in 0.62.0), plus 5 HTTP/2 advisories in a package canvas doesn't import. goldmark had GO-2026-5320 (XSS), fixed in 1.7.17; we would get 1.8.5.
- **Init and network:** only table setup and `x/image` TIFF registration. No network I/O. LaTeX `exec` sits behind the `latex` build tag.
- **cgo:** none by default. WebP and AVIF output via cgo needs the `formats` build tag.
- **Size:** +7.15 MB stripped.
- **Alternative for "SVG later":** write a small SVG emitter ourselves. Our shapes are rounded rects, cubic Béziers and text, which come to around a hundred lines of `encoding/xml` escaping, with no dependencies.

## fogleman/gg: accept with conditions

- **Modules:** 5 in the graph and 3 compiled: `fogleman/gg`, `golang/freetype` and `golang.org/x/image`.
- **No marginal cost on top of go-graphviz:** go-graphviz already compiles gg and freetype, so with go-graphviz accepted, gg adds no modules and almost no binary size.
- **Licences:**
  - gg is MIT.
  - `golang/freetype` is your choice of FTL or GPLv2+; choose FTL.
  - FTL §2 requires binary redistributions to "provide a disclaimer that states that the software is based in part of the work of the FreeType Team, in the distribution documentation" (`licenses/ftl.txt`).
- **Maintenance:**
  - gg: dormant. Last commit 2021-09-28, last release v1.3.0 on 2019-03-09, single owner, 81 open issues. Not archived.
  - `golang/freetype` is **archived**, with its last commit on 2017-06-09.
  - The risk is "frozen code we own", not active danger: we draw our own shapes and text from embedded fonts and decode no untrusted images.
- **Advisories:** none in OSV for gg or freetype.
- **Init and network:** none.
- **cgo:** none.
- **Size:** +0.83 MB stripped, including the goregular TTF.
- **Conditions:**
  - Add the FTL credit line to README/NOTICE.
  - Load fonts only from embedded `gofont` bytes (`truetype.Parse`), never `gg.LoadFontFace`, `LoadPNG` or `LoadImage` on paths.
  - Treat the vendored copy as ours to patch.
- **Cheaper first-party alternative** if we ever drop go-graphviz: `golang.org/x/image/vector` (path fill), `x/image/font/opentype`, `font.Drawer` and `image/draw`. It needs zero non-`golang.org` modules, but we would have to write a stroker for edges and outlines.

## Go fonts: accept

- **What:** `golang.org/x/image/font/gofont/*`, a first-party Go module. 40 commits in the last 12 months; release v0.47.0 on 2026-10-09.
- **Licence:** the fonts are by Bigelow & Holmes "under the same open source license as the rest of the Go project's software", a BSD-3-style text (`font/gofont/ttfs/README`). Binary redistribution must reproduce the copyright notice in the documentation.
- **Size:** each face is a `[]byte` of about 141-188 KB (e.g. Go-Regular 148,672 B; Go-Mono 173,248 B). Three faces added 0.48 MB stripped.
- **Init and network:** none.
- **Advisories:** none at v0.47.0. GO-2026-4962 (sfnt memory, fixed in 0.39.0) shows why we must pin ≥ v0.45.0.

## Lucide icons: accept with conditions

- **What:** `lucide-icons/lucide`, owned by an organisation. Release 1.69.0 on 2026-11-05; 810 commits in 12 months; 25.6k stars.
- **Licence:** the `LICENSE` file is **ISC**. Icons derived from Feather (listed in the file) are additionally **MIT** (© Cole Bemis). Both need the notice kept "in all copies".
- **Not a Go module.** gg can't parse SVG.
- **Conditions:**
  - Copy only the handful of icon path strings we use into our source.
  - Convert them by hand to gg path calls (stroked, width 2, round caps and joins).
  - Keep the ISC notice, plus the MIT notice if any copied icon is Feather-derived, in NOTICE.
- **Size:** negligible.

## Cheaper alternatives if go-graphviz is dropped

- **In-house layered (Sugiyama) layout:**
  - Our maps are small DAGs of tens of nodes. Longest-path layering, barycentre crossing reduction, simple x-placement and cubic-Bézier edges add no dependencies, no WASM, no EPL, no init cost and about 9 MB less binary.
  - The prototype ticket should judge whether the result looks good enough.
- **Run the system `dot -Tjson` if installed:** we embed nothing, so there is no EPL distribution question. But it adds a runtime dependency and process execution, which clashes with "vendored, self-contained".
- **`github.com/nikolaydubina/go-graph-layout`** (now `gverger/go-graph-layout`): MIT, but 98 stars and no commits since 2025-04. It fails "well-known".
- **gonum** (BSD-3, 8.5k stars) has force-directed layouts only, not layered DAG layout.
