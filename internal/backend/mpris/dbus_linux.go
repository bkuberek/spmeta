//go:build linux

package mpris

import (
	"context"
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"

	"github.com/bkuberek/spmeta/internal/backend"
)

const (
	playerPath  = "/org/mpris/MediaPlayer2"
	playerIface = "org.mpris.MediaPlayer2.Player"

	methodListNames = "org.freedesktop.DBus.ListNames"
	methodGetAll    = "org.freedesktop.DBus.Properties.GetAll"

	daemonName      = "org.freedesktop.DBus"
	daemonPath      = "/org/freedesktop/DBus"
	propertiesIface = "org.freedesktop.DBus.Properties"
	mprisNamespace  = "org.mpris.MediaPlayer2"

	// signalBuffer absorbs a burst without godbus starting a goroutine per
	// signal it cannot deliver at once.
	signalBuffer = 16
)

// signalRules match the signals that announce a player change: a property of
// the Player interface changing on the MPRIS object path, and an MPRIS bus
// name gaining or losing its owner (a player starting or quitting).
var signalRules = [][]dbus.MatchOption{
	{
		dbus.WithMatchObjectPath(playerPath),
		dbus.WithMatchInterface(propertiesIface),
		dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchArg(0, playerIface),
	},
	{
		dbus.WithMatchSender(daemonName),
		dbus.WithMatchObjectPath(daemonPath),
		dbus.WithMatchInterface(daemonName),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg0Namespace(mprisNamespace),
	},
}

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

// Signals adds the signalRules to the bus and returns one coalesced wake per
// matching signal. The channel is registered first so no signal is lost
// between the rules and the registration. godbus closes it when the
// connection closes or drops, which closes the returned channel.
func (b *godbusBus) Signals(ctx context.Context) (<-chan struct{}, error) {
	ch := make(chan *dbus.Signal, signalBuffer)
	b.conn.Signal(ch)
	for _, rule := range signalRules {
		if err := b.conn.AddMatchSignalContext(ctx, rule...); err != nil {
			return nil, err
		}
	}
	return backend.Coalesce(ch, subscribed), nil
}

// subscribed reports whether sig is one of the signalRules members. godbus
// also hands over the signals the bus addresses to this connection, such as
// NameAcquired for its own unique name, whatever the rules say.
func subscribed(sig *dbus.Signal) bool {
	return sig.Name == propertiesIface+".PropertiesChanged" || sig.Name == daemonName+".NameOwnerChanged"
}

func (b *godbusBus) Close() error { return b.conn.Close() }
