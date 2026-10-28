package mpris

import (
	"errors"
	"testing"

	"github.com/bkuberek/spmeta/internal/backend"
	"github.com/bkuberek/spmeta/internal/model"
)

func TestSessionBusAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		env     map[string]string
		sockets []string
		want    string
	}{
		{
			name: "env address wins",
			env: map[string]string{
				"DBUS_SESSION_BUS_ADDRESS": "unix:path=/tmp/dbus-abc",
				"XDG_RUNTIME_DIR":          "/run/user/1000",
			},
			sockets: []string{"/run/user/1000/bus"},
			want:    "unix:path=/tmp/dbus-abc",
		},
		{
			name: "env address is returned verbatim",
			env:  map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:abstract=/tmp/dbus-xyz,guid=0123"},
			want: "unix:abstract=/tmp/dbus-xyz,guid=0123",
		},
		{
			name:    "runtime dir socket when env is empty",
			env:     map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			sockets: []string{"/run/user/1000/bus"},
			want:    "unix:path=/run/user/1000/bus",
		},
		{
			name: "autolaunch falls back to the runtime dir socket",
			env: map[string]string{
				"DBUS_SESSION_BUS_ADDRESS": "autolaunch:",
				"XDG_RUNTIME_DIR":          "/run/user/1000",
			},
			sockets: []string{"/run/user/1000/bus"},
			want:    "unix:path=/run/user/1000/bus",
		},
		{
			name:    "autolaunch with options is still autolaunch",
			env:     map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:scope=*user", "XDG_RUNTIME_DIR": "/run/user/7"},
			sockets: []string{"/run/user/7/bus"},
			want:    "unix:path=/run/user/7/bus",
		},
		{
			name:    "special characters in the path are escaped",
			env:     map[string]string{"XDG_RUNTIME_DIR": "/run/user/a b,c=d"},
			sockets: []string{"/run/user/a b,c=d/bus"},
			want:    "unix:path=/run/user/a%20b%2Cc%3Dd/bus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := sessionBusAddress(getenvFrom(tt.env), socketsFrom(tt.sockets))
			if err != nil {
				t.Fatalf("sessionBusAddress: %v", err)
			}
			if got != tt.want {
				t.Errorf("address = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionBusAddressNotAvailable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		env     map[string]string
		sockets []string
	}{
		{name: "nothing set"},
		{name: "autolaunch only", env: map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:"}},
		{name: "runtime dir without a bus socket", env: map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}},
		{
			name:    "bus path exists but is not under the runtime dir",
			env:     map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			sockets: []string{"/run/user/1001/bus"},
		},
		{name: "socket without a runtime dir", sockets: []string{"/bus"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := sessionBusAddress(getenvFrom(tt.env), socketsFrom(tt.sockets))
			if got != "" {
				t.Errorf("address = %q, want empty", got)
			}
			var be *backend.Error
			if !errors.As(err, &be) {
				t.Fatalf("error = %v, want *backend.Error", err)
			}
			if be.Kind != backend.KindNotAvailable || be.Source != model.SourceMPRIS {
				t.Errorf("kind = %v, source = %q, want not available from mpris", be.Kind, be.Source)
			}
			if be.Message != "no D-Bus session bus found" || be.Hint != hintNoBus {
				t.Errorf("message = %q, hint = %q", be.Message, be.Hint)
			}
		})
	}
}

func TestSessionBusAddressEnvSkipsSocketCheck(t *testing.T) {
	t.Parallel()
	env := map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/x", "XDG_RUNTIME_DIR": "/run/user/1"}
	_, err := sessionBusAddress(getenvFrom(env), func(string) bool {
		t.Error("socket check ran although the env address was usable")
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
}

func getenvFrom(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func socketsFrom(paths []string) func(string) bool {
	return func(p string) bool {
		for _, s := range paths {
			if s == p {
				return true
			}
		}
		return false
	}
}
