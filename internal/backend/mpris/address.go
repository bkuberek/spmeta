package mpris

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

const (
	envSessionBus = "DBUS_SESSION_BUS_ADDRESS"
	envRuntimeDir = "XDG_RUNTIME_DIR"
	autolaunch    = "autolaunch:"
)

// sessionBusAddress finds the session bus without ever asking godbus to
// autolaunch one: the environment variable first, then the systemd-style
// socket in the runtime directory.
func sessionBusAddress(getenv func(string) string, isSocket func(path string) bool) (string, error) {
	if addr := getenv(envSessionBus); addr != "" && !strings.HasPrefix(addr, autolaunch) {
		return addr, nil
	}
	if dir := getenv(envRuntimeDir); dir != "" {
		if sock := filepath.Join(dir, "bus"); isSocket(sock) {
			return "unix:path=" + escapeAddressValue(sock), nil
		}
	}
	return "", &backend.Error{
		Kind:    backend.KindNotAvailable,
		Source:  model.SourceMPRIS,
		Message: "no D-Bus session bus found",
		Hint:    hintNoBus,
	}
}

// escapeAddressValue percent-encodes every byte that the D-Bus address syntax
// does not allow unescaped.
func escapeAddressValue(s string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '/', c == '.', c == '\\':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
