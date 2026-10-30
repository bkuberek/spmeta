package reference

import (
	"strconv"
	"strings"
)

// ManInfo is what the man page header shows besides the reference.
type ManInfo struct {
	// Version is the spmeta version, such as 0.3.0; empty shows none.
	Version string
	// Date is the release date as YYYY-MM-DD.
	Date string
}

// ManPage returns the spmeta(1) man page in roff with the man macros.
func ManPage(info ManInfo) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	// text writes roff text, one sentence per line as roff expects, each
	// wrapped before 80 bytes.
	text := func(s string) {
		for _, l := range wrap(s) {
			line(l)
		}
	}

	date := info.Date
	if date == "" {
		date = `""`
	}
	source := "spmeta"
	if info.Version != "" {
		source += " " + info.Version
	}
	line(`.\" Generated from the spmeta command reference; do not edit.`)
	line(".TH SPMETA 1 " + date + ` "` + source + `" "User Commands"`)

	line(".SH NAME")
	text(`spmeta \- ` + roff(root.Summary))

	line(".SH SYNOPSIS")
	for i, c := range Commands() {
		if c.group() {
			continue
		}
		if i > 0 {
			line(".br")
		}
		text(manSynopsis(c))
	}

	line(".SH DESCRIPTION")
	for i, p := range description {
		if i > 0 {
			line(".PP")
		}
		text(roff(p))
	}

	line(".SH COMMANDS")
	for _, c := range Commands()[1:] {
		if c.group() {
			continue
		}
		line(".TP")
		text(manCommand(c))
		text(roff(c.Summary) + ".")
	}

	line(".SH OPTIONS")
	for _, c := range Commands() {
		var flags []Flag
		for _, f := range c.Flags {
			if !f.Builtin || c == root {
				flags = append(flags, f)
			}
		}
		if len(flags) == 0 {
			continue
		}
		if c != root {
			line(`.SS "` + c.Path + `"`)
		}
		for _, f := range flags {
			line(".TP")
			text(manFlag(f))
			desc := f.Help + "."
			if f.Default != "" {
				desc += " Default: " + f.Default + "."
			}
			if f.Env != "" {
				desc += " Environment: " + f.Env + "."
			}
			text(roff(desc))
		}
	}

	line(`.SH "EXIT STATUS"`)
	for _, e := range exitCodes {
		line(".TP")
		line(".B " + strconv.Itoa(e.Code))
		text(roff(e.Meaning) + ".")
	}

	line(".SH ENVIRONMENT")
	text(roff("Environment variables override the configuration file, and flags override both. An empty variable counts as unset."))
	seen := map[string]bool{}
	for _, c := range Commands() {
		for _, f := range c.Flags {
			if f.Env == "" || seen[f.Env] {
				continue
			}
			seen[f.Env] = true
			same := `Same as \fB` + roff("--"+f.Name) + `\fR`
			if c != root {
				same += ` of \fB` + roff(c.Path) + `\fR`
			}
			line(".TP")
			line(".B " + f.Env)
			text(same + ".")
		}
	}
	line(".TP")
	line(".B XDG_CONFIG_HOME")
	text(roff("The base configuration directory, used only when it is an absolute path."))

	line(".SH FILES")
	for _, f := range files {
		line(".TP")
		line(".I " + roff(f.name))
		text(roff(f.text) + ".")
	}

	line(".SH EXAMPLES")
	for i, e := range examples {
		if i > 0 {
			line(".PP")
		}
		text(roff(e.text) + ":")
		line(".PP")
		line(".RS 4")
		line(`\fB` + roff(e.name) + `\fR`)
		line(".RE")
	}

	line(`.SH "SEE ALSO"`)
	text(roff("The full documentation, with the JSON schema, the template fields, and status bar examples:"))
	line("https://github.com/bkuberek/spmeta")
	return b.String()
}

// manSynopsis is the SYNOPSIS line of c, with every flag.
func manSynopsis(c *Command) string {
	parts := []string{`\fB` + c.Path + `\fR`}
	for _, f := range c.Flags {
		if !f.Builtin {
			parts = append(parts, "["+manFlag(f)+"]")
		}
	}
	if arg := manArg(c); arg != "" {
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

// manCommand is the COMMANDS tag of c: its path and positional argument.
func manCommand(c *Command) string {
	tag := `\fB` + c.Path + `\fR`
	if arg := manArg(c); arg != "" {
		tag += " " + arg
	}
	return tag
}

func manArg(c *Command) string {
	if len(c.Args) == 0 {
		return ""
	}
	arg := `\fI` + strings.Join(c.Args, "|") + `\fR`
	if c.ArgsOptional {
		arg = "[" + arg + "]"
	}
	return arg
}

// manFlag shows f in bold with its value in italics, joined by a
// non-breaking space so a line never ends between them.
func manFlag(f Flag) string {
	s := `\fB` + roff("--"+f.Name) + `\fR`
	if f.Short != "" {
		s = `\fB` + roff("-"+f.Short) + `\fR, ` + s
	}
	if f.TakesValue() {
		s += `\~\fI` + roff(f.Arg()) + `\fR`
	}
	return s
}

// roff escapes backslashes in s, and hyphens, which would otherwise print as
// typographic hyphens instead of the ASCII ones that options are typed with.
func roff(s string) string {
	return strings.NewReplacer(`\`, `\e`, "-", `\-`).Replace(s)
}

// maxLine is the length that roff text lines are wrapped before.
const maxLine = 80

// wrap splits escaped roff text into lines: one per sentence, each broken
// at spaces before maxLine bytes where possible. A line that would start
// with a dot or an apostrophe, which start requests, is protected with \&.
func wrap(s string) []string {
	var lines []string
	for sentence := range strings.SplitAfterSeq(s, ". ") {
		cur := ""
		for _, word := range strings.Fields(sentence) {
			if cur != "" && len(cur)+1+len(word) >= maxLine {
				lines = append(lines, cur)
				cur = ""
			}
			if cur != "" {
				cur += " "
			}
			cur += word
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}
	for i, l := range lines {
		if strings.HasPrefix(l, ".") || strings.HasPrefix(l, "'") {
			lines[i] = `\&` + l
		}
	}
	return lines
}
