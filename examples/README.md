# Examples

Copy-paste configurations for the status bars that `spmeta` is built for. Each example assumes `spmeta` is on your `PATH`; the flags are described in [docs/design/cli.md](../docs/design/cli.md).

| Directory | Tool | Mode | Files |
| --- | --- | --- | --- |
| [waybar](waybar/) | Waybar (Linux) | `--watch --format waybar`, updates on each change | [config.jsonc](waybar/config.jsonc), [style.css](waybar/style.css) |
| [sketchybar](sketchybar/) | SketchyBar (macOS) | Polls from a plugin script every 5 seconds | [sketchybarrc](sketchybar/sketchybarrc), [plugins/spotify.sh](sketchybar/plugins/spotify.sh) |
| [tmux](tmux/) | tmux | Polls from the status line every 5 seconds | [spmeta.tmux.conf](tmux/spmeta.tmux.conf) |

Each file starts with a comment that says where to put it.

On macOS, the first read from a new host app needs the Automation permission described in the [README](../README.md#macos). The permission is per app: allowing your terminal does not cover the status bar. Let the bar run its plugin once and allow the bar app (for example SketchyBar) when macOS asks.
