// Package config merges flags, SPMETA_* environment variables, the optional
// config.toml and built-in defaults into one Settings value.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultSource      = "auto"
	defaultFormat      = "text"
	defaultWebInterval = 3 * time.Second

	minLocalInterval = 0.5
	minWebInterval   = 1.0
	maxIntervalSecs  = float64(math.MaxInt64 / int64(time.Second))

	flagInterval = "--interval"
)

var (
	sources = []string{"auto", "local", "web"}
	formats = []string{"text", "json", "template", "waybar"}
)

// Settings is the resolved configuration for one run.
type Settings struct {
	Source string // auto | local | web
	Format string // text | json | template | waybar

	Template       string
	TemplateOrigin string // where Template came from, for error messages; empty when unset

	Player      string
	ClientID    string
	Interval    time.Duration // zero means "use the source default"
	WebInterval time.Duration

	ConfigPath string // empty when no location is known
	TokenPath  string // independent of --config; empty when no location is known

	Debug bool
	Quiet bool
	Watch bool

	// Warnings are non-fatal findings, such as unknown configuration keys.
	Warnings []string
}

// Flags holds the command-line values. A nil pointer means the flag was not
// given, which is what lets a lower layer apply.
type Flags struct {
	Source   *string
	Format   *string
	Template *string
	Player   *string
	ClientID *string
	Config   *string
	Interval *float64 // seconds

	Watch bool
	Quiet bool
	Debug bool
}

// Env is everything Load reads from the outside world, so tests stay away
// from the real environment and home directory. The zero value is an empty
// environment with no home directory and no files.
type Env struct {
	Getenv   func(string) string
	Home     string
	ReadFile func(string) ([]byte, error)
}

// OSEnv returns the real process environment, home directory and filesystem.
func OSEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Getenv: os.Getenv, Home: home, ReadFile: os.ReadFile}
}

func (e Env) getenv(key string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(key)
}

func (e Env) readFile(path string) ([]byte, error) {
	if e.ReadFile == nil {
		return nil, fs.ErrNotExist
	}
	return e.ReadFile(path)
}

// Load resolves Settings with the precedence flags > environment > file >
// defaults. Every error it returns is a usage error: it names the flag,
// variable, or file and key at fault. On error the Settings are empty except
// for TokenPath, which depends only on the environment, so doctor can still
// check the saved login.
func Load(flags Flags, env Env) (Settings, error) {
	s, err := load(flags, env)
	if err != nil {
		return Settings{TokenPath: tokenPath(env)}, err
	}
	return s, nil
}

func load(flags Flags, env Env) (Settings, error) {
	path, origin := configPath(flags.Config, env)
	fromFlags, err := flagLayer(flags)
	if err != nil {
		return Settings{}, err
	}
	fromEnv, err := envLayer(env)
	if err != nil {
		return Settings{}, err
	}
	fromFile, warnings, err := fileLayer(path, origin, env)
	if err != nil {
		return Settings{}, err
	}

	s := Settings{
		ConfigPath: path,
		TokenPath:  tokenPath(env),
		Debug:      flags.Debug,
		Quiet:      flags.Quiet,
		Watch:      flags.Watch,
		Warnings:   warnings,
	}
	s.Source, _ = pick(defaultSource, fromFlags.source, fromEnv.source, fromFile.source)
	s.Format, _ = pick(defaultFormat, fromFlags.format, fromEnv.format, fromFile.format)
	s.Template, s.TemplateOrigin = pick("", fromFlags.template, fromEnv.template, fromFile.template)
	s.Player, _ = pick("", fromFlags.player, fromEnv.player, fromFile.player)
	s.ClientID, _ = pick("", fromFlags.clientID, fromEnv.clientID, fromFile.clientID)
	s.Interval, _ = pick(0, fromFlags.interval, fromFile.interval)
	s.WebInterval, _ = pick(defaultWebInterval, fromFile.webInterval)

	if s.Format == "template" && s.Template == "" {
		return Settings{}, errors.New(`--format template needs a template: set --template, SPMETA_TEMPLATE or "template" in the config file`)
	}
	if fromFlags.interval.ok {
		minimum := minLocalInterval
		if s.Source == "web" {
			minimum = minWebInterval
		}
		if fromFlags.interval.v.Seconds() < minimum {
			return Settings{}, fmt.Errorf("invalid value %s for %s: the minimum for source %s is %g seconds",
				fromFlags.interval.v, flagInterval, s.Source, minimum)
		}
	}
	return s, nil
}

// value is one layer's contribution to a key.
type value[T any] struct {
	v      T
	origin string
	ok     bool
}

func some[T any](v T, origin string) value[T] { return value[T]{v: v, origin: origin, ok: true} }

// pick returns the first set value, else def, with the origin that supplied it.
func pick[T any](def T, layers ...value[T]) (T, string) {
	for _, l := range layers {
		if l.ok {
			return l.v, l.origin
		}
	}
	return def, ""
}

// layer is the validated contribution of one source of configuration.
type layer struct {
	source, format, template, player, clientID value[string]
	interval, webInterval                      value[time.Duration]
}

