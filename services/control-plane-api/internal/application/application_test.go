package application

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestAgentGRPCShutdownDoesNotWaitForStalledHandshake(t *testing.T) {
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &handshakeListener{Listener: baseListener, readStarted: make(chan struct{})}
	server := newAgentGRPCServer(20 * time.Millisecond)
	serveDone := make(chan struct{})
	go func() {
		_ = server.Serve(listener)
		close(serveDone)
	}()

	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	<-listener.readStarted

	started := time.Now()
	gracefulStopGRPC(server, 20*time.Millisecond)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("bounded gRPC stop took %s", elapsed)
	}
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("gRPC Serve did not stop")
	}
}

type handshakeListener struct {
	net.Listener
	readStarted chan struct{}
}

func (l *handshakeListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &readSignalConnection{Conn: connection, readStarted: l.readStarted}, nil
}

type readSignalConnection struct {
	net.Conn
	readStarted chan struct{}
	once        sync.Once
}

func (c *readSignalConnection) Read(buffer []byte) (int, error) {
	c.once.Do(func() { close(c.readStarted) })
	return c.Conn.Read(buffer)
}

func TestBootstrapWorkspaceUsesItsPublicIDAsNamespace(t *testing.T) {
	workspaceID := "ws-abcdefghijklmnopqrst"
	workspace := bootstrapWorkspace(workspaceID)

	if workspace.PublicID != workspaceID || workspace.Namespace != workspaceID {
		t.Fatalf("bootstrap workspace = %+v", workspace)
	}
}
