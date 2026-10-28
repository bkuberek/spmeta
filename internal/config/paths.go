package config

import "path/filepath"

const (
	appDir         = "spmeta"
	configFileName = "config.toml"
	tokenFileName  = "token.json"
)

// configDir returns the directory holding config.toml and token.json:
// $XDG_CONFIG_HOME/spmeta when that variable is an absolute path (the XDG
// specification says to ignore relative values), else ~/.config/spmeta. It
// is empty when neither base is known.
func configDir(env Env) string {
	if xdg := env.getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, appDir)
	}
	if env.Home != "" {
		return filepath.Join(env.Home, ".config", appDir)
	}
	return ""
}

// configPath picks the configuration file: --config, SPMETA_CONFIG, then the
// default location. origin names the flag or variable that chose an explicit
// path and is empty for the default location. path is empty when no default
// location is known.
func configPath(flagPath *string, env Env) (path, origin string) {
	if flagPath != nil && *flagPath != "" {
		return *flagPath, "--config"
	}
	if p := env.getenv("SPMETA_CONFIG"); p != "" {
		return p, "SPMETA_CONFIG"
	}
	if dir := configDir(env); dir != "" {
		return filepath.Join(dir, configFileName), ""
	}
	return "", ""
}

// tokenPath lives beside the default configuration file; --config and
// SPMETA_CONFIG do not move it.
func tokenPath(env Env) string {
	if dir := configDir(env); dir != "" {
		return filepath.Join(dir, tokenFileName)
	}
	return ""
}