func flagLayer(f Flags) (layer, error) {
	var l layer
	if f.Quiet && f.Watch {
		return l, errors.New("--quiet cannot be combined with --watch")
	}
	var err error
	if l.source, err = enumValue(f.Source, "--source", sources); err != nil {
		return l, err
	}
	if l.format, err = enumValue(f.Format, "--format", formats); err != nil {
		return l, err
	}
	l.template = optional(f.Template, "--template")
	l.player = optional(f.Player, "--player")
	l.clientID = optional(f.ClientID, "--client-id")
	if l.template.ok && !l.format.ok {
		l.format = some("template", "--template")
	}
	if f.Interval != nil {
		if !f.Watch {
			return l, fmt.Errorf("%s requires --watch", flagInterval)
		}
		d, ok := duration(*f.Interval)
		if !ok {
			return l, fmt.Errorf("invalid value %g for %s: want a finite number of seconds", *f.Interval, flagInterval)
		}
		l.interval = some(d, flagInterval)
	}
	return l, nil
}

func envLayer(env Env) (layer, error) {
	var l layer
	var err error
	if l.source, err = enumValue(envPtr(env, "SPMETA_SOURCE"), "SPMETA_SOURCE", sources); err != nil {
		return l, err
	}
	if l.format, err = enumValue(envPtr(env, "SPMETA_FORMAT"), "SPMETA_FORMAT", formats); err != nil {
		return l, err
	}
	l.template = optional(envPtr(env, "SPMETA_TEMPLATE"), "SPMETA_TEMPLATE")
	l.player = optional(envPtr(env, "SPMETA_PLAYER"), "SPMETA_PLAYER")
	l.clientID = optional(envPtr(env, "SPMETA_CLIENT_ID"), "SPMETA_CLIENT_ID")
	return l, nil
}

// envPtr returns the variable's value, or nil when it is unset or empty.
func envPtr(env Env, key string) *string {
	if v := env.getenv(key); v != "" {
		return &v
	}
	return nil
}

func optional(v *string, origin string) value[string] {
	if v == nil {
		return value[string]{}
	}
	return some(*v, origin)
}

func enumValue(v *string, origin string, allowed []string) (value[string], error) {
	if v == nil {
		return value[string]{}, nil
	}
	if !slices.Contains(allowed, *v) {
		return value[string]{}, fmt.Errorf("invalid value %q for %s (want %s)", *v, origin, wantList(allowed))
	}
	return some(*v, origin), nil
}

func wantList(allowed []string) string {
	last := len(allowed) - 1
	return strings.Join(allowed[:last], ", ") + " or " + allowed[last]
}

// duration converts seconds to a Duration, rejecting values that are not
// finite, not positive, or too large to represent.
func duration(seconds float64) (time.Duration, bool) {
	if math.IsNaN(seconds) || seconds <= 0 || seconds > maxIntervalSecs {
		return 0, false
	}
	return time.Duration(math.Round(seconds * float64(time.Second))), true
}

// fileLayer reads the configuration file. A missing file at the default
// location is not an error, but a missing file named explicitly by origin
// (--config or SPMETA_CONFIG) is, so a mistyped path is never silently
// ignored. Unknown top-level keys become warnings and are otherwise ignored.
func fileLayer(path, origin string, env Env) (l layer, warnings []string, err error) {
	if path == "" {
		return l, nil, nil
	}
	data, err := env.readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if origin != "" {
			return l, nil, fmt.Errorf("config file %s (from %s) does not exist", path, origin)
		}
		return l, nil, nil
	}
	if err != nil {
		return l, nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return l, nil, fmt.Errorf("config file %s is not valid TOML: %w", path, err)
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		origin := key + " in " + path
		raw := raw[key]
		switch key {
		case "client_id":
			l.clientID, err = fileString(raw, origin)
		case "default_source":
			l.source, err = fileEnum(raw, origin, sources)
		case "default_format":
			l.format, err = fileEnum(raw, origin, formats)
		case "template":
			l.template, err = fileString(raw, origin)
		case "player":
			l.player, err = fileString(raw, origin)
		case "interval":
			l.interval, err = fileInterval(raw, origin, minLocalInterval)
		case "web_interval":
			l.webInterval, err = fileInterval(raw, origin, minWebInterval)
		default:
			warnings = append(warnings, fmt.Sprintf("unknown key %q in %s (ignored)", key, path))
		}
		if err != nil {
			return layer{}, nil, err
		}
	}
	return l, warnings, nil
}

func fileString(raw any, origin string) (value[string], error) {
	s, ok := raw.(string)
	if !ok {
		return value[string]{}, fmt.Errorf("invalid value %v for %s: want a string", raw, origin)
	}
	return some(s, origin), nil
}

func fileEnum(raw any, origin string, allowed []string) (value[string], error) {
	s, err := fileString(raw, origin)
	if err != nil {
		return s, err
	}
	return enumValue(&s.v, origin, allowed)
}

func fileInterval(raw any, origin string, minimum float64) (value[time.Duration], error) {
	var seconds float64
	switch n := raw.(type) {
	case float64:
		seconds = n
	case int64:
		seconds = float64(n)
	default:
		return value[time.Duration]{}, fmt.Errorf("invalid value %v for %s: want a number of seconds", raw, origin)
	}
	d, ok := duration(seconds)
	if !ok || seconds < minimum {
		return value[time.Duration]{}, fmt.Errorf("invalid value %v for %s: want a finite number of seconds, at least %g", raw, origin, minimum)
	}
	return some(d, origin), nil
}
