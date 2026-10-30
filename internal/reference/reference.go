// Package reference describes the spmeta command line once: its commands,
// flags, accepted values, exit codes, environment and files. The help pages,
// the shell completion scripts and the spmeta(1) man page are all rendered
// from it, and a test in internal/cli checks it against the flags each
// command registers. Public contract: docs/design/cli.md.
package reference

import "strings"

// Flag is one command-line flag of a command.
type Flag struct {
	// Name is the flag without dashes, as registered with the flag package.
	Name string
	// Short is a one-letter alias; only -h has one.
	Short string
	// Value names the flag's value in usage text, such as PATH. It is empty
	// for a boolean flag and for a flag with Values.
	Value string
	// Values lists every accepted value of a flag that takes one of a set.
	Values []string
	// File marks a value that is a file path, so completion offers files.
	File bool
	// Env is the environment variable with the same effect, if any.
	Env string
	// Default is shown as "(default X)"; empty when there is nothing to show.
	Default string
	// Help is the one-line description, without a final period.
	Help string
	// Builtin marks -h and --help, which the flag package recognizes
	// without a registered flag.
	Builtin bool
}

// Arg returns how usage text shows the flag's value: the placeholder, the
// accepted values joined with "|", or "" for a boolean flag.
func (f Flag) Arg() string {
	if len(f.Values) > 0 {
		return strings.Join(f.Values, "|")
	}
	return f.Value
}

// TakesValue reports whether the flag needs a value.
func (f Flag) TakesValue() bool { return f.Arg() != "" }

// Command is spmeta itself or one of its subcommands.
type Command struct {
	// Name is the word that selects the command; "spmeta" for the root.
	Name string
	// Path is the command line that selects the command, such as
	// "spmeta auth login". The package sets it.
	Path string
	// Summary is the one-line description in command lists.
	Summary string
	// Intro is the first line of the command's help page. A command without
	// one has no help page of its own.
	Intro string
	// Usage replaces the synopsis generated from the flags on help pages.
	Usage string
	// Flags are the command's flags, in the order help pages list them.
	Flags []Flag
	// Args lists the values of the command's positional argument.
	Args []string
	// ArgsOptional marks the positional argument as optional.
	ArgsOptional bool
	// Notes are lines printed at the end of the help page.
	Notes []string
	// Commands are the subcommands.
	Commands []*Command
}

// group reports whether the command only selects a subcommand, like auth,
// and has no flags of its own besides -h.
func (c *Command) group() bool {
	if len(c.Commands) == 0 {
		return false
	}
	for _, f := range c.Flags {
		if !f.Builtin {
			return false
		}
	}
	return true
}

// ExitCode is one exit status and its meaning.
type ExitCode struct {
	Code    int
	Meaning string
}

// entry is a named item of a man page list, such as a file or an example.
type entry struct {
	name, text string
}

var (
	helpFlag   = Flag{Name: "help", Short: "h", Help: "Print this help", Builtin: true}
	debugFlag  = Flag{Name: "debug", Help: "Write diagnostics to stderr, never tokens or codes"}
	configFlag = Flag{Name: "config", Value: "PATH", File: true, Env: "SPMETA_CONFIG", Help: "Read this configuration file instead of the default"}
)

var root = build(&Command{
	Name:    "spmeta",
	Summary: "print the track Spotify is currently playing",
	Intro:   "spmeta prints the track Spotify is currently playing.",
	Usage:   "[flags]",
	Flags: []Flag{
		{Name: "source", Values: []string{"auto", "local", "web"}, Env: "SPMETA_SOURCE", Default: "auto", Help: "Source to read"},
		{Name: "format", Values: []string{"text", "json", "template", "waybar"}, Env: "SPMETA_FORMAT", Default: "text", Help: "Output format"},
		{Name: "template", Value: "STR", Env: "SPMETA_TEMPLATE", Help: "Template for --format template, for example '{artist} - {title}'"},
		{Name: "player", Value: "NAME", Env: "SPMETA_PLAYER", Help: "Preferred MPRIS player (Linux)"},
		{Name: "watch", Help: "Keep running and print a line whenever the item or its state changes"},
		{Name: "interval", Value: "SECONDS", Default: "1, or 3 for Web", Help: "Seconds between reads with --watch; at least 0.5, or 1 for Web"},
		configFlag,
		{Name: "quiet", Help: "Print nothing; only set the exit code"},
		debugFlag,
		helpFlag,
		{Name: "version", Help: "Print version and build information"},
	},
	Notes: []string{"Configuration: $XDG_CONFIG_HOME/spmeta/config.toml, else ~/.config/spmeta/config.toml."},
	Commands: []*Command{
		{
			Name:    "auth",
			Summary: "Manage the login to the Spotify Web API",
			Intro:   "spmeta auth manages the login that the Web source uses.",
			Flags:   []Flag{helpFlag},
			Notes: []string{
				"Run 'spmeta auth login' without a Client ID to see how to create the Spotify app.",
				"The login is stored in ~/.config/spmeta/token.json ($XDG_CONFIG_HOME/spmeta when set; --config does not move it) and lasts about 6 months.",
			},
			Commands: []*Command{
				{
					Name:    "login",
					Summary: "Log in to Spotify in your browser and store the login",
					Flags: []Flag{
						{Name: "client-id", Value: "ID", Env: "SPMETA_CLIENT_ID", Default: "SPMETA_CLIENT_ID, else client_id in the configuration file", Help: "Client ID of your Spotify app"},
						{Name: "port", Value: "PORT", Default: "8974", Help: "Loopback port for the browser callback; 0 picks any free port"},
						{Name: "no-browser", Help: "Print the login URL instead of opening a browser"},
						debugFlag,
						configFlag,
						helpFlag,
					},
				},
				{Name: "logout", Summary: "Delete the stored login", Flags: []Flag{configFlag, helpFlag}},
				{Name: "status", Summary: "Show the stored login; exit 4 when there is none", Flags: []Flag{configFlag, helpFlag}},
			},
		},
		{
			Name:    "doctor",
			Summary: "Report which sources work and why the others do not",
			Intro:   "spmeta doctor reports which sources spmeta can read and why the others cannot.",
			Flags:   []Flag{debugFlag, configFlag, helpFlag},
			Notes: []string{
				"On macOS it may wait up to 120 s while you answer the Automation prompt.",
				"With a Web login (spmeta auth login) it makes one request to the Spotify Web API.",
				"Exit status: 0 when at least one source is usable, 4 when none is.",
			},
		},
		{
			Name:    "completion",
			Summary: "Print the shell completion script for bash, zsh, or fish",
			Intro:   "spmeta completion prints a shell completion script on stdout.",
			Flags:   []Flag{helpFlag},
			Args:    shells,
			Notes: []string{
				"Homebrew, .deb, and .rpm installs include the scripts; zsh with Homebrew also needs",
				"$(brew --prefix)/share/zsh/site-functions in FPATH before compinit. Otherwise load it",
				"in the current shell, or add the line to the shell's startup file:",
				"  bash  eval \"$(spmeta completion bash)\"",
				"  zsh   source <(spmeta completion zsh)    (after compinit)",
				"  fish  spmeta completion fish | source",
			},
		},
		{Name: "version", Summary: "Print version and build information"},
		{Name: "help", Summary: "Print this help, or the help of a command, such as 'spmeta help auth'", ArgsOptional: true},
	},
})

