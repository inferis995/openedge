package mqtt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func newTestSpool(t *testing.T, maxBytes int64) *Spool {
	t.Helper()
	s, err := NewSpool(filepath.Join(t.TempDir(), "sub", "spool.jsonl"), maxBytes)
	if err != nil {
		t.Fatalf("creating spool: %v", err)
	}
	return s
}

func msg(payload string) SpooledMessage {
	return SpooledMessage{Topic: "data/plant/line1", QoS: 1, Payload: payload}
}

// Order is everything. A time series resent backwards is not a time series,
// and the `ts` field in the payload does not save you from a buffer that
// reorders.
func TestTheSpoolReplaysInTheOrderItWasWritten(t *testing.T) {
	s := newTestSpool(t, 0)

	for i := range 50 {
		if err := s.Add(msg(fmt.Sprintf("sample-%02d", i))); err != nil {
			t.Fatalf("Add(%d): %v", i, err)
		}
	}

	var got []string
	if err := s.Drain(func(m SpooledMessage) error {
		got = append(got, m.Payload)
		return nil
	}); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if len(got) != 50 {
		t.Fatalf("rispediti %d record su 50", len(got))
	}
	for i, p := range got {
		if want := fmt.Sprintf("sample-%02d", i); p != want {
			t.Fatalf("posizione %d: %q, atteso %q — lo spool ha riordinato", i, p, want)
		}
	}

	if n, _, _ := s.Stats(); n != 0 {
		t.Errorf("dopo una rispedizione riuscita restano %d byte sul disco", n)
	}
}

// The case this exists for: the process dies while the link is down, and
// comes back.
func TestWhatIsSpooledSurvivesTheProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spool.jsonl")

	first, err := NewSpool(path, 0)
	if err != nil {
		t.Fatalf("creating spool: %v", err)
	}
	for i := range 10 {
		if addErr := first.Add(msg(fmt.Sprintf("prima-%d", i))); addErr != nil {
			t.Fatalf("Add: %v", addErr)
		}
	}

	// The process dies: no Close, no commit. It comes back and reopens the file.
	second, err := NewSpool(path, 0)
	if err != nil {
		t.Fatalf("riapertura dello spool: %v", err)
	}

	var got []string
	if err := second.Drain(func(m SpooledMessage) error {
		got = append(got, m.Payload)
		return nil
	}); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("dopo il riavvio si rispediscono %d record su 10 — è il buco "+
			"nello storico che questo file esiste per evitare", len(got))
	}
}

// If the broker falls over again mid-replay, what has not gone out stays.
func TestAFailedSendKeepsTheRestOnDisk(t *testing.T) {
	s := newTestSpool(t, 0)
	for i := range 10 {
		if err := s.Add(msg(fmt.Sprintf("s-%d", i))); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	boom := errors.New("broker sparito di nuovo")
	var sent int
	err := s.Drain(func(SpooledMessage) error {
		if sent == 4 {
			return boom
		}
		sent++
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Drain ha restituito %v, atteso l'errore del broker", err)
	}

	// The 4 that went out are gone, the 6 left stay — in the original order.
	var got []string
	if err := s.Drain(func(m SpooledMessage) error {
		got = append(got, m.Payload)
		return nil
	}); err != nil {
		t.Fatalf("seconda Drain: %v", err)
	}
	if len(got) != 6 {
		t.Fatalf("restano %d record, attesi 6", len(got))
	}
	if got[0] != "s-4" {
		t.Errorf("la ripresa riparte da %q invece che da \"s-4\"", got[0])
	}
}

// A long outage must not fill the disk. The hole that follows has to be
// counted, so it is declared rather than discovered from a chart.
func TestAFullSpoolDropsTheOldestAndSaysSo(t *testing.T) {
	s := newTestSpool(t, 1024)

	for i := range 200 {
		if err := s.Add(msg(fmt.Sprintf("sample-%03d", i))); err != nil {
			t.Fatalf("Add(%d): %v", i, err)
		}
	}

	size, dropped, _ := s.Stats()
	if size > 1024 {
		t.Errorf("lo spool è cresciuto a %d byte oltre il tetto di 1024", size)
	}
	if dropped == 0 {
		t.Fatal("nessun record risulta buttato: il buco nello storico ci sarà " +
			"comunque, ma nessuno saprà che c'è")
	}

	var got []string
	if err := s.Drain(func(m SpooledMessage) error {
		got = append(got, m.Payload)
		return nil
	}); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("lo spool pieno si è svuotato del tutto invece di tenere i più recenti")
	}
	// The most recent are kept: the last one written must be there.
	if got[len(got)-1] != "sample-199" {
		t.Errorf("l'ultimo record è %q, atteso \"sample-199\" — sono stati buttati "+
			"i nuovi invece dei vecchi", got[len(got)-1])
	}
}

// A line truncated mid-write must not take the rest down with it.
func TestACorruptLineIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spool.jsonl")

	s, err := NewSpool(path, 0)
	if err != nil {
		t.Fatalf("creating spool: %v", err)
	}
	if addErr := s.Add(msg("buono-1")); addErr != nil {
		t.Fatalf("Add: %v", addErr)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("apertura per sporcare il file: %v", err)
	}
	if _, wErr := f.WriteString("{\"t\":\"data/x\",\"p\":\"tronc\n"); wErr != nil {
		t.Fatalf("writing the corrupt line: %v", wErr)
	}
	f.Close()

	s2, err := NewSpool(path, 0)
	if err != nil {
		t.Fatalf("riapertura: %v", err)
	}
	if err := s2.Add(msg("buono-2")); err != nil {
		t.Fatalf("Add dopo la corruzione: %v", err)
	}

	var got []string
	if err := s2.Drain(func(m SpooledMessage) error {
		got = append(got, m.Payload)
		return nil
	}); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(got) != 2 || got[0] != "buono-1" || got[1] != "buono-2" {
		t.Fatalf("una riga illeggibile ha portato via i record buoni: %v", got)
	}
	if _, _, corrupt := s2.Stats(); corrupt != 1 {
		t.Errorf("righe corrotte contate: %d, attesa 1", corrupt)
	}
}

