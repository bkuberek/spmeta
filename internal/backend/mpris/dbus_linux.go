//go:build linux

package mpris

import (
	"context"
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

const (
	playerPath  = "/org/mpris/MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"

	methodListNames = "org.freedesktop.DBus.ListNames"
	methodGetAll    = "org.freedesktop.DBus.Properties.GetAll"
)

// Dial connects to the session bus. It resolves the address itself, so it
// never starts a bus with dbus-launch. The connection lives until Close or
// until ctx ends.
func Dial(ctx context.Context) (Bus, error) {
	addr, err := sessionBusAddress(os.Getenv, isSocket)
	if err != nil {
		return nil, err
	}
	conn, err := dbus.Connect(addr, dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("connect to session bus: %w", err)
	}
	return &godbusBus{conn: conn}, nil
}

func isSocket(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}

type godbusBus struct {
	conn *dbus.Conn
}

func (b *godbusBus) ListNames(ctx context.Context) ([]string, error) {
	var names []string
	err := b.conn.BusObject().CallWithContext(ctx, methodListNames, 0).Store(&names)
	return names, err
}

func (b *godbusBus) GetAll(ctx context.Context, busName string) (Properties, error) {
	var raw map[string]dbus.Variant
	err := b.conn.Object(busName, playerPath).
		CallWithContext(ctx, methodGetAll, 0, playerIface).
		Store(&raw)
	if err != nil {
		return nil, err
	}
	return unwrapProperties(raw), nil
}

func (b *godbusBus) Close() error { return b.conn.Close() }
