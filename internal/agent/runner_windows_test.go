//go:build windows

package agent

import "testing"

func TestSourceTempDirBaseUsesWorkingDirectoryVolumeWhenDifferent(t *testing.T) {
	tests := []struct {
		name      string
		cwd       string
		systemTmp string
		want      string
	}{
		{
			name:      "different drives",
			cwd:       `D:\work\project`,
			systemTmp: `C:\Users\fixture\Temp`,
			want:      `D:\work\project`,
		},
		{
			name:      "same drive ignoring case",
			cwd:       `c:\work\project`,
			systemTmp: `C:\Users\fixture\Temp`,
			want:      "",
		},
		{
			name:      "missing cwd volume",
			cwd:       `\work\project`,
			systemTmp: `C:\Users\fixture\Temp`,
			want:      "",
		},
		{
			name:      "missing temp volume",
			cwd:       `D:\work\project`,
			systemTmp: `\Temp`,
			want:      "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sourceTempDirBase(test.cwd, test.systemTmp); got != test.want {
				t.Fatalf("sourceTempDirBase(%q, %q) = %q, want %q", test.cwd, test.systemTmp, got, test.want)
			}
		})
	}
}
