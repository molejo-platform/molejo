package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	var address string
	var caPath string
	var idleDuration time.Duration
	var targetURL string
	flag.StringVar(&address, "address", "", "TCP address used to reach the Gateway")
	flag.StringVar(&caPath, "ca", "", "PEM CA used to validate the Gateway certificate")
	flag.DurationVar(&idleDuration, "idle-duration", 0, "time to keep the WebSocket idle before the second message")
	flag.StringVar(&targetURL, "url", "", "public wss URL used for SNI and the Host header")
	flag.Parse()
	if address == "" || caPath == "" || targetURL == "" {
		fmt.Fprintln(os.Stderr, "address, ca, and url are required")
		os.Exit(2)
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		fail("read CA", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		fail("parse CA", fmt.Errorf("no certificates found"))
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		NetDialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
		},
	}
	connection, response, err := dialer.Dial(targetURL, nil)
	if err != nil {
		if response != nil {
			fail("dial WebSocket", fmt.Errorf("HTTP %d: %w", response.StatusCode, err))
		}
		fail("dial WebSocket", err)
	}
	defer connection.Close()

	for index, message := range []string{"first", "second"} {
		if index > 0 && idleDuration > 0 {
			time.Sleep(idleDuration)
		}
		_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := connection.WriteJSON(map[string]string{"message": message}); err != nil {
			fail("write WebSocket message", err)
		}
		var payload struct {
			Message string `json:"message"`
			Version string `json:"version"`
		}
		if err := connection.ReadJSON(&payload); err != nil {
			fail("read WebSocket message", err)
		}
		if payload.Message != message || payload.Version == "" {
			fail("validate WebSocket response", fmt.Errorf("unexpected payload: %#v", payload))
		}
	}
}

func fail(action string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", action, err)
	os.Exit(1)
}