var exitCodes = []ExitCode{
	{0, "An item is playing, paused, or loaded, and was written. Also: --watch stopped by SIGINT or SIGTERM, and every other command that succeeded"},
	{1, "Unexpected error: unparseable source output, network failure, timeout, or a Web 5xx after one retry"},
	{2, "Usage error: an unknown flag or command, an invalid value in a flag, an environment variable, or the configuration file, or a missing template"},
	{3, "A source is reachable but nothing is loaded, or no Web device is active"},
	{4, "No source available: Spotify not running, no MPRIS player, no session bus, not logged in to Web, or an unsupported platform"},
	{5, "Web authentication required: the refresh failed, the token file is unreadable, or the login was aborted or timed out. Run spmeta auth login"},
	{6, "Permission denied: macOS Automation not granted, or a Web 403 (not on the app's allowlist, or the app owner's Premium lapsed)"},
	{7, "Rate limited by the Web API, and spmeta gave up"},
}

// description is the DESCRIPTION section of the man page, one paragraph per
// string.
var description = []string{
	"spmeta prints the item Spotify is playing right now: one line of text, JSON, a template of your own, or a line for a Waybar custom module. " +
		"It reads the Spotify client on this machine, through AppleScript on macOS and MPRIS over D-Bus on Linux, and never launches Spotify. " +
		"After spmeta auth login it can also read playback on your other devices through the Spotify Web API.",
	"With --watch, spmeta keeps running and prints a line whenever the item or its playback state changes.",
	"Flags must precede positional arguments. -flag and --flag are equivalent, and a value follows as --flag value or --flag=value. " +
		"Flags override environment variables, which override the configuration file. An empty environment variable counts as unset.",
}

var files = []entry{
	{"$XDG_CONFIG_HOME/spmeta/config.toml", "The optional TOML configuration file, used when XDG_CONFIG_HOME is an absolute path. --config and SPMETA_CONFIG name another file"},
	{"~/.config/spmeta/config.toml", "The configuration file when XDG_CONFIG_HOME is not set"},
	{"token.json", "The Web API login that spmeta auth login writes, with mode 0600, in the directory of the default configuration file. --config does not move it"},
}

var examples = []entry{
	{"spmeta", "Print one line, such as the state, title, artist, album, and time"},
	{"spmeta --format json", "Print the item as one JSON object"},
	{"spmeta --template '{artist} - {title}'", "Print a line of your own"},
	{"spmeta --watch --format waybar", "Feed a Waybar custom module that has return-type json"},
	{"spmeta doctor", "Show which sources work, and why the others do not"},
	{`eval "$(spmeta completion bash)"`, "Load bash completion into the current shell"},
}

// Root returns the spmeta command, the root of the command tree.
func Root() *Command { return root }

// Commands returns every command, the root first and each command before
// its subcommands.
func Commands() []*Command {
	var all []*Command
	var walk func(*Command)
	walk = func(c *Command) {
		all = append(all, c)
		for _, sub := range c.Commands {
			walk(sub)
		}
	}
	walk(root)
	return all
}

// ExitCodes returns the exit statuses of spmeta, in order.
func ExitCodes() []ExitCode { return exitCodes }

// Topics returns the commands that `spmeta help` explains.
func Topics() []string { return topicsOf(root) }

// build sets the paths of c and its subcommands, and the arguments of the
// help command, then returns c.
func build(c *Command) *Command {
	setPaths(c, "")
	for _, sub := range c.Commands {
		if sub.Name == "help" {
			sub.Args = topicsOf(c)
		}
	}
	return c
}

func setPaths(c *Command, parent string) {
	c.Path = strings.TrimSpace(parent + " " + c.Name)
	for _, sub := range c.Commands {
		setPaths(sub, c.Path)
	}
}

func topicsOf(c *Command) []string {
	var topics []string
	for _, sub := range c.Commands {
		if sub.Intro != "" {
			topics = append(topics, sub.Name)
		}
	}
	return topics
}
