package cli

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestObservationWidthUsesTerminalDimensions(t *testing.T) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	terminal := os.NewFile(uintptr(fd), "width-test-pty")
	defer terminal.Close()
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 31, Col: 113}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		columns string
		want    int
	}{
		{"", 113},
		{"invalid", 113},
		{"0", 113},
		{"19", 19},
	} {
		t.Run("columns="+tc.columns, func(t *testing.T) {
			t.Setenv("COLUMNS", tc.columns)
			if got := observationWidth(terminal); got != tc.want {
				t.Fatalf("width = %d, want %d", got, tc.want)
			}
		})
	}
	t.Setenv("COLUMNS", "")
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{}); err != nil {
		t.Fatal(err)
	}
	if got := observationWidth(terminal); got != 80 {
		t.Fatalf("zero terminal width fallback = %d, want 80", got)
	}
}
