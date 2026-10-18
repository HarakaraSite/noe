// Package agent owns the Go side of the built-in agent module. Its HTTP
// broker is created on the first internal RPC, so importing the public module
// does not require credentials or open a listener.
package agent
