package reference

import (
	"fmt"
	"slices"
	"strings"
)

var shells = []string{"bash", "zsh", "fish"}

// Shells returns the shells Completion writes scripts for.
func Shells() []string { return slices.Clone(shells) }

// Completion returns the completion script for shell. The scripts are
// static: they complete commands, flags and accepted values without running
// spmeta.
func Completion(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashScript(), nil
	case "zsh":
		return zshScript(), nil
	case "fish":
		return fishScript(), nil
	}
	last := len(shells) - 1
	return "", fmt.Errorf("unsupported shell %q (want %s or %s)", shell, strings.Join(shells[:last], ", "), shells[last])
}

// positional returns the words the first argument after c can be: its
// subcommands, or the values of its positional argument.
func positional(c *Command) []string {
	if len(c.Commands) == 0 {
		return c.Args
	}
	names := make([]string, 0, len(c.Commands))
	for _, sub := range c.Commands {
		names = append(names, sub.Name)
	}
	return names
}

// subcommandPaths returns the path of every command but the root.
func subcommandPaths() []string {
	var paths []string
	for _, c := range Commands()[1:] {
		paths = append(paths, c.Path)
	}
	return paths
}

// quoteAll wraps each of words in q.
func quoteAll(words []string, q string) []string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = q + w + q
	}
	return quoted
}

const bashHead = `# bash completion for spmeta
# Generated from the spmeta command reference; do not edit.
# Load it into the current shell with: eval "$(spmeta completion bash)"

_spmeta() {
    local cur=${COMP_WORDS[COMP_CWORD]} prev=${COMP_WORDS[COMP_CWORD-1]}
    # Bash splits --flag=value into the words --flag, = and value.
    if [[ $cur == = ]]; then
        cur=
    elif [[ $prev == = ]] && ((COMP_CWORD > 2)); then
        prev=${COMP_WORDS[COMP_CWORD-2]}
    fi

    # The command is the run of subcommand names that follows spmeta.
    local cmd=spmeta i=1
    while ((i < COMP_CWORD)); do
        case "$cmd ${COMP_WORDS[i]}" in
`

const bashFoot = `    esac
    if [[ $cur == -* ]]; then
        COMPREPLY=($(compgen -W "$flags" -- "$cur"))
    elif ((i == COMP_CWORD)); then
        COMPREPLY=($(compgen -W "$args" -- "$cur"))
    fi
    return 0
}

complete -F _spmeta spmeta
`

func bashScript() string {
	var b strings.Builder
	b.WriteString(bashHead)
	b.WriteString("        " + strings.Join(quoteAll(subcommandPaths(), `"`), " | ") + ")\n")
	b.WriteString("            cmd=\"$cmd ${COMP_WORDS[i]}\" ;;\n")
	b.WriteString("        *) break ;;\n        esac\n        ((i++))\n    done\n\n")

	// Values: one case arm per way of completing them, matching the
	// command and the flag before the cursor.
	var actions []string
	patterns := map[string][]string{}
	for _, c := range Commands() {
		for _, f := range c.Flags {
			if !f.TakesValue() {
				continue
			}
			action := "return 0"
			switch {
			case len(f.Values) > 0:
				action = `COMPREPLY=($(compgen -W "` + strings.Join(f.Values, " ") + `" -- "$cur"))`
			case f.File:
				action = "local IFS=$'\\n'\n        COMPREPLY=($(compgen -f -- \"$cur\"))\n        compopt -o filenames 2>/dev/null"
			}
			if _, ok := patterns[action]; !ok {
				actions = append(actions, action)
			}
			patterns[action] = append(patterns[action], `"`+c.Path+" --"+f.Name+`"`, `"`+c.Path+" -"+f.Name+`"`)
		}
	}
	b.WriteString("    case \"$cmd $prev\" in\n")
	for _, action := range actions {
		b.WriteString("    " + strings.Join(patterns[action], " | ") + ")\n")
		if action != "return 0" {
			b.WriteString("        " + action + "\n")
		}
		b.WriteString("        return 0 ;;\n")
	}
	b.WriteString("    esac\n\n")

	b.WriteString("    local flags= args=\n    case $cmd in\n")
	for _, c := range Commands() {
		words := positional(c)
		if len(c.Flags) == 0 && len(words) == 0 {
			continue
		}
		var flags []string
		for _, f := range c.Flags {
			if f.Short != "" {
				flags = append(flags, "-"+f.Short)
			}
			flags = append(flags, "--"+f.Name)
		}
		b.WriteString(`    "` + c.Path + "\")\n")
		b.WriteString(`        flags="` + strings.Join(flags, " ") + "\"\n")
		b.WriteString(`        args="` + strings.Join(words, " ") + "\" ;;\n")
	}
	b.WriteString(bashFoot)
	return b.String()
}

const zshHead = `#compdef spmeta
# zsh completion for spmeta
# Generated from the spmeta command reference; do not edit.
# Install it as _spmeta in a directory on $fpath, or load it into the
# current shell after compinit with: source <(spmeta completion zsh)

_spmeta() {
  local cmd=spmeta i=2
  # The command is the run of subcommand names that follows spmeta.
  while (( i < CURRENT )); do
    case "$cmd ${words[i]}" in
`

