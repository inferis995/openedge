package mqtt

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Spool is the disk buffer that keeps samples alive while the broker is not.
//
// # THE DEFECT IT REMOVES
//
// A driver reads the PLC on a fixed tick and publishes over MQTT. When the
// broker was unreachable — network down, container restarted, maintenance —
// that sample went into paho's in-memory queue, which starts from a volatile
// store: on process restart it was gone. What remained in the historian was a
// hole, and a hole in the history is the first thing a shift supervisor
// notices and the one thing there is no excuse for.
//
// # WHY REPLAY WORKS AT ALL, WHICH IS NOT OBVIOUS
//
// The timestamp is not stamped when the row is written to the database. The
// driver stamps it when it READS the PLC and carries it in the payload (`ts`),
// and the historian writes that value into the `time` column. So a sample held
// on disk for twenty minutes and resent afterwards lands in the history at the
// instant it was read, not at the instant it was resent. Without that property
// store-and-forward would draw a wrong curve instead of a gap, which is worse
// than the gap.
//
// # FORMAT
//
// A text file, one JSON record per line. No index, no database: write order is
// replay order, and that is all this needs. A line that cannot be parsed — a
// write truncated by a crash — is skipped and counted, without stopping the
// rest.
//
// # DURABILITY, STATED RATHER THAN IMPLIED
//
// There is no fsync per line: at a thousand samples a second it would cost
// more than reading the PLC. The file is appended and left to the page cache,
// which survives the death of the process and of the container, and survives a
// clean reboot. It does NOT survive a sudden power loss — there the last
// seconds go. That is the right trade for telemetry, and it belongs in writing.
type Spool struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	size     int64

	// dropped counts records discarded because the spool was full. It is not a
	// statistic: it is the size of the hole that will remain in the history,
	// and whoever reads the logs must be able to see it rather than discover it
	// from a chart.
	dropped uint64
	// corrupt counts unreadable lines skipped during replay.
	corrupt uint64
}

// SpooledMessage is one message waiting to go out.
type SpooledMessage struct {
	Topic    string `json:"t"`
	QoS      byte   `json:"q"`
	Retained bool   `json:"r"`
	Payload  string `json:"p"`
}

// DefaultSpoolMaxBytes is the ceiling past which the oldest records are
// dropped. 64 MB is roughly a day of samples for a mid-sized gateway, and is
// small against any disk a container runs on.
const DefaultSpoolMaxBytes int64 = 64 << 20

// NewSpool opens (or creates) the spool. An existing file is kept: that is the
// normal case on restart after an outage, and exactly what this is for.
func NewSpool(path string, maxBytes int64) (*Spool, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultSpoolMaxBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating spool directory: %w", err)
	}
	s := &Spool{path: path, maxBytes: maxBytes}
	if fi, statErr := os.Stat(path); statErr == nil {
		s.size = fi.Size()
	}
	return s, nil
}

// Add queues a message on disk.
//
// When the record does not fit under the ceiling, the oldest are dropped until
// it does. The oldest rather than the newest, because when the link returns the
// first thing anyone needs is the state of the plant NOW; the lost stretch of
// history is recorded in the counter, so the gap is declared instead of silent.
func (s *Spool) Add(m SpooledMessage) error {
	line, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding spooled message: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	// A single record larger than the whole spool will never fit: dropping it
	// now beats emptying the file for nothing and then refusing it anyway.
	if int64(len(line)) > s.maxBytes {
		s.dropped++
		return fmt.Errorf("message of %d bytes exceeds the whole spool cap of %d", len(line), s.maxBytes)
	}

	if s.size+int64(len(line)) > s.maxBytes {
		if trimErr := s.dropOldestLocked(int64(len(line))); trimErr != nil {
			return trimErr
		}
	}

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening spool: %w", err)
	}

	n, writeErr := f.Write(line)
	s.size += int64(n)
	closeErr := f.Close()

	if writeErr != nil {
		return fmt.Errorf("writing to spool: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing spool after write: %w", closeErr)
	}
	return nil
}

// dropOldestLocked rewrites the file without its oldest records, freeing at
// least `need` bytes. It costs a rewrite, but only happens when the spool is
// full — that is, when the link has been down a long time, and disk throughput
// is not the contended resource.
func (s *Spool) dropOldestLocked(need int64) error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.size = 0
			return nil
		}
		return fmt.Errorf("reading spool to trim it: %w", err)
	}

	target := s.maxBytes - need
	cut := 0
	for int64(len(data)-cut) > target {
		i := indexByte(data[cut:], '\n')
		if i < 0 {
			cut = len(data) // no newline left: the file is one line, drop it
			break
		}
		cut += i + 1
		s.dropped++
	}

	if err := os.WriteFile(s.path, data[cut:], 0o644); err != nil {
		return fmt.Errorf("rewriting trimmed spool: %w", err)
	}
	s.size = int64(len(data) - cut)
	return nil
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// Drain resends the messages in the order they were written.
//
// send is called for every record. On the first error the replay stops and
// EVERYTHING not yet sent stays on disk: that is the difference between a
// buffer and a hole. Stopping also keeps a broker that has only just come back
// from being hammered into falling over again.
//
// The file is rewritten with the remainder only after send has succeeded, so a
// crash mid-replay produces duplicates, not losses. For a time series a point
// written twice with the same timestamp is harmless; a missing point is not.
func (s *Spool) Drain(send func(SpooledMessage) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("opening spool to drain it: %w", err)
	}

	var pending [][]byte
	var sendErr error

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		if len(line) == 0 {
			continue
		}
		if sendErr != nil {
			pending = append(pending, line)
			continue
		}
		var m SpooledMessage
		if unmarshalErr := json.Unmarshal(line, &m); unmarshalErr != nil {
			s.corrupt++
			continue
		}
		if sErr := send(m); sErr != nil {
			sendErr = sErr
			pending = append(pending, line)
		}
	}
	scanErr := sc.Err()
	if closeErr := f.Close(); closeErr != nil && scanErr == nil {
		scanErr = closeErr
	}

	if rewriteErr := s.rewriteLocked(pending); rewriteErr != nil {
		return rewriteErr
	}
	if sendErr != nil {
		return sendErr
	}
	return scanErr
}

func (s *Spool) rewriteLocked(lines [][]byte) error {
	if len(lines) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing drained spool: %w", err)
		}
		s.size = 0
		return nil
	}
	var buf []byte
	for _, l := range lines {
		buf = append(buf, l...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(s.path, buf, 0o644); err != nil {
		return fmt.Errorf("rewriting spool after drain: %w", err)
	}
	s.size = int64(len(buf))
	return nil
}

// Stats reports the spool state: bytes waiting, records dropped for a full
// spool, unreadable lines skipped.
func (s *Spool) Stats() (pendingBytes int64, dropped, corrupt uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size, s.dropped, s.corrupt
}
