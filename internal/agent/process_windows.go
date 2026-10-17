//go:build windows

package agent

import "strings"

func sameEnvironmentKey(got, want string) bool { return strings.EqualFold(got, want) }
