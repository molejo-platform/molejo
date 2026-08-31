package main

import (
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
)

func TestGracefulStopGRPCDoesNotWaitForeverForOpenConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
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
