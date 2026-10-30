package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/bkuberek/spmeta/internal/auth"
	"github.com/bkuberek/spmeta/internal/config"
	"github.com/bkuberek/spmeta/internal/model"
	"github.com/bkuberek/spmeta/internal/reference"
)

// flagSets returns the flag set of every command in the reference, keyed by
// its path; nil means the command takes no flags.
func flagSets() map[string]*flag.FlagSet {
	logout, _ := newTokenFlags("logout")
	status, _ := newTokenFlags("status")
	doctor, _, _ := newDoctorFlags()
	return map[string]*flag.FlagSet{
		"spmeta":             newRootFlags().set,
		"spmeta auth":        nil,
		"spmeta auth login":  newLoginFlags(auth.DefaultLoginPort).set,
		"spmeta auth logout": logout,
		"spmeta auth status": status,
		"spmeta doctor":      doctor,
		"spmeta completion":  newCompletionFlags(),
		"spmeta version":     nil,
		"spmeta help":        nil,
	}
}

// TestFlagSetsMatchTheReference keeps the help text, the completion scripts
// and the man page, which come from the reference, in step with the flags
// each command really registers.
func TestFlagSetsMatchTheReference(t *testing.T) {
	sets := flagSets()
	commands := map[string]*reference.Command{}
	for _, c := range reference.Commands() {
		commands[c.Path] = c
	}
	for _, path := range slices.Sorted(maps.Keys(sets)) {
		if commands[path] == nil {
			t.Errorf("%s has flags but is not in the reference", path)
		}
	}
	for path, c := range commands {
		set, ok := sets[path]
		if !ok {
			t.Errorf("reference command %q has no entry in flagSets", path)
			continue
		}
		registered := map[string]*flag.Flag{}
		if set != nil {
			set.VisitAll(func(f *flag.Flag) { registered[f.Name] = f })
		}
		documented := map[string]bool{}
		for _, f := range c.Flags {
			if f.Builtin {
				continue
			}
			documented[f.Name] = true
			r := registered[f.Name]
			if r == nil {
				t.Errorf("%s --%s is in the reference but not registered", path, f.Name)
				continue
			}
			if isBool(r) == f.TakesValue() {
				t.Errorf("%s --%s: the reference says takes a value = %v, the flag set disagrees", path, f.Name, f.TakesValue())
			}
			if def := r.DefValue; def != "" && def != "false" && def != "0" && f.Default != def {
				t.Errorf("%s --%s: the reference default is %q, the flag set default is %q", path, f.Name, f.Default, def)
			}
		}
		for name := range registered {
			if !documented[name] {
				t.Errorf("%s --%s is registered but not in the reference", path, name)
			}
		}
	}
}

func isBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// TestEnumeratedValuesMatchTheConfiguration checks that the values the
// reference lists for --source and --format are exactly the ones accepted,
// in the same order, and that the environment variable it names is the one
// read.
func TestEnumeratedValuesMatchTheConfiguration(t *testing.T) {
	for _, f := range reference.Root().Flags {
		if len(f.Values) == 0 {
			continue
		}
		last := len(f.Values) - 1
		want := "(want " + strings.Join(f.Values[:last], ", ") + " or " + f.Values[last] + ")"
		t.Run(f.Name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			never := WithBackend(unreadBackend{t})
			code := Run(context.Background(), []string{"--" + f.Name, "bogus"}, &out, &errOut, BuildInfo{}, WithEnv(config.Env{}), never)
			if code != exitUsage || !strings.Contains(errOut.String(), want) {
				t.Errorf("--%s bogus: exit %d, stderr %q; want a usage error with %q", f.Name, code, errOut.String(), want)
			}
			env := config.Env{Getenv: func(key string) string {
				if key == f.Env {
					return "bogus"
				}
				return ""
			}}
			errOut.Reset()
			code = Run(context.Background(), nil, &out, &errOut, BuildInfo{}, WithEnv(env), never)
			if code != exitUsage || !strings.Contains(errOut.String(), f.Env) || !strings.Contains(errOut.String(), want) {
				t.Errorf("%s=bogus: exit %d, stderr %q; want a usage error naming %s with %q", f.Env, code, errOut.String(), f.Env, want)
			}
		})
	}
}

// unreadBackend fails the test when it is read: a usage error comes before
// any read.
type unreadBackend struct{ t *testing.T }

func (unreadBackend) Source() model.Source { return model.SourceMacOS }

func (b unreadBackend) Read(context.Context) (model.NowPlaying, error) {
	b.t.Error("the backend was read before the usage error")
	return model.NowPlaying{}, errors.New("not read in this test")
}
