package handlers

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip builds an archive from the given entries and returns its path.
func writeZip(t *testing.T, entries ...zipEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the archive: %v", err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, createErr := zw.Create(e.name)
		if createErr != nil {
			t.Fatalf("creating the entry: %v", createErr)
		}
		if _, writeErr := w.Write(e.payload); writeErr != nil {
			t.Fatalf("writing the entry: %v", writeErr)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("finalizing the archive: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing the archive: %v", err)
	}
	return path
}

type zipEntry struct {
	name    string
	payload []byte
}

// dump returns n bytes that deflate cannot shrink.
//
// The first version of these tests used repetitive SQL, which compresses to a
// fraction of its size — so the archives came out under backupMinZipBytes and
// two of the tests were passing on the size floor instead of on the check they
// were written for. Both went on passing with their defect reintroduced.
// A payload that does not compress keeps each test about one thing.
func dump(n int) []byte {
	b := make([]byte, n)
	x := uint32(0x9E3779B9)
	for i := range b {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		b[i] = byte(x)
	}
	return b
}

func TestAGoodArchivePasses(t *testing.T) {
	path := writeZip(t, zipEntry{"full_backup.sql", dump(64 * 1024)})
	if err := verifyBackupZip(path); err != nil {
		t.Fatalf("a real backup was rejected: %v", err)
	}
}

// The failure the whole check exists for: the process died before the zip
// writer wrote its central directory, so the file on disk has a plausible name
// and size and cannot be opened at all.
func TestAnArchiveWithoutItsDirectoryIsRejected(t *testing.T) {
	good := writeZip(t, zipEntry{"full_backup.sql", dump(64 * 1024)})
	data, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	truncated := filepath.Join(t.TempDir(), "truncated.zip")
	// Two thirds of the bytes: nowhere near the end, and — asserted below —
	// still well past the size floor, so the only thing that can reject this
	// file is actually opening it.
	body := data[:len(data)*2/3]
	if err := os.WriteFile(truncated, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(body) < backupMinZipBytes {
		t.Fatalf("the fixture is %d bytes, under the %d-byte floor: this test would "+
			"pass on the size check and prove nothing about opening the archive",
			len(body), backupMinZipBytes)
	}

	if err := verifyBackupZip(truncated); err == nil {
		t.Fatal("a zip with no central directory was accepted as a backup")
	}
}

// A corrupt compressed stream: the header still claims a size, and only
// decompressing the entry finds out otherwise.
func TestAnArchiveWithACorruptEntryIsRejected(t *testing.T) {
	good := writeZip(t, zipEntry{"full_backup.sql", dump(64 * 1024)})
	data, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a run of bytes in the middle of the compressed data, well past the
	// local header and well before the central directory.
	for i := len(data) / 3; i < len(data)/3+64; i++ {
		data[i] ^= 0xFF
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.zip")
	if err := os.WriteFile(corrupt, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := verifyBackupZip(corrupt); err == nil {
		t.Fatal("an archive whose entry does not decompress was accepted; listing " +
			"the entries is not the same as reading them")
	}
}

func TestAnArchiveWithNoDumpInsideIsRejected(t *testing.T) {
	path := writeZip(t, zipEntry{"readme.txt", dump(64 * 1024)})
	err := verifyBackupZip(path)
	if err == nil {
		t.Fatal("an archive with no .sql entry was accepted as a backup")
	}
	if !strings.Contains(err.Error(), "no .sql dump") {
		t.Errorf("the error should name what is missing, got: %v", err)
	}
}

// An empty database is a legitimate thing to back up; a dump of a few hundred
// bytes is not one, because even an empty OpenEdge schema is far larger.
func TestAnArchiveCarryingAlmostNothingIsRejected(t *testing.T) {
	// Padded so the archive itself is well over the size floor: what is empty
	// here is the dump, not the file, and only the dump floor can say so.
	path := writeZip(t,
		zipEntry{"padding.bin", dump(8 * 1024)},
		zipEntry{"full_backup.sql", []byte("-- oops\n")},
	)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < backupMinZipBytes {
		t.Fatalf("the fixture is %d bytes: this test would pass on the size check",
			info.Size())
	}

	if err := verifyBackupZip(path); err == nil {
		t.Fatal("an archive carrying 8 bytes of SQL was accepted as a backup")
	}
}

func TestAMissingArchiveIsRejected(t *testing.T) {
	if err := verifyBackupZip(filepath.Join(t.TempDir(), "absent.zip")); err == nil {
		t.Fatal("a file that does not exist was accepted as a backup")
	}
}

// ---------------------------------------------------------------------------
// writeDumpZip
// ---------------------------------------------------------------------------

// What writeDumpZip produces must be what verifyBackupZip accepts. Two halves
// that disagree would mean every backup failing verification, or none.
func TestWhatWeWriteIsWhatWeAccept(t *testing.T) {
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "full_backup.sql")
	if err := os.WriteFile(dumpPath, dump(64*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "out.zip")

	if err := writeDumpZip(zipPath, dumpPath); err != nil {
		t.Fatalf("writeDumpZip: %v", err)
	}
	if err := verifyBackupZip(zipPath); err != nil {
		t.Fatalf("verifyBackupZip rejected what writeDumpZip produced: %v", err)
	}
}

func TestWritingAnArchiveFromAMissingDumpFails(t *testing.T) {
	dir := t.TempDir()
	err := writeDumpZip(filepath.Join(dir, "out.zip"), filepath.Join(dir, "absent.sql"))
	if err == nil {
		t.Fatal("packing a dump that does not exist reported success")
	}
}

// ---------------------------------------------------------------------------
// humanBytes
// ---------------------------------------------------------------------------

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{5 << 20, "5.0 MB"},
		{2 << 30, "2.0 GB"},
		{3 << 40, "3.0 TB"},
		// Beyond terabytes the loop stops rather than running off the end of
		// the unit string.
		{4 << 50, "4096.0 TB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// probeWritable
// ---------------------------------------------------------------------------

// The check that would have caught, on the first boot, that no Docker install
// had ever written a backup: the directory existed and was listable, and every
// create was refused.
func TestADirectoryThatRefusesWritesIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which is allowed to write into a read-only directory")
	}
	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.Mkdir(dir, 0o555); err != nil { // r-xr-xr-x: listable, not writable
		t.Fatal(err)
	}

	if err := probeWritable(dir); err == nil {
		t.Fatal("a directory that refuses every create was reported as writable; " +
			"stat is not a write, and only a write settles this")
	}
}

func TestAWritableDirectoryPasses(t *testing.T) {
	dir := t.TempDir()
	if err := probeWritable(dir); err != nil {
		t.Fatalf("a writable directory was rejected: %v", err)
	}
}

// The probe must not leave its own droppings in the directory it checks.
func TestTheProbeLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	if err := probeWritable(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("the probe left %v behind", names)
	}
}

func TestADirectoryThatIsNotThereIsReported(t *testing.T) {
	if err := probeWritable(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a directory that does not exist was reported as writable")
	}
}
