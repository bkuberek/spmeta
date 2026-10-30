package reference

import (
	"slices"
	"strings"
)

// Help returns the help page of topic: "" or "spmeta" for spmeta itself, or
// one of Topics. ok is false when there is no such page.
func Help(topic string) (text string, ok bool) {
	c := root
	if topic != "" && topic != root.Name {
		c = nil
		for _, sub := range root.Commands {
			if sub.Name == topic && sub.Intro != "" {
				c = sub
			}
		}
		if c == nil {
			return "", false
		}
	}
	var b strings.Builder
	writeHelp(&b, c)
	return b.String(), true
}

func writeHelp(b *strings.Builder, c *Command) {
	b.WriteString(c.Intro + "\n\nUsage:\n")
	if !c.group() {
		b.WriteString("  " + usageLine(c) + "\n")
	}
	for _, sub := range c.Commands {
		b.WriteString("  " + usageLine(sub) + "\n")
	}

	if len(c.Commands) > 0 {
		b.WriteString("\nCommands:\n")
		rows := make([][2]string, 0, len(c.Commands))
		for _, sub := range c.Commands {
			rows = append(rows, [2]string{sub.Name, sub.Summary})
		}
		writeColumns(b, rows, 3)
	}

	flags := listFlags(c)
	b.WriteString("\nFlags:\n")
	rows := make([][2]string, 0, len(flags))
	for _, f := range flags {
		label := "    "
		if f.Short != "" {
			label = "-" + f.Short + ", "
		}
		rows = append(rows, [2]string{label + flagUsage(f.Flag), f.describe()})
	}
	writeColumns(b, rows, 2)

	var tail []string
	if env := envOf(flags); len(env) > 0 {
		tail = append(tail, "Environment: "+strings.Join(env, ", ")+".")
	}
	tail = append(tail, c.Notes...)
	if len(tail) > 0 {
		b.WriteString("\n" + strings.Join(tail, "\n") + "\n")
	}
}

// usageLine is the command path followed by its short synopsis.
func usageLine(c *Command) string {
	syn := c.Usage
	switch {
	case syn != "":
	case c.group():
		names := make([]string, 0, len(c.Commands))
		for _, sub := range c.Commands {
			names = append(names, sub.Name)
		}
		syn = strings.Join(names, "|")
	default:
		syn = synopsis(c)
	}
	return strings.TrimSpace(c.Path + " " + syn)
}

// synopsis lists every flag of c but -h, then its positional argument.
func synopsis(c *Command) string {
	var parts []string
	for _, f := range c.Flags {
		if !f.Builtin {
			parts = append(parts, "["+flagUsage(f)+"]")
		}
	}
	if len(c.Args) > 0 {
		arg := strings.Join(c.Args, "|")
		if c.ArgsOptional {
			arg = "[" + arg + "]"
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

// flagUsage is "--name" followed by the value, if the flag takes one.
func flagUsage(f Flag) string {
	if f.TakesValue() {
		return "--" + f.Name + " " + f.Arg()
	}
	return "--" + f.Name
}

// listedFlag is a flag on a help page. in names the subcommands that accept
// it when a group lists the flags of its subcommands and not all of them do.
type listedFlag struct {
	Flag
	in []string
}

func (f listedFlag) describe() string {
	var notes []string
	if len(f.in) > 0 {
		notes = append(notes, strings.Join(f.in, ", ")+" only")
	}
	if f.Default != "" {
		notes = append(notes, "default "+f.Default)
	}
	if len(notes) == 0 {
		return f.Help
	}
	return f.Help + " (" + strings.Join(notes, "; ") + ")"
}

// listFlags returns the flags a help page lists: the command's own, or for
// a group the flags of all its subcommands, each once.
func listFlags(c *Command) []listedFlag {
	if !c.group() {
		list := make([]listedFlag, 0, len(c.Flags))
		for _, f := range c.Flags {
			list = append(list, listedFlag{Flag: f})
		}
		return list
	}
	var list []listedFlag
	index := map[string]int{}
	for _, sub := range c.Commands {
		for _, f := range sub.Flags {
			if i, ok := index[f.Name]; ok {
				list[i].in = append(list[i].in, sub.Name)
				continue
			}
			index[f.Name] = len(list)
			list = append(list, listedFlag{Flag: f, in: []string{sub.Name}})
		}
	}
	for i := range list {
		if len(list[i].in) == len(c.Commands) {
			list[i].in = nil
		}
	}
	return list
}

// envOf returns the environment variables of flags, each once.
func envOf(flags []listedFlag) []string {
	var env []string
	for _, f := range flags {
		if f.Env != "" && !slices.Contains(env, f.Env) {
			env = append(env, f.Env)
		}
	}
	return env
}

// writeColumns writes rows as two aligned columns indented by two spaces,
// with gap spaces after the widest left cell.
func writeColumns(b *strings.Builder, rows [][2]string, gap int) {
	width := 0
	for _, r := range rows {
		width = max(width, len(r[0]))
	}
	for _, r := range rows {
		b.WriteString("  " + r[0] + strings.Repeat(" ", width-len(r[0])+gap) + r[1] + "\n")
	}
}
