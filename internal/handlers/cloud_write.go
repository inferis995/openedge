package handlers

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
)

// CloudWriteOrgsSetting names the organizations whose PLCs may be written
// from the cloud broker, as a comma-separated list of ids.
//
// Cloud sync is one connection for the whole platform. Its write path,
// {prefix}/sys/write/{org_id}/..., took the organization from the topic, and
// the topic is whatever the publisher chose: anybody able to publish under
// the prefix on the cloud broker could drive any organization's equipment.
// The local broker pins each login to its own organization; the cloud broker
// is not ours to configure. So the platform administrator now says which
// organizations accept writes from it, and an empty list — the default —
// accepts none.
const CloudWriteOrgsSetting = "cloud_write_org_ids"

// ParseOrgIDList reads "3, 1,3" as [1 3]: positive ids, sorted, once each.
// Anything else in the list is dropped rather than failing the whole list.
func ParseOrgIDList(s string) []int {
	seen := map[int]bool{}
	var out []int
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// FormatOrgIDList is the stored form of ids: "1,3".
func FormatOrgIDList(ids []int) string {
	parts := make([]string, len(ids))
	for i, n := range ids {
		parts[i] = strconv.Itoa(n)
	}
	clean := ParseOrgIDList(strings.Join(parts, ","))
	parts = parts[:len(clean)]
	for i, n := range clean {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// WriteTopicOrg is the organization a sys/write/{org_id}/... topic names, or
// 0 when it names none.
func WriteTopicOrg(topic string) int {
	parts := strings.Split(topic, "/")
	if len(parts) < 3 || parts[0] != "sys" || parts[1] != "write" {
		return 0
	}
	n, err := strconv.Atoi(parts[2])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// CloudWriteAllowed says whether a write that arrived from the cloud broker
// on topic (already without the cloud prefix) may proceed. The setting is
// read on every message, so a change applies without a restart; an error
// reading it refuses the write.
func CloudWriteAllowed(ctx context.Context, db *sql.DB, topic string) (int, bool) {
	org := WriteTopicOrg(topic)
	if org == 0 {
		return 0, false
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM global_settings WHERE key = $1`, CloudWriteOrgsSetting).Scan(&raw)
	if err != nil {
		return org, false
	}
	for _, id := range ParseOrgIDList(raw) {
		if id == org {
			return org, true
		}
	}
	return org, false
}