// Draining a spool that is not there is not an error: it is the normal case on
// first start and on every successful reconnect.
func TestDrainingAnAbsentSpoolIsNotAnError(t *testing.T) {
	s := newTestSpool(t, 0)
	called := false
	if err := s.Drain(func(SpooledMessage) error { called = true; return nil }); err != nil {
		t.Fatalf("Drain su spool inesistente: %v", err)
	}
	if called {
		t.Error("send chiamata su uno spool vuoto")
	}
}

// The behavior that matters, seen from a driver: publishing with the broker
// down does not lose the sample, it queues it.
//
// The Client is built without connecting, so publishNow finds `c.client == nil`
// and fails exactly as it would against an unreachable broker. That is the case
// this work exists to cover, and it is provable without a broker.
func TestPublishingWithNoBrokerSpoolsInsteadOfLosing(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(Config{
		Host:      "127.0.0.1",
		Port:      1883,
		ClientID:  "test-spool",
		SpoolPath: filepath.Join(dir, "spool.jsonl"),
	})
	if c.spool == nil {
		t.Fatal("lo spool non è stato creato nonostante SpoolPath sia valorizzato")
	}

	for i := range 5 {
		if err := c.PublishWithQoS("data/x", fmt.Sprintf(`{"ts":%d}`, i), 1, false); err != nil {
			t.Fatalf("PublishWithQoS ha restituito un errore invece di accodare: %v", err)
		}
	}

	pending, _, _ := c.spool.Stats()
	if pending == 0 {
		t.Fatal("niente è finito sul disco: i cinque campioni sono persi, che è " +
			"esattamente il difetto da correggere")
	}

	var got []string
	if err := c.spool.Drain(func(m SpooledMessage) error {
		got = append(got, m.Payload)
		return nil
	}); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("in coda ci sono %d messaggi su 5", len(got))
	}
	if got[0] != `{"ts":0}` || got[4] != `{"ts":4}` {
		t.Errorf("payload alterati dal passaggio sul disco: %v", got)
	}
}

// Without SpoolPath the behavior stays as it was: the error goes back to the
// caller. A subscribe-only client must not build an outbound queue nobody will
// ever drain.
func TestWithoutASpoolPathPublishStillFails(t *testing.T) {
	c := NewClient(Config{Host: "127.0.0.1", Port: 1883, ClientID: "test-nospool"})
	if c.spool != nil {
		t.Fatal("è stato creato uno spool senza che SpoolPath fosse valorizzato")
	}
	if err := c.PublishWithQoS("data/x", "{}", 1, false); err == nil {
		t.Error("la pubblicazione a broker giù ha restituito nil senza spool: " +
			"il chiamante crede che il messaggio sia partito")
	}
}
