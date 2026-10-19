package cli

import "golang.org/x/sys/windows"

// terminalWidth uses the visible console window, rather than its scrollback buffer.
func terminalWidth(fd uintptr) int {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(fd), &info); err != nil {
		return 0
	}
	return int(info.Window.Right) - int(info.Window.Left) + 1
}
