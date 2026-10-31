package hostapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestFromProcesses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		snapshot string
		start    int
		want     string
	}{
		{
			"iTerm",
			`
  500     1 /Applications/iTerm.app/Contents/MacOS/iTerm2
  600   500 /usr/bin/login
  700   600 -zsh
`,
			700, "iTerm",
		},
		{
			"Terminal",
			`
  400     1 /System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal
  410   400 /usr/bin/login
  420   410 -zsh
`,
			420, "Terminal",
		},
		{
			"Ghostty",
			`
  300     1 /Applications/Ghostty.app/Contents/MacOS/ghostty
  310   300 /usr/bin/login
  320   310 /bin/zsh
`,
			320, "Ghostty",
		},
		{
			"Warp",
			`
  300     1 /Applications/Warp.app/Contents/MacOS/stable
  310   300 /bin/zsh
`,
			310, "Warp",
		},
		{
			"VS Code helper nests inside the main app",
			`
  300     1 /Applications/Visual Studio Code.app/Contents/MacOS/Electron
  310   300 /Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper.app/Contents/MacOS/Code Helper
  320   310 /bin/zsh
`,
			320, "Visual Studio Code",
		},
		{
			"VS Code helper alone still names the outermost app",
			`
  310     1 /Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (Plugin).app/Contents/MacOS/Code Helper (Plugin)
  320   310 /bin/zsh
`,
			320, "Visual Studio Code",
		},
		{
			"kitty",
			`
  300     1 /Applications/kitty.app/Contents/MacOS/kitty
  310   300 /bin/zsh
`,
			310, "kitty",
		},
		{
			"Alacritty",
			`
  300     1 /Applications/Alacritty.app/Contents/MacOS/alacritty
  310   300 /bin/zsh
`,
			310, "Alacritty",
		},
		{
			"SketchyBar reaches launchd without a bundle",
			`
    1     0 /sbin/launchd
  200     1 /opt/homebrew/bin/sketchybar
  210   200 /bin/sh
  220   210 /bin/bash
`,
			220, "sketchybar",
		},
		{
			"the outermost non-shell wins before launchd",
			`
    1     0 /sbin/launchd
  200     1 /opt/homebrew/bin/sketchybar
  210   200 /usr/bin/env
  220   210 /opt/homebrew/bin/spmeta-wrapper
  230   220 /bin/sh
`,
			230, "sketchybar",
		},
		{
			"tmux server ends at launchd with nothing but shells",
			`
    1     0 /sbin/launchd
  200     1 tmux
  210   200 -zsh
  220   210 /bin/sh
`,
			220, "",
		},
		{
			"an app wins over non-bundle ancestors above it",
			`
    1     0 /sbin/launchd
  100     1 /Applications/iTerm.app/Contents/MacOS/iTerm2
  200   100 /opt/homebrew/bin/sketchybar
  210   200 /bin/sh
`,
			210, "iTerm",
		},
		{
			"missing pid breaks the chain",
			`
  600   500 /usr/bin/login
  700   600 -zsh
`,
			700, "",
		},
		{
			"a broken chain never trusts the non-bundle rule",
			`
  200   150 /opt/homebrew/bin/sketchybar
  210   200 /bin/sh
`,
			210, "",
		},
		{
			"cycle",
			`
  200   210 /opt/homebrew/bin/foo
  210   200 /bin/sh
`,
			210, "",
		},
		{"empty output", "", 100, ""},
		{"start pid unknown", "    1     0 /sbin/launchd\n", 999, ""},
		{"start pid is launchd", "    1     0 /sbin/launchd\n", 1, ""},
		{
			"garbage lines are skipped",
			`PID PPID COMM
not a line
  310   300 /bin/zsh
  300     1 /Applications/kitty.app/Contents/MacOS/kitty
  abc   def /x
`,
			310, "kitty",
		},
		{
			"a path with spaces stays whole",
			`
  300     1 /Applications/My Terminal.app/Contents/MacOS/My Terminal
  310   300 /bin/zsh
`,
			310, "My Terminal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FromProcesses(tt.snapshot, tt.start); got != tt.want {
				t.Errorf("FromProcesses = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromProcessesBoundsTheWalk(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	const depth = maxHops + 10
	for pid := 100; pid < 100+depth; pid++ {
		fmt.Fprintf(&b, "%d %d /bin/sh\n", pid, pid+1)
	}
	fmt.Fprintf(&b, "%d 1 /Applications/Deep.app/Contents/MacOS/deep\n", 100+depth)
	if got := FromProcesses(b.String(), 100); got != "" {
		t.Errorf("FromProcesses = %q, want empty past %d hops", got, maxHops)
	}
}

func TestFromTermProgram(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"", ""},
		{"Apple_Terminal", "Terminal"},
		{"iTerm.app", "iTerm"},
		{"ghostty", "Ghostty"},
		{"WarpTerminal", "Warp"},
		{"vscode", "Visual Studio Code"},
		{"WezTerm", "WezTerm"},
		{"tmux", ""},
		{"Hyper.app", "Hyper"},
		{"Hyper", "Hyper"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := FromTermProgram(tt.in); got != tt.want {
				t.Errorf("FromTermProgram(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveFallsBackSilently(t *testing.T) {
	t.Parallel()
	ok := func(context.Context) (string, error) {
		return "  500     1 /Applications/Ghostty.app/Contents/MacOS/ghostty\n  600   500 /bin/zsh\n", nil
	}
	failing := func(context.Context) (string, error) { return "", errors.New("ps: not found") }
	tests := []struct {
		name string
		run  func(context.Context) (string, error)
		term string
		want string
	}{
		{"process chain wins over TERM_PROGRAM", ok, "Apple_Terminal", "Ghostty"},
		{"ps fails, TERM_PROGRAM answers", failing, "iTerm.app", "iTerm"},
		{"ps fails, tmux hides the terminal", failing, "tmux", ""},
		{"ps fails, nothing set", failing, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolve(context.Background(), tt.term, 600, tt.run, nil); got != tt.want {
				t.Errorf("resolve = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalProgram(t *testing.T) {
	tests := []struct {
		termProgram, lcTerminal, want string
	}{
		{"tmux", "iTerm2", "iTerm.app"},
		{"", "iTerm2", "iTerm.app"},
		{"Apple_Terminal", "iTerm2", "Apple_Terminal"},
		{"tmux", "", "tmux"},
		{"tmux", "SomethingElse", "tmux"},
	}
	for _, tc := range tests {
		if got := terminalProgram(tc.termProgram, tc.lcTerminal); got != tc.want {
			t.Errorf("terminalProgram(%q, %q) = %q, want %q", tc.termProgram, tc.lcTerminal, got, tc.want)
		}
	}
}
