package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// list_tags: a plant has thousands of tags, and every field of every one is
// more than an agent can read. The API filters by gateway_id or area_id; the
// alias search and the compact projection happen here.

// compactTagFields is what list_tags returns unless fields is "full".
var compactTagFields = []string{"id", "alias", "gateway_id", "data_type", "eu_unit"}

func listTagsPath(args map[string]interface{}) string {
	q := url.Values{}
	if v := intArg(args, "gateway_id"); v > 0 {
		q.Set("gateway_id", strconv.Itoa(v))
	} else if v := intArg(args, "area_id"); v > 0 {
		q.Set("area_id", strconv.Itoa(v))
	}
	if len(q) == 0 {
		return "/api/tags"
	}
	return "/api/tags?" + q.Encode()
}

// shapeTags applies search (a case-insensitive substring of the alias, or of
// the code for a tag without one) and, unless fields is "full", keeps only
// the compact fields.
func shapeTags(data []byte, args map[string]interface{}) (string, error) {
	var tags []map[string]interface{}
	if err := json.Unmarshal(data, &tags); err != nil {
		return "", fmt.Errorf("unexpected tags response: %w", err)
	}
	search := strings.ToLower(strings.TrimSpace(strArg(args, "search")))
	full := strings.EqualFold(strArg(args, "fields"), "full")
	out := make([]map[string]interface{}, 0, len(tags))
	for _, t := range tags {
		if search != "" {
			name, _ := t["alias"].(string)
			if name == "" {
				name, _ = t["code"].(string)
			}
			if !strings.Contains(strings.ToLower(name), search) {
				continue
			}
		}
		if full {
			out = append(out, t)
			continue
		}
		c := make(map[string]interface{}, len(compactTagFields))
		for _, f := range compactTagFields {
			if v, ok := t[f]; ok {
				c[f] = v
			}
		}
		out = append(out, c)
	}
	pretty, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(pretty), nil
}