const zshFoot = `  esac
  (( $#specs )) || return 1
  _arguments $specs
}

if [[ $funcstack[1] == _spmeta ]]; then
  _spmeta "$@"
else
  compdef _spmeta spmeta
fi
`

func zshScript() string {
	var b strings.Builder
	b.WriteString(zshHead)
	b.WriteString("      (" + strings.Join(quoteAll(subcommandPaths(), `"`), "|") + ")\n")
	b.WriteString("        cmd=\"$cmd ${words[i]}\" ;;\n")
	b.WriteString("      (*) break ;;\n    esac\n    (( i++ ))\n  done\n")
	b.WriteString("  # Complete the rest as if the command were a program of its own.\n")
	b.WriteString("  if (( i > 2 )); then\n    words=(\"${words[1]}\" \"${(@)words[i,-1]}\")\n    (( CURRENT -= i - 2 ))\n  fi\n\n")
	b.WriteString("  local -a specs\n  case $cmd in\n")
	for _, c := range Commands() {
		words := positional(c)
		if len(c.Flags) == 0 && len(words) == 0 {
			continue
		}
		b.WriteString(`    ("` + c.Path + "\")\n      specs=(\n")
		for _, f := range c.Flags {
			b.WriteString("        " + zshFlagSpec(f) + "\n")
		}
		b.WriteString("      )\n")
		if len(words) > 0 {
			b.WriteString("      (( CURRENT == 2 )) && specs+=(" + zshArgSpec(c, words) + ")\n")
		}
		b.WriteString("      ;;\n")
	}
	b.WriteString(zshFoot)
	return b.String()
}

// zshFlagSpec is the _arguments specification of f.
func zshFlagSpec(f Flag) string {
	help := zshQuote("[" + strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(f.Help) + "]")
	if f.Short != "" {
		// After -h or --help nothing else is completed.
		return "'(- *)'{-" + f.Short + ",--" + f.Name + "}" + help
	}
	if !f.TakesValue() {
		return zshQuote("--"+f.Name) + help
	}
	action := " "
	switch {
	case len(f.Values) > 0:
		action = "(" + strings.Join(f.Values, " ") + ")"
	case f.File:
		action = "_files"
	}
	return zshQuote("--"+f.Name+"=") + help + zshQuote(":"+f.Name+":"+action)
}

// zshArgSpec is the _arguments specification of the first positional
// argument of c, which is one of words.
func zshArgSpec(c *Command, words []string) string {
	if len(c.Commands) == 0 {
		return zshQuote("1:argument:(" + strings.Join(words, " ") + ")")
	}
	items := make([]string, 0, len(c.Commands))
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	for _, sub := range c.Commands {
		items = append(items, sub.Name+`\:"`+escape.Replace(sub.Summary)+`"`)
	}
	return zshQuote("1:command:((" + strings.Join(items, " ") + "))")
}

// zshQuote quotes s as one single-quoted shell word.
func zshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const fishHead = `# fish completion for spmeta
# Generated from the spmeta command reference; do not edit.
# Load it into the current shell with: spmeta completion fish | source

# __spmeta_command prints the command that the words before the cursor
# select, such as "spmeta auth login".
function __spmeta_command
    set -l words (commandline -opc)
    set -e words[1]
    set -l cmd spmeta
    for word in $words
        switch "$cmd $word"
`

const fishFunctions = `                set cmd "$cmd $word"
            case '*'
                break
        end
    end
    echo $cmd
end

# __spmeta_is CMD succeeds when the command line selects CMD.
function __spmeta_is
    test (__spmeta_command) = "$argv[1]"
end

# __spmeta_next CMD succeeds when the cursor is on the first word after CMD.
function __spmeta_next
    __spmeta_is $argv[1]
    and test (count (commandline -opc)) -eq (count (string split ' ' -- $argv[1]))
end

complete -c spmeta -f
`

func fishScript() string {
	var b strings.Builder
	b.WriteString(fishHead)
	b.WriteString("            case " + strings.Join(quoteAll(subcommandPaths(), "'"), " ") + "\n")
	b.WriteString(fishFunctions)
	for _, c := range Commands() {
		cmd := c.Path
		if strings.Contains(cmd, " ") {
			cmd = `"` + cmd + `"`
		}
		is := fishQuote("__spmeta_is " + cmd)
		next := fishQuote("__spmeta_next " + cmd)
		b.WriteString("\n")
		for _, sub := range c.Commands {
			b.WriteString("complete -c spmeta -n " + next + " -a " + sub.Name + " -d " + fishQuote(sub.Summary) + "\n")
		}
		if len(c.Args) > 0 {
			b.WriteString("complete -c spmeta -n " + next + " -a " + fishQuote(strings.Join(c.Args, " ")) + "\n")
		}
		for _, f := range c.Flags {
			line := "complete -c spmeta -n " + is
			if f.Short != "" {
				line += " -s " + f.Short
			}
			line += " -l " + f.Name
			switch {
			case len(f.Values) > 0:
				line += " -x -a " + fishQuote(strings.Join(f.Values, " "))
			case f.File:
				line += " -r -F"
			case f.TakesValue():
				line += " -x"
			}
			b.WriteString(line + " -d " + fishQuote(f.Help) + "\n")
		}
	}
	return b.String()
}

// fishQuote quotes s as one single-quoted fish word.
func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}
