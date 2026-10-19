//go:build darwin || linux

package cli

import "golang.org/x/sys/unix"

// terminalWidth returns zero when fd has no readable terminal dimensions.
func terminalWidth(fd uintptr) int {
	size, err := unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(size.Col)
}
