package auth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the user's browser with `open` on macOS or
// `xdg-open` on Linux. It does not wait for the browser, and other platforms
// get an error, which Login ignores because it prints the URL as well.
func OpenBrowser(url string) error {
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "linux":
		name = "xdg-open"
	default:
		return fmt.Errorf("no browser opener for %s", runtime.GOOS)
	}
	cmd := exec.Command(name, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
