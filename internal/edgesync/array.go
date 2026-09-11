package edgesync

import (
	"database/sql/driver"
	"strconv"
	"strings"
)

// pqInt64Array renders a slice as a Postgres int array literal.
//
// lib/pq has no array support of its own, and the alternative — building the
// list into the SQL text — is how an identifier list becomes an injection
// point. These identifiers come from the central platform rather than from a
// user, but a query that is safe only because of where its input happens to
// come from is safe by accident.
type pqInt64Array []int64

func (a pqInt64Array) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, v := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(v, 10))
	}
	b.WriteByte('}')
	return b.String(), nil
}
