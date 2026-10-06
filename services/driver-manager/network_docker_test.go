package main

import (
	"context"
	"os"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// TestEnsureNetworkInsideDocker runs only inside a container with the Docker
// socket mounted (OPENEDGE_DOCKER_IT=1): the manager must pick the network it
// is itself attached to, named in WANT_NETWORK.
func TestEnsureNetworkInsideDocker(t *testing.T) {
	if os.Getenv("OPENEDGE_DOCKER_IT") != "1" {
		t.Skip("needs a container with the Docker socket")
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{dockerClient: cli, ctx: context.Background()}
	if err := m.ensureNetwork(); err != nil {
		t.Fatal(err)
	}
	nw, err := cli.NetworkInspect(m.ctx, m.networkID, network.InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if nw.Name != os.Getenv("WANT_NETWORK") {
		t.Errorf("drivers would join %q, want %q", nw.Name, os.Getenv("WANT_NETWORK"))
	}
}
