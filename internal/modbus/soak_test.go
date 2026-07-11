package modbus

// Soak/integration tests: a fake Modbus TCP slave over real TCP verifies
// that exception responses keep the session alive (only transport errors
// drop it) and that the client survives repeated connection drops.

import (
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// fakeModbusServer implements a minimal Modbus TCP slave: FC3 (read holding
// registers) with a configurable "unmapped" region that returns exception
// 0x02 ILLEGAL DATA ADDRESS.
type fakeModbusServer struct {
	ln net.Listener

	// exceptionFrom: register addresses >= this value answer exception 0x02.
	exceptionFrom atomic.Int64
	// dropAfterRequests > 0 closes the connection after N requests.
	dropAfterRequests atomic.Int64
}

func newFakeModbusServer(t *testing.T) *fakeModbusServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeModbusServer{ln: ln}
	s.exceptionFrom.Store(1 << 20) // disabled by default
	go s.acceptLoop()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeModbusServer) hostPort() (string, int) {
	a := s.ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func (s *fakeModbusServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func (s *fakeModbusServer) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	served := int64(0)
	for {
		// MBAP header: txid(2) proto(2) length(2) unit(1)
		header := make([]byte, 7)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint16(header[4:6]))
		if length < 2 || length > 260 {
			return
		}
		pdu := make([]byte, length-1)
		if _, err := io.ReadFull(conn, pdu); err != nil {
			return
		}

		fc := pdu[0]
		var resp []byte
		if fc == 0x03 && len(pdu) >= 5 {
			addr := binary.BigEndian.Uint16(pdu[1:3])
			qty := binary.BigEndian.Uint16(pdu[3:5])
			if int64(addr) >= s.exceptionFrom.Load() {
				resp = []byte{fc | 0x80, 0x02} // ILLEGAL DATA ADDRESS
			} else {
				resp = make([]byte, 2+2*int(qty))
				resp[0], resp[1] = fc, byte(2*qty)
				for i := 0; i < int(qty); i++ {
					// Register value = its own address (easy to assert on)
					binary.BigEndian.PutUint16(resp[2+2*i:], addr+uint16(i))
				}
			}
		} else {
			resp = []byte{fc | 0x80, 0x01} // ILLEGAL FUNCTION
		}

		out := make([]byte, 7+len(resp))
		copy(out, header[:4])
		binary.BigEndian.PutUint16(out[4:6], uint16(1+len(resp)))
		out[6] = header[6]
		copy(out[7:], resp)
		if _, err := conn.Write(out); err != nil {
			return
		}

		served++
		if max := s.dropAfterRequests.Load(); max > 0 && served >= max {
			return
		}
	}
}

func newTestModbusClient(t *testing.T, srv *fakeModbusServer) *Client {
	host, port := srv.hostPort()
	c := NewClient(Config{
		Host: host, Port: port, SlaveID: 1,
		Timeout: 2 * time.Second,
	})
	if err := c.Connect(); err != nil {
		t.Fatalf("connect to fake slave: %v", err)
	}
	t.Cleanup(func() { _ = c.Disconnect() })
	return c
}

// TestExceptionResponseKeepsConnection verifies the fix for the
// reconnect-churn bug: a deterministic Modbus exception (unmapped register)
// must be distinguishable from a transport error, and the SAME session must
// keep working for subsequent good reads.
func TestExceptionResponseKeepsConnection(t *testing.T) {
	srv := newFakeModbusServer(t)
	srv.exceptionFrom.Store(100)
	c := newTestModbusClient(t, srv)

	// Good read first.
	if _, err := c.ReadHoldingRegisters(10, 2); err != nil {
		t.Fatalf("good read failed: %v", err)
	}

	// Unmapped register → exception, recognizable as such.
	_, err := c.ReadHoldingRegisters(150, 1)
	if err == nil {
		t.Fatal("expected exception error for unmapped register")
	}
	if !IsExceptionError(err) {
		t.Fatalf("error should be classified as Modbus exception, got: %v", err)
	}

	// The session must still be usable without any reconnect.
	data, err := c.ReadHoldingRegisters(20, 1)
	if err != nil {
		t.Fatalf("read after exception failed (session should survive): %v", err)
	}
	if got := binary.BigEndian.Uint16(data); got != 20 {
		t.Errorf("register 20 = %d, want 20", got)
	}
}

// TestTransportErrorIsNotException verifies a dropped connection is NOT
// classified as a Modbus exception (the driver disconnects only on these).
func TestTransportErrorIsNotException(t *testing.T) {
	srv := newFakeModbusServer(t)
	srv.dropAfterRequests.Store(1)
	c := newTestModbusClient(t, srv)

	if _, err := c.ReadHoldingRegisters(1, 1); err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	// Server closed the session: next read is a transport failure.
	_, err := c.ReadHoldingRegisters(1, 1)
	if err == nil {
		t.Skip("client library transparently reconnected — nothing to assert")
	}
	if IsExceptionError(err) {
		t.Errorf("transport error misclassified as Modbus exception: %v", err)
	}
}

// TestSoak_ReconnectAfterRepeatedDrops runs 20 connect→read→server-drop
// cycles, mirroring the driver poll loop's disconnect/reconnect path.
func TestSoak_ReconnectAfterRepeatedDrops(t *testing.T) {
	srv := newFakeModbusServer(t)
	srv.dropAfterRequests.Store(1)
	c := newTestModbusClient(t, srv)

	success := 0
	for i := 0; i < 20; i++ {
		data, err := c.ReadHoldingRegisters(uint16(i), 1)
		if err == nil && binary.BigEndian.Uint16(data) == uint16(i) {
			success++
		}
		// The server drops after each request; emulate the driver's recovery.
		_ = c.Disconnect()
		if err := c.Connect(); err != nil {
			t.Fatalf("cycle %d: reconnect failed: %v", i, err)
		}
	}
	if success < 18 {
		t.Errorf("only %d/20 reads succeeded across reconnect cycles", success)
	}
}
