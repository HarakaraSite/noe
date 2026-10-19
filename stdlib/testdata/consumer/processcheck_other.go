//go:build !linux

package main

func processIsAlive(int) bool { return false }
