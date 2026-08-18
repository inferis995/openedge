//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// The invite flow, which is how everyone except the first admin gets an account.
//
// Note what these tests are NOT allowed to do. /api/auth/accept-invite sits
// behind LoginRateLimit — burst five, then one token every six seconds, shared
// per IP with /api/auth/login — and the suite arrives from one address. The
// first version of this file skipped on 429, so in CI all three tests skipped or
// short-circuited and the file reported green while testing nothing. Skipping is
// how a rate limiter turns a suite into decoration; these wait instead.

type inviteRef struct {
	ID    int    `json:"id"`
	OrgID int    `json:"org_id"`
	Token string `json:"token"`
}

func createInvite(t *testing.T, admin *apiClient, orgID int) inviteRef {
	t.Helper()
	raw := admin.mustDo(http.MethodPost,
		fmt.Sprintf("/api/organizations/%d/invites", orgID),
		map[string]string{"email": "invitee-" + uniqueSuffix() + "@example.com", "role": "user"},
		http.StatusCreated)

	var inv inviteRef
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatalf("decode invite: %v — %s", err, truncate(raw))
	}
	if inv.Token == "" {
		t.Fatalf("the invite carries no token — %s", truncate(raw))
	}
	return inv
}

// postAccept sends one acceptance and reports exactly what came back, 429
// included. The concurrency test needs to see the limiter; the others do not.
func postAccept(token, username, password string) (int, string) {
	body, _ := json.Marshal(map[string]string{
		"token": token, "username": username, "password": password,
	})
	resp, err := httpClient(20 * time.Second).Post(
		apiBase()+"/api/auth/accept-invite", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

// acceptInvite waits out the limiter rather than giving up on it. A 429 says
// nothing about the code under test, and neither does a skipped test.
func acceptInvite(t *testing.T, token, username, password string) (int, string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		status, body := postAccept(token, username, password)
		if status != http.StatusTooManyRequests {
			return status, body
		}
		if time.Now().After(deadline) {
			t.Fatalf("the login limiter refused every attempt for 90s — this test never ran")
		}
		time.Sleep(7 * time.Second) // one token every six
	}
}

// A public endpoint that creates a real account inside a customer's tenant used
// to accept `binding:"required,min=6"`.
func TestInviteRefusesAPasswordTooShortToStore(t *testing.T) {
	admin, _ := adminSession(t)
	org := createOrg(t, admin, "e2e-invite-weak-"+uniqueSuffix())
	inv := createInvite(t, admin, org.ID)

	status, body := acceptInvite(t, inv.Token, "weak-"+uniqueSuffix(), "abc123")
	if status != http.StatusBadRequest {
		t.Fatalf("a six-character password was accepted with %d — %s", status, body)
	}
	if !strings.Contains(body, "12") {
		t.Errorf("the refusal does not say how long a password must be: %s", body)
	}

	// The same invite still works with one that is long enough: a check that
	// only ever refuses could be refusing for any reason at all.
	if status, body = acceptInvite(t, inv.Token, "ok-"+uniqueSuffix(), "a-properly-long-password"); status != http.StatusCreated {
		t.Fatalf("a valid acceptance was refused with %d — %s", status, body)
	}
}

// An invite is one seat.
func TestAnInviteCannotBeUsedTwice(t *testing.T) {
	admin, _ := adminSession(t)
	org := createOrg(t, admin, "e2e-invite-once-"+uniqueSuffix())
	inv := createInvite(t, admin, org.ID)

	if status, body := acceptInvite(t, inv.Token, "first-"+uniqueSuffix(), "a-properly-long-password"); status != http.StatusCreated {
		t.Fatalf("the first acceptance failed with %d — %s", status, body)
	}

	status, body := acceptInvite(t, inv.Token, "second-"+uniqueSuffix(), "a-properly-long-password")
	if status == http.StatusCreated {
		t.Fatalf("the same invite created a second account — %s", body)
	}
	if status != http.StatusBadRequest {
		t.Errorf("want 400 and a reason about the invite, got %d — %s", status, body)
	}
}

// Two acceptances of one invite, at the same moment.
//
// What the first version of this test asserted — "exactly one account is
// created" — was already true before the fix, and it passed against the unfixed
// code in CI. The unique index on users(email) sees to it: both requests insert
// the invite's OWN email address, so the second violates the constraint and is
// rolled back. The database was preventing the duplicate account, not the
// handler.
//
// What the handler was actually doing wrong is the reason the loser was
// refused. AcceptInvite read the invite outside the transaction that consumes
// it, so the second request believed the invite was unused, hashed the password
// and drove on until Postgres stopped it — and reported that collision as
// `409 username already taken`. The username was fine. Someone told a customer
// to pick a different one, and it failed again, and nothing in the response or
// the logs pointed at the invite.
//
// With the lookup inside the transaction and FOR UPDATE, the loser waits for
// the winner to commit, re-reads a row that no longer matches
// `accepted_at IS NULL`, and is told the truth: the invite is spent.
//
// So this asserts the refusal, not just the count. The count alone cannot fail.
func TestConcurrentAcceptancesRefuseTheLoserForTheRightReason(t *testing.T) {
	admin, _ := adminSession(t)
	org := createOrg(t, admin, "e2e-invite-race-"+uniqueSuffix())

	const racers = 3

	// Rounds, not one shot. A race is only reproduced when the requests really
	// overlap, and whether they do depends on scheduling and on how far away the
	// server is: with the defect deliberately reintroduced, this caught it
	// through the proxy and missed it on the direct path in the same CI run.
	// One clean round is not evidence, so it demands three.
	const rounds = 3
	clean := 0

	// Earlier tests drain the limiter's five tokens; it refills one every six
	// seconds, and a round where anybody is throttled is a round that proves
	// nothing. Wait for a full bucket before racing.
	time.Sleep(20 * time.Second)

	for attempt := 1; attempt <= 8 && clean < rounds; attempt++ {
		inv := createInvite(t, admin, org.ID)

		var (
			mu       sync.Mutex
			statuses []int
			bodies   []string
			wg       sync.WaitGroup
			start    = make(chan struct{})
		)
		for i := range racers {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start // release them together
				status, body := postAccept(inv.Token,
					fmt.Sprintf("race-%d-%s", i, uniqueSuffix()), "a-properly-long-password")
				mu.Lock()
				statuses, bodies = append(statuses, status), append(bodies, body)
				mu.Unlock()
			}(i)
		}
		close(start)
		wg.Wait()

		throttled, created := 0, 0
		for _, s := range statuses {
			switch s {
			case http.StatusTooManyRequests:
				throttled++
			case http.StatusCreated:
				created++
			}
		}
		if throttled > 0 {
			t.Logf("attempt %d: the limiter refused %d of %d — retrying", attempt, throttled, racers)
			time.Sleep(25 * time.Second)
			continue
		}
		clean++

		if created != 1 {
			t.Fatalf("%d of %d concurrent acceptances of ONE invite created an account "+
				"(want exactly 1) — statuses %v, bodies %q", created, racers, statuses, bodies)
		}

		// The discriminating half: every loser must be told the invite is spent.
		for i, s := range statuses {
			if s == http.StatusCreated {
				continue
			}
			if s == http.StatusConflict {
				t.Errorf("a loser got 409 %q — that is the users(email) unique index refusing the "+
					"insert, which means the request read the invite outside the transaction and "+
					"ran the whole way on a decision that was already stale. It blames the "+
					"username, which was never the problem", bodies[i])
				continue
			}
			if s != http.StatusBadRequest || !strings.Contains(strings.ToLower(bodies[i]), "invite") {
				t.Errorf("a loser was refused with %d %q; want 400 naming the invite", s, bodies[i])
			}
		}
		if t.Failed() {
			return
		}
		time.Sleep(20 * time.Second) // let the bucket refill for the next round
	}

	if clean < rounds {
		t.Fatalf("only %d of %d rounds actually raced; the limiter throttled the rest, and a "+
			"test that never runs is not a passing test", clean, rounds)
	}
}
