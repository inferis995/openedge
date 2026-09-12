package naming

import (
	"strings"
	"testing"
)

// A name with a slash in it is the reason this package exists. Before it, the
// slash survived the reduction and became an extra topic level: a site called
// "Linea 1/2" published on data/acme/linea-1/2/reparto/gw/tag, which the
// historian parsed as site "linea-1", area "2", gateway "reparto" — a
// hierarchy that does not exist. The lookup failed on every single sample and
// the tag was never historised, with no error anywhere.
func TestASlashCannotBecomeATopicLevel(t *testing.T) {
	for _, name := range []string{"Linea 1/2", "Temp/Out", "a/b/c"} {
		got := Slug(name)
		if strings.Contains(got, "/") {
			t.Errorf("Slug(%q) = %q: the slash survived and will be read as a "+
				"topic separator, putting the tag at a hierarchy level that "+
				"does not exist", name, got)
		}
	}
}

func TestTheCanonicalForm(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Motor 1", "motor-1"},
		{"Motor_1", "motor-1"},
		{"Motor/1", "motor-1"},
		{"motor-1", "motor-1"},
		{"  Cella 1  ", "cella-1"},
		{"OPC-UA1", "opc-ua1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The three spellings an operator may plausibly use for the same thing must
// land on one string, because that string is what the topic and the SQL
// comparison are both built from.
func TestTheThreeSeparatorsConverge(t *testing.T) {
	a, b, c := Slug("Cella 1"), Slug("Cella_1"), Slug("Cella/1")
	if a != b || b != c {
		t.Fatalf("the same name spelled three ways produced %q, %q and %q; a tag "+
			"renamed from one spelling to another would stop being historised", a, b, c)
	}
}

// Slug is applied to values that have already been through it — a gateway name
// slugified by the driver and slugified again downstream — so it has to be a
// no-op the second time or the topic would keep changing shape.
func TestSlugIsIdempotent(t *testing.T) {
	for _, name := range []string{"Linea 1/2", "Motor_1", "  Cella 1  ", "opc-ua1"} {
		once := Slug(name)
		if twice := Slug(once); twice != once {
			t.Errorf("Slug(%q) = %q but Slug(%q) = %q", name, once, once, twice)
		}
	}
}

// ---------------------------------------------------------------------------
// The Go form and the SQL form
// ---------------------------------------------------------------------------

// This is the assertion that matters most. The historian compares a name
// stored in Postgres against a slug that arrived in a topic, and it does the
// comparison in SQL. If the SQL reduction ever handles one character fewer
// than the Go one, the two sides stop meeting for exactly the names containing
// that character — and, again, nothing raises an error.
func TestEveryGoReplacementIsAlsoInTheSQL(t *testing.T) {
	sql := SQL("t.alias")
	for _, r := range replacements {
		want := "'" + r.from + "', '" + r.to + "'"
		if !strings.Contains(sql, want) {
			t.Errorf("Slug collapses %q to %q but SQL(%q) does not: %s\n"+
				"names containing %q would match in Go and not in Postgres",
				r.from, r.to, "t.alias", sql, r.from)
		}
	}
}

// And the reverse: a REPLACE in the SQL that Go does not perform would make
// Postgres match names the driver never publishes under.
func TestTheSQLHasNoReplacementGoDoesNot(t *testing.T) {
	if got, want := strings.Count(SQL("t.alias"), "REPLACE("), len(replacements); got != want {
		t.Fatalf("SQL performs %d replacements, Slug performs %d", got, want)
	}
}

func TestSQLWrapsTheExpressionItIsGiven(t *testing.T) {
	sql := SQL("o.name")
	if !strings.Contains(sql, "BTRIM(o.name)") {
		t.Errorf("SQL(%q) did not trim the column: %s", "o.name", sql)
	}
	if !strings.HasPrefix(sql, "LOWER(") {
		t.Errorf("SQL(%q) did not lower-case the result, so a name typed in "+
			"capitals would not match its slug: %s", "o.name", sql)
	}
}
