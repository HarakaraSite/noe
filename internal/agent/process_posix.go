//go:build !windows

package agent

func sameEnvironmentKey(got, want string) bool { return got == want }
