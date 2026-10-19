package cli

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestObservationWidthColumnsAndNonTerminalFallback(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, output := range []struct {
		name   string
		writer io.Writer
	}{
		{"buffer", &bytes.Buffer{}},
		{"file", file},
	} {
		t.Run(output.name, func(t *testing.T) {
			for _, tc := range []struct {
				columns string
				want    int
			}{
				{"73", 73},
				{"", 80},
				{"invalid", 80},
				{"0", 80},
				{"-1", 80},
			} {
				t.Run("columns="+tc.columns, func(t *testing.T) {
					t.Setenv("COLUMNS", tc.columns)
					if got := observationWidth(output.writer); got != tc.want {
						t.Fatalf("width = %d, want %d", got, tc.want)
					}
				})
			}
		})
	}
}
