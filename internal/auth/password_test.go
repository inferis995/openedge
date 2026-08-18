package auth

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestValidatePasswordEnforcesTheMinimum(t *testing.T) {
	for _, tc := range []struct {
		name string
		pw   string
		ok   bool
	}{
		{"the old minimum", "abc123", false},
		{"one short", strings.Repeat("a", MinPasswordLength-1), false},
		{"exactly the minimum", strings.Repeat("a", MinPasswordLength), true},
		{"empty", "", false},
		// Twelve characters that occupy more than twelve bytes. Counting bytes
		// would accept six of them, which is the length this rule replaced.
		{"non-ASCII, long enough", strings.Repeat("ü", MinPasswordLength), true},
		{"non-ASCII, too short", strings.Repeat("ü", MinPasswordLength-1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.pw)
			if tc.ok && err != nil {
				t.Fatalf("rejected a password that should pass: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("accepted a password shorter than the minimum")
			}
		})
	}
}

// The rule has to hold on every path that stores a password, not on the ones
// somebody remembered.
//
// There were four — accept-invite, create-user, change-password, reset-password
// — and all four said `binding:"required,min=6"`, each written separately, each
// a place a fifth could be added tomorrow with its own number. A validator that
// three of five callers use is not a policy.
//
// So this reads the handlers and fails on any password field still carrying its
// own length, wherever it is.
func TestNoHandlerCarriesItsOwnPasswordMinimum(t *testing.T) {
	dir := filepath.Join("..", "handlers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the handlers: %v", err)
	}

	// A struct tag on a field whose json name mentions "password", carrying a
	// min= of its own.
	tagRe := regexp.MustCompile(`json:"[^"]*[Pp]assword[^"]*"\s+binding:"([^"]*)"`)
	minRe := regexp.MustCompile(`\bmin=(\d+)\b`)

	scanned, offenders := 0, []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		for _, m := range tagRe.FindAllStringSubmatch(string(body), -1) {
			scanned++
			if got := minRe.FindStringSubmatch(m[1]); got != nil {
				n, _ := strconv.Atoi(got[1])
				if n < MinPasswordLength {
					offenders = append(offenders, e.Name()+": binding:\""+m[1]+"\"")
				}
			}
		}
	}

	if scanned == 0 {
		t.Fatal("found no password fields at all — the pattern no longer matches the code, " +
			"and a pattern that matches nothing passes this test forever")
	}
	if len(offenders) > 0 {
		t.Fatalf("%d password field(s) validate their own length below auth.MinPasswordLength (%d). "+
			"Drop the min= and call auth.ValidatePassword, so there is one rule:\n  %s",
			len(offenders), MinPasswordLength, strings.Join(offenders, "\n  "))
	}
}
