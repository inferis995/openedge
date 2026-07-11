package s7

// Soak/integration tests: a fake S7 PLC over real TCP exercises the
// connection lifecycle the way a flaky network does — split TPKT frames,
// abrupt disconnects, protocol garbage — to verify the client recovers
// without a process restart.

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// fakeS7Server speaks just enough ISO-on-TCP/S7comm for Client:
// COTP connect confirm, setup-communication ack, and read-var ack-data.
type fakeS7Server struct {
	ln net.Listener
	t  *testing.T

	// lastBitAddress records the 3-byte bit address of the last read request.
	lastBitAddress atomic.Int64

	// splitWrites makes the server deliver read responses in 3 fragments.
	splitWrites atomic.Bool
	// dropAfterReads > 0 closes the connection after that many reads.
	dropAfterReads atomic.Int64
	// garbageResponse makes the next read response start with a bad TPKT version.
	garbageResponse atomic.Bool
}

func newFakeS7Server(t *testing.T) *fakeS7Server {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeS7Server{ln: ln, t: t}
	go s.acceptLoop()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeS7Server) addr() (string, int) {
	tcpAddr := s.ln.Addr().(*net.TCPAddr)
	return tcpAddr.IP.String(), tcpAddr.Port
}

func (s *fakeS7Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func readFrame(conn net.Conn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[2:]))
	if length < 4 || length > 8192 {
		return nil, io.ErrUnexpectedEOF
	}
	frame := make([]byte, length)
	copy(frame, header)
	if _, err := io.ReadFull(conn, frame[4:]); err != nil {
		return nil, err
	}
	return frame, nil
}

func (s *fakeS7Server) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// 1. COTP connect request -> connect confirm (byte 5 = 0xD0)
	if _, err := readFrame(conn); err != nil {
		return
	}
	cc := []byte{tpktVersion, tpktReserved, 0x00, 0x16,
		0x11, cotpConnectConfirm, 0x00, 0x01, 0x00, 0x00, 0x00,
		0xC0, 0x01, 0x0A, 0xC1, 0x02, 0x01, 0x00, 0xC2, 0x02, 0x01, 0x02}
	if _, err := conn.Write(cc); err != nil {
		return
	}

	// 2. Setup communication -> ack ending with negotiated PDU length
	if _, err := readFrame(conn); err != nil {
		return
	}
	setup := make([]byte, 27)
	setup[0] = tpktVersion
	binary.BigEndian.PutUint16(setup[2:], 27)
	setup[4], setup[5], setup[6] = 0x02, cotpDataTransfer, 0x80
	setup[7], setup[8] = 0x32, s7PDUAckData
	binary.BigEndian.PutUint16(setup[25:], 480) // negotiated PDU size
	if _, err := conn.Write(setup); err != nil {
		return
	}

	// 3. Read-var requests
	reads := int64(0)
	for {
		req, err := readFrame(conn)
		if err != nil {
			return
		}
		payload, lengthBits := []byte{0x01}, 1 // default: single bit, LSB set
		if len(req) >= 31 {
			bitAddr := int64(req[28])<<16 | int64(req[29])<<8 | int64(req[30])
			s.lastBitAddress.Store(bitAddr)
			// Size the payload to the requested transport type.
			switch req[22] {
			case s7TransportWord:
				payload, lengthBits = []byte{0x00, 0x2A}, 16 // INT 42
			case s7TransportDWord, s7TransportReal:
				payload, lengthBits = []byte{0x00, 0x00, 0x00, 0x2A}, 32
			}
		}

		resp := s.buildReadResponse(payload, lengthBits)
		if s.garbageResponse.Load() {
			resp[0] = 0x42 // invalid TPKT version
			s.garbageResponse.Store(false)
		}

		if s.splitWrites.Load() {
			// Deliver the frame in 3 fragments with pauses, like a WAN link.
			for _, chunk := range [][]byte{resp[:2], resp[2:9], resp[9:]} {
				if _, err := conn.Write(chunk); err != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		} else {
			if _, err := conn.Write(resp); err != nil {
				return
			}
		}

		reads++
		if max := s.dropAfterReads.Load(); max > 0 && reads >= max {
			return // abrupt close mid-session
		}
	}
}

// buildReadResponse builds an S7 ack-data frame carrying payload.
func (s *fakeS7Server) buildReadResponse(payload []byte, lengthBits int) []byte {
	paramLen := 2 // function + item count
	dataLen := 4 + len(payload)
	total := 19 + paramLen + dataLen
	resp := make([]byte, total)
	resp[0] = tpktVersion
	binary.BigEndian.PutUint16(resp[2:], uint16(total))
	resp[4], resp[5], resp[6] = 0x02, cotpDataTransfer, 0x80
	resp[7], resp[8] = 0x32, s7PDUAckData
	binary.BigEndian.PutUint16(resp[13:], uint16(paramLen))
	binary.BigEndian.PutUint16(resp[15:], uint16(dataLen))
	resp[17], resp[18] = 0x00, 0x00 // error class/code
	resp[19], resp[20] = s7FunctionReadVar, 0x01
	dataStart := 19 + paramLen
	resp[dataStart] = 0xFF // success
	resp[dataStart+1] = 0x03
	binary.BigEndian.PutUint16(resp[dataStart+2:], uint16(lengthBits))
	copy(resp[dataStart+4:], payload)
	return resp
}

