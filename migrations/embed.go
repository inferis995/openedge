// Package migrations embeds the base schema.
//
// The files in this directory are mounted into Postgres's init directory on a
// central install, and shipped inside the edge installer for a box. Embedding
// them from here rather than copying them into the package that ships them is
// the point: a second copy is a second thing to keep in step, and a schema that
// has silently diverged is discovered by a restore that comes back empty.
package migrations

import "embed"

// Files is every schema file, in the order Postgres will run them.
//
//go:embed *.sql
var Files embed.FS
