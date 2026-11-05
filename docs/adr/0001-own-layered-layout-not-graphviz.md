# Our own layered layout, not Graphviz

We lay out the map graph with our own ~400-line Sugiyama layout (`internal/layout`) instead of embedding go-graphviz or calling `dot`. go-graphviz compiles a 1.5 MB WASM blob at `init()`, which takes ~400 ms on every start without its unsafe shared temp-dir cache. It also adds ~6.6 MB, EPL/expat notices, and probably a macOS JIT entitlement. Our maps are small DAGs (at most 100 tickets), our layout runs in ~1–28 ms, and it was preferred by eye.

## Consequences

On dense maps our layout is looser than `dot`. Improve the layering heuristic rather than reaching for Graphviz again. Evidence: [How do we get the dot-style layout: embedded go-graphviz or our own layered layout?](https://github.com/distrail-io/wayfinder-map/issues/6).
