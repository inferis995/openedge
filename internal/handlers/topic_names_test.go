package handlers

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
)

func TestTopicUnsafeNamesAreRefused(t *testing.T) {
	for _, name := range []string{"+", "Acme #1", "$SYS", "a\x00b"} {
		if err := checkTopicSafe("organization", name); !errors.Is(err, errTopicName) {
			t.Errorf("%q: %v, want refused", name, err)
		}
	}
	for _, name := range []string{"Acme Corp", "Linea 1/2", "Società Acque"} {
		if err := checkTopicSafe("site", name); err != nil {
			t.Errorf("%q refused: %v", name, err)
		}
	}
}

// Against a real Postgres (OPENEDGE_TEST_DSN, wiped).
func TestTwoOrganizationsCannotShareTopics(t *testing.T) {
	dsn := os.Getenv("OPENEDGE_TEST_DSN")
	if dsn == "" {
		t.Skip("OPENEDGE_TEST_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, q := range []string{
		`DROP TABLE IF EXISTS tag_forecasts, forecast_profiles, tag_history, tags, gateways, areas, sites, users, organizations CASCADE`,
		`CREATE TABLE organizations (id SERIAL PRIMARY KEY, name TEXT)`,
		`CREATE TABLE sites (id SERIAL PRIMARY KEY, org_id INT REFERENCES organizations(id), name TEXT)`,
		`INSERT INTO organizations (name) VALUES ('Acme Corp'), ('a-b')`,
		`INSERT INTO sites (org_id, name) VALUES (1, 'Plant A'), (2, 'c')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		org   int
		oname string
		sites []string
		clash bool
	}{
		{"same slug as another organization", 0, "acme-corp", nil, true},
		{"same slug, other spelling", 0, "ACME_CORP", nil, true},
		{"a new, distinct organization", 0, "Beta", nil, false},
		{"the organization keeping its own name", 1, "Acme Corp", []string{"Plant A"}, false},
		{"a site whose group is another organization's", 0, "a", []string{"b-c"}, true},
		{"a site of its own", 0, "a", []string{"b"}, false},
	} {
		err := checkTopicNames(ctx, db, tc.org, tc.oname, tc.sites)
		if tc.clash != errors.Is(err, errTopicName) {
			t.Errorf("%s: %v (clash expected %v)", tc.name, err, tc.clash)
		}
	}
}
