package stdlib

import (
	"bufio"
	"net"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
)

// A child using the parent's filesystem and accepting WebSockets must retain
// the backend policy, but must not replace the parent's server upgrade callback.
func TestWorkerConfigKeepsBackendAndIsolatesServerBinding(t *testing.T) {
	backend, err := filesystem.New(filesystem.Config{Root: t.TempDir(), Cwd: "/", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	parentLoop, childLoop := &Loop{}, &Loop{}
	parentUpgraded := false
	server := &Serve{
		Loop: parentLoop,
		upgrade: func(string, net.Conn, *bufio.Reader, string) {
			parentUpgraded = true
		},
	}
	parent := Config{
		Loop:    parentLoop,
		FS:      &FS{Loop: parentLoop, Backend: backend},
		Serve:   server,
		Sockets: &WebSockets{Loop: parentLoop, Serve: server},
	}
	child := parent.forWorker(&workerContext{}, "./child.mjs", workerOptions{})
	if child.Loop != nil || parent.Loop != parentLoop {
		t.Fatal("child inherited the parent loop or changed its Config")
	}
	child.bindWorkerLoop(childLoop)
	if child.Loop != childLoop || child.FS == parent.FS || child.FS.Loop != childLoop || parent.FS.Loop != parentLoop {
		t.Fatal("filesystem settings were not copied and rebound independently")
	}
	if child.FS.Backend != backend {
		t.Fatal("child replaced the shared filesystem backend and its policy")
	}
	if child.Serve == server || child.Serve.Loop != childLoop || child.Serve.upgrade != nil {
		t.Fatal("child server retained the parent's runtime binding")
	}
	if child.Sockets == parent.Sockets || child.Sockets.Loop != childLoop || child.Sockets.Serve != child.Serve {
		t.Fatal("child sockets were not rebound to the child's server")
	}
	// Sockets installation writes this field. The same write in the child
	// must leave the parent's previously installed runtime callback intact.
	child.Serve.upgrade = func(string, net.Conn, *bufio.Reader, string) {
		t.Fatal("parent called the child's upgrade callback")
	}
	parent.Serve.upgrade("", nil, nil, "")
	if !parentUpgraded || parent.Serve.Loop != parentLoop || parent.Sockets.Serve != server || parent.Sockets.Loop != parentLoop {
		t.Fatal("child rebinding changed the parent's server/socket configuration")
	}
}
