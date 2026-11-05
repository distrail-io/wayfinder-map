# The map is a Kitty-graphics image, with a text list fallback

The map view is a raster image (drawn with `golang.org/x/image`) placed with the Kitty graphics protocol through Bubble Tea's `tea.Raw`. It is not a text/Unicode graph. Text-only output was rejected as not visual enough, and images were verified to render in a Herdr pane inside Ghostty. Terminals that fail the Kitty capability probe, or runs with `--list`, get a plain status-grouped list instead. There is no Sixel or Unicode-art graph.

## Consequences

Image output must wait for Bubble Tea's frame (~60 ms) and track cell pixel size via `TIOCGWINSZ`. Herdr inside iTerm2 does not show images (herdr #3941), so those users get the list.