func newTestClient(t *testing.T, srv *fakeS7Server) *Client {
	ip, port := srv.addr()
	c := NewClient(Config{
		IP: ip, Port: port, Rack: 0, Slot: 2,
		ConnectRetry: 1, RetryInterval: 10 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	if err := c.Connect(); err != nil {
		t.Fatalf("connect to fake S7: %v", err)
	}
	t.Cleanup(c.Disconnect)
	return c
}

// TestReadTag_SplitTPKTFrame verifies that a response split across multiple
// TCP segments is reassembled instead of failing to parse (the pre-fix
// behavior desynced the stream permanently).
func TestReadTag_SplitTPKTFrame(t *testing.T) {
	srv := newFakeS7Server(t)
	srv.splitWrites.Store(true)
	c := newTestClient(t, srv)

	tv := c.ReadTag("DB1.DBX0.0", DataTypeBOOL)
	if tv.Error != nil {
		t.Fatalf("read over split frames failed: %v", tv.Error)
	}
	if tv.Value != true {
		t.Errorf("value = %v, want true", tv.Value)
	}
	if !c.IsConnected() {
		t.Error("client should still be connected after split-frame read")
	}
}

// TestReadTag_BOOLBitOffsetInRequest verifies the request addresses the exact
// bit (start*8 + bitOffset) — the pre-fix driver always requested bit 0, so
// M0.3-style tags permanently read false.
func TestReadTag_BOOLBitOffsetInRequest(t *testing.T) {
	srv := newFakeS7Server(t)
	c := newTestClient(t, srv)

	tv := c.ReadTag("DB1.DBX0.5", DataTypeBOOL)
	if tv.Error != nil {
		t.Fatalf("read failed: %v", tv.Error)
	}
	if got := srv.lastBitAddress.Load(); got != 5 {
		t.Errorf("request bit address = %d, want 5 (byte 0, bit 5)", got)
	}
	if tv.Value != true {
		t.Errorf("value = %v, want true (server returns LSB=1)", tv.Value)
	}

	tv = c.ReadTag("DB1.DBX2.3", DataTypeBOOL)
	if tv.Error != nil {
		t.Fatalf("read failed: %v", tv.Error)
	}
	if got := srv.lastBitAddress.Load(); got != 2*8+3 {
		t.Errorf("request bit address = %d, want %d (byte 2, bit 3)", got, 2*8+3)
	}
}

// TestProtocolErrorMarksDisconnected verifies that an unparseable response
// drops the connection (the stream can't be trusted) instead of leaving a
// desynced session marked connected forever.
func TestProtocolErrorMarksDisconnected(t *testing.T) {
	srv := newFakeS7Server(t)
	c := newTestClient(t, srv)
	srv.garbageResponse.Store(true)

	tv := c.ReadTag("DB1.DBW0", DataTypeINT)
	if tv.Error == nil {
		t.Fatal("expected error on garbage response")
	}
	if c.IsConnected() {
		t.Error("client must mark itself disconnected after a protocol error")
	}

	// And it must be able to reconnect and read again.
	if err := c.Connect(); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	tv = c.ReadTag("DB1.DBW0", DataTypeINT)
	if tv.Error != nil {
		t.Fatalf("read after reconnect failed: %v", tv.Error)
	}
}

// TestSoak_ReconnectAfterRepeatedDrops simulates a flapping network: the
// server closes the session after every read, 20 times in a row. The client
// must detect each failure and recover with a reconnect every time.
func TestSoak_ReconnectAfterRepeatedDrops(t *testing.T) {
	srv := newFakeS7Server(t)
	srv.dropAfterReads.Store(1)
	c := newTestClient(t, srv)

	recovered := 0
	for i := 0; i < 20; i++ {
		tv := c.ReadTag("DB1.DBB"+strconv.Itoa(i%4), DataTypeBOOL)
		if tv.Error == nil {
			recovered++
		}
		// After the read the server drops the session; next read fails and
		// must flip IsConnected so the driver poll loop reconnects.
		_ = c.ReadTag("DB1.DBB0", DataTypeBOOL) // may fail: connection dropped
		if !c.IsConnected() {
			if err := c.Connect(); err != nil {
				t.Fatalf("cycle %d: reconnect failed: %v", i, err)
			}
		}
	}
	if recovered < 18 { // allow a couple of timing-dependent misses
		t.Errorf("only %d/20 reads succeeded across reconnect cycles", recovered)
	}
	if !c.IsConnected() {
		t.Error("client should end the soak connected")
	}
}
