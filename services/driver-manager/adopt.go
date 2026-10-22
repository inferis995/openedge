package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
)

// optionalDriverEnv are variables the manager sets on a driver only when it is
// configured to. A container that has one the manager would no longer set is
// out of date just like one missing a variable.
var optionalDriverEnv = []string{"DB_MAX_OPEN_CONNS"}

// adoptMismatch says why a driver container found on startup cannot be kept
// as it is, or "" when it can.
//
// Adopting a running driver after a manager restart avoids a fleet-wide data
// gap. But a container carries the configuration it was created with: before
// this check a driver created with the database's IP baked into DB_HOST was
// adopted again and again, so moving to host names never reached the drivers
// already running, and a rotated password or a new pool size never reached
// any of them. Now a container is kept only if it is running the right image,
// on the manager's network, with the environment the manager would give it
// today; otherwise it is recreated with the current one.
//
// The reason names variables, never their values: it is logged, and some of
// them are passwords.
func adoptMismatch(c *container.InspectResponse, image, networkID string, want []string) string {
	if c == nil || c.ContainerJSONBase == nil || c.State == nil || c.Config == nil {
		return "incomplete inspect"
	}
	if !c.State.Running {
		return "not running"
	}
	if c.Config.Image != image {
		return fmt.Sprintf("image %s, want %s", c.Config.Image, image)
	}
	if networkID != "" && !onNetwork(c, networkID) {
		return "not on the manager's network"
	}
	if keys := envDrift(c.Config.Env, want); len(keys) > 0 {
		return "environment changed: " + strings.Join(keys, ", ")
	}
	return ""
}

func onNetwork(c *container.InspectResponse, networkID string) bool {
	if c.NetworkSettings != nil {
		for _, ep := range c.NetworkSettings.Networks {
			if ep != nil && ep.NetworkID == networkID {
				return true
			}
		}
	}
	return c.HostConfig != nil && string(c.HostConfig.NetworkMode) == networkID
}

// envDrift lists the variables the manager owns whose value in the container
// (have) differs from what it would set now (want), sorted. Variables the
// manager does not own, such as PATH from the image, are ignored.
func envDrift(have, want []string) []string {
	parse := func(env []string) map[string]string {
		m := make(map[string]string, len(env))
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			m[k] = v
		}
		return m
	}
	h, w := parse(have), parse(want)
	owned := make(map[string]bool, len(w)+len(optionalDriverEnv))
	for k := range w {
		owned[k] = true
	}
	for _, k := range optionalDriverEnv {
		owned[k] = true
	}
	var drift []string
	for k := range owned {
		hv, hok := h[k]
		wv, wok := w[k]
		if hok != wok || hv != wv {
			drift = append(drift, k)
		}
	}
	sort.Strings(drift)
	return drift
}
