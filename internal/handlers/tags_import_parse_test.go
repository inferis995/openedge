package handlers

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every data type the parser accepts must be one the database will store.
//
// The parser accepted UINT, UDINT and WORD; tags.data_type carries a CHECK
// constraint listing INT, REAL, BOOL, DINT and STRING. So those three lines
// parsed, reached the INSERT, and were refused there — the operator got a
// Postgres constraint message naming a column, not a sentence naming the types
// this accepts.
//
// This test reads the CHECK out of the schema file rather than restating it, so
// widening the column in SQL without widening the parser (or the reverse) fails
// here instead of at somebody's first import.
func TestTheParserAcceptsOnlyWhatTheColumnStores(t *testing.T) {
	schema, err := os.ReadFile("../../migrations/20250308_schema.sql")
	if err != nil {
		t.Fatalf("reading the schema: %v", err)
	}

	re := regexp.MustCompile(`data_type\s+VARCHAR\(\d+\)\s+NOT NULL\s+CHECK\s*\(data_type IN \(([^)]*)\)\)`)
	m := re.FindSubmatch(schema)
	if m == nil {
		t.Fatal("could not find the CHECK on tags.data_type in the schema — the " +
			"extractor is broken, and a broken extractor passes this test forever")
	}

	allowed := map[string]bool{}
	for _, raw := range strings.Split(string(m[1]), ",") {
		if v := strings.Trim(strings.TrimSpace(raw), "'"); v != "" {
			allowed[v] = true
		}
	}
	if len(allowed) < 3 {
		t.Fatalf("only %d types parsed out of the CHECK: %v", len(allowed), allowed)
	}
	t.Logf("the column accepts: %v", allowed)

	for typ := range allowed {
		line := "Some_Tag : " + typ + " AT 40001"
		if _, err := parseTagLine(line); err != nil {
			t.Errorf("the column stores %s but the parser refuses it: %v", typ, err)
		}
	}

	// And the three that used to slip through to a constraint violation.
	for _, typ := range []string{"UINT", "UDINT", "WORD"} {
		if allowed[typ] {
			continue // the column was widened; nothing to assert
		}
		if _, err := parseTagLine("Some_Tag : " + typ + " AT 40001"); err == nil {
			t.Errorf("the parser accepts %s, which the column will refuse on INSERT: "+
				"the line looks imported and the row is not there", typ)
		}
	}
}

// A blank line, a comment and a trailing semicolon are all normal in a PLC
// export, and none of them is an error.
func TestTheParserSkipsWhatIsNotATag(t *testing.T) {
	for _, line := range []string{"", "   ", "// just a comment", "\t// indented comment"} {
		parsed, err := parseTagLine(line)
		if err != nil {
			t.Errorf("%q was treated as an error: %v", line, err)
		}
		if parsed != nil {
			t.Errorf("%q produced a tag: %+v", line, parsed)
		}
	}

	parsed, err := parseTagLine("HMI_CFG_HBeg_1 : DINT AT 42095;  // heartbeat")
	if err != nil {
		t.Fatalf("a normal line failed to parse: %v", err)
	}
	if parsed.Alias != "HMI_CFG_HBeg_1" || parsed.DataType != "DINT" || parsed.Address != "42095" {
		t.Errorf("parsed as %+v — the semicolon or the comment leaked into a field", parsed)
	}
}
