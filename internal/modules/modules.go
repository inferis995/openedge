package modules

// Module keys an organization may enable or disable. They match the frontend
// navigation items one to one: hiding a page in the menu and blocking its
// dedicated API routes are the same list.
//
// Only page-owned route groups are gated. Shared data routes (/tags,
// /history, /gateways, /alarms, /dashboard/overview) stay open on purpose:
// the Trend page reads history, the dashboard reads gateways, and closing
// those would break the pages that remain. What a disabled module loses is
// its own pages and their private APIs — never another page's data.
var groupModules = map[string]string{
	"/synoptics":   "synoptics",
	"/recipes":     "recipes",
	"/shifts":      "shifts",
	"/maintenance": "maintenance",
	"/custom-kpis": "kpis",
	"/oee":         "oee",
	"/i3x/v1":      "i3x",
	"/reports":     "reports",
	"/udt":         "udt",
	"/trend-views": "trend",
}

// All lists every module key the organizations UI offers, in menu order.
var All = []string{
	"dashboard", "setup",
	"sites", "areas", "gateways", "map", "tags", "inventory", "udt",
	"synoptics", "alarms", "recipes", "shifts", "maintenance",
	"trend", "historian", "reports", "kpis", "oee",
	"i3x", "connected-apps", "users",
}

// Valid reports whether key is a known module.
func Valid(key string) bool {
	for _, m := range All {
		if m == key {
			return true
		}
	}
	return false
}

// ModuleForGroup returns the module key owning an API group path,
// or "" when the group is shared platform surface that stays open.
func ModuleForGroup(group string) string {
	return groupModules[group]
}
