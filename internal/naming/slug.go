// Package naming holds the one canonical reduction of a human-written name to
// the form used inside MQTT topics.
//
// It exists because that reduction was duplicated eight times — once per
// driver, once in internal/sparkplug, once in the Mosquitto ACL builder, and
// once more as a SQL expression in the historian — and the copies had already
// drifted. Drift here does not raise an error: the driver publishes on a topic
// the historian cannot resolve, the lookup fails, and the tag is simply never
// written down. On a chart that reads as a quiet process, not as a fault.
package naming

import (
	"fmt"
	"strings"
)

// replacements is the whole definition of the canonical form. Slug applies it
// in Go and SQL builds the equivalent Postgres expression from the same list,
// so the two cannot disagree.
//
// '/' is here because it is a topic separator: a site named "Linea 1/2" would
// otherwise produce data/org/linea-1/2/area/... — one level too many, parsed
// as a different site with the wrong area, matching nothing forever.
var replacements = []struct{ from, to string }{
	{" ", "-"},
	{"_", "-"},
	{"/", "-"},
}

// Slug reduces a name to its canonical topic form: trimmed, lower-cased, with
// spaces, underscores and slashes collapsed to hyphens. It is deliberately not
// a general slugifier — it does not touch accents, '+', '#' or any other
// character — because widening it would silently orphan every tag already
// stored under the old spelling.
func Slug(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.from, r.to)
	}
	return s
}

// SQL returns a Postgres expression applying the same reduction to expr, so a
// query can compare a stored name against a slug arriving from a topic without
// either side being canonicalised in advance.
//
// expr is interpolated into the statement and must therefore be a column name
// or a placeholder written in the source — never user input.
func SQL(expr string) string {
	inner := fmt.Sprintf("BTRIM(%s)", expr)
	for _, r := range replacements {
		inner = fmt.Sprintf("REPLACE(%s, '%s', '%s')", inner, r.from, r.to)
	}
	return fmt.Sprintf("LOWER(%s)", inner)
}
