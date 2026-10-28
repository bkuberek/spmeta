// Package local selects the backend that reads the Spotify client running on
// this machine. Each supported operating system provides New in a
// build-constrained file.
package local

// Settings are the user options a local backend honors.
type Settings struct {
	// Player names the MPRIS player to read on Linux. Other systems ignore it.
	Player string
}
