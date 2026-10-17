package highlight

// Theme selects the fixed syntax and diff palette used by a highlighter. The
// zero value is the dark theme for compatibility with callers that omit it.
type Theme uint8

const (
	ThemeDark Theme = iota
	ThemeLight
)

// palette is copied into each stateful highlighter so a stream cannot change
// colors midway through a multiline token or diff.
type palette struct {
	comment           string
	keyword           string
	string            string
	number            string
	builtin           string
	function          string
	added             string
	removed           string
	addedBackground   string
	removedBackground string
}

// Dark defaults retained as constants for compatibility within the package.
// The scanners themselves always read their state-carried palette.
const (
	styleComment  = "\x1b[38;2;129;184;139m"
	styleKeyword  = "\x1b[38;2;101;169;224m"
	styleString   = "\x1b[38;2;206;145;120m"
	styleNumber   = "\x1b[38;2;181;206;168m"
	styleBuiltin  = "\x1b[38;2;78;201;176m"
	styleFunction = "\x1b[38;2;220;220;170m"
	styleAdded    = "\x1b[38;2;137;209;133m"
	styleRemoved  = "\x1b[38;2;244;135;113m"
	bgAdded       = "\x1b[48;2;33;58;43m"
	bgRemoved     = "\x1b[48;2;74;34;29m"
)

func paletteFor(theme Theme) palette {
	if theme == ThemeLight {
		return palette{
			comment:           "\x1b[38;2;0;119;0m",
			keyword:           "\x1b[38;2;0;0;255m",
			string:            "\x1b[38;2;163;21;21m",
			number:            "\x1b[38;2;8;122;80m",
			builtin:           "\x1b[38;2;35;117;141m",
			function:          "\x1b[38;2;121;94;38m",
			added:             "\x1b[38;2;0;119;0m",
			removed:           "\x1b[38;2;163;21;21m",
			addedBackground:   "\x1b[48;2;218;251;225m",
			removedBackground: "\x1b[48;2;255;235;233m",
		}
	}
	return palette{
		comment:           styleComment,
		keyword:           styleKeyword,
		string:            styleString,
		number:            styleNumber,
		builtin:           styleBuiltin,
		function:          styleFunction,
		added:             styleAdded,
		removed:           styleRemoved,
		addedBackground:   bgAdded,
		removedBackground: bgRemoved,
	}
}
