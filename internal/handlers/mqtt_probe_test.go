package handlers

import (
	"context"
	"net"
	"testing"
)

// fakeBroker accepts one connection and answers the CONNECT with a CONNACK
// carrying the given return code (0 accepted, 5 not authorized).
func fakeBroker(t *testing.T, code byte) int {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 512)
				if _, err := c.Read(buf); err != nil {
					return
				}
				_, _ = c.Write([]byte{0x20, 0x02, 0x00, code})
				_, _ = c.Read(buf) // wait for DISCONNECT or close
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestTheBrokerProbeSaysWhyItFailed(t *testing.T) {
	ctx := context.Background()

	ok := probeBroker(ctx, MQTTProbeRequest{Target: "cloud", Host: "127.0.0.1", Port: fakeBroker(t, 0)})
	if !ok.OK || ok.Scheme != "tcp" {
		t.Errorf("an accepting broker: %+v", ok)
	}

	auth := probeBroker(ctx, MQTTProbeRequest{Target: "cloud", Host: "127.0.0.1", Port: fakeBroker(t, 5),
		Username: "u", Password: "wrong"})
	if auth.OK || auth.Code != "auth" {
		t.Errorf("a refused password: %+v", auth)
	}

	// A port nobody listens on.
	ln, _ := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	refused := probeBroker(ctx, MQTTProbeRequest{Target: "cloud", Host: "127.0.0.1", Port: port})
	if refused.OK || refused.Code != "refused" {
		t.Errorf("a closed port: %+v", refused)
	}
}
