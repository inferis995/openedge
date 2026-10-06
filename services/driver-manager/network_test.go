package main

import (
	"testing"

	"github.com/docker/docker/api/types/network"
)

func ep(id string) *network.EndpointSettings { return &network.EndpointSettings{NetworkID: id} }

// Drivers have to land on the broker's network: Docker isolates bridge
// networks, and a driver anywhere else cannot publish what it reads.
func TestDriversJoinTheManagersOwnNetwork(t *testing.T) {
	for _, c := range []struct {
		name string
		nets map[string]*network.EndpointSettings
		want string
	}{
		{"on-prem / VPS", map[string]*network.EndpointSettings{"openedge-net": ep("a")}, "a"},
		{"project-prefixed", map[string]*network.EndpointSettings{"x_default": ep("b"), "x_openedge-net": ep("c")}, "c"},
		{"edge box / Coolify default network", map[string]*network.EndpointSettings{"k3j2h_default": ep("d")}, "d"},
		{"several, none named", map[string]*network.EndpointSettings{"zeta": ep("z"), "alpha": ep("e")}, "e"},
	} {
		id, _, ok := pickDriverNetwork(c.nets)
		if !ok || id != c.want {
			t.Errorf("%s: got %q (ok=%v), want %q", c.name, id, ok, c.want)
		}
	}
	if _, _, ok := pickDriverNetwork(map[string]*network.EndpointSettings{"host": ep("h"), "none": ep("n")}); ok {
		t.Error("host/none networks were chosen")
	}
}
