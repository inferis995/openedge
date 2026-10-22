package main

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

const testImage = "openedge-driver-mqtt:latest"

func runningDriver(env ...string) *container.InspectResponse {
	return &container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			State:      &container.State{Running: true},
			HostConfig: &container.HostConfig{NetworkMode: "net-1"},
		},
		Config: &container.Config{Image: testImage, Env: env},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{"openedge-net": {NetworkID: "net-1"}},
		},
	}
}

func TestAdoptMismatch(t *testing.T) {
	want := []string{"GATEWAY_ID=7", "DB_HOST=postgres", "MQTT_HOST=mosquitto", "DB_PASSWORD=secret"}
	image := []string{"PATH=/usr/local/bin:/usr/bin"} // from the image: not ours

	t.Run("up to date is kept", func(t *testing.T) {
		c := runningDriver(append(image, want...)...)
		if why := adoptMismatch(c, testImage, "net-1", want); why != "" {
			t.Fatalf("want adopted, got %q", why)
		}
	})

	t.Run("baked database IP is recreated", func(t *testing.T) {
		// The production failure: created before host names, with the IP of
		// a postgres container that no longer exists.
		c := runningDriver("GATEWAY_ID=7", "DB_HOST=172.18.0.5", "MQTT_HOST=172.18.0.6", "DB_PASSWORD=secret")
		why := adoptMismatch(c, testImage, "net-1", want)
		if !strings.Contains(why, "DB_HOST") || !strings.Contains(why, "MQTT_HOST") {
			t.Fatalf("want DB_HOST and MQTT_HOST named, got %q", why)
		}
	})

	t.Run("reason never carries values", func(t *testing.T) {
		c := runningDriver("GATEWAY_ID=7", "DB_HOST=postgres", "MQTT_HOST=mosquitto", "DB_PASSWORD=old-secret")
		why := adoptMismatch(c, testImage, "net-1", want)
		if !strings.Contains(why, "DB_PASSWORD") || strings.Contains(why, "secret") {
			t.Fatalf("want the key without its value, got %q", why)
		}
	})

	t.Run("optional variable no longer set is recreated", func(t *testing.T) {
		c := runningDriver(append(want, "DB_MAX_OPEN_CONNS=5")...)
		if why := adoptMismatch(c, testImage, "net-1", want); !strings.Contains(why, "DB_MAX_OPEN_CONNS") {
			t.Fatalf("want DB_MAX_OPEN_CONNS named, got %q", why)
		}
	})

	t.Run("other network is recreated", func(t *testing.T) {
		c := runningDriver(want...)
		if why := adoptMismatch(c, testImage, "net-2", want); why == "" {
			t.Fatal("a driver on another network was adopted")
		}
	})

	t.Run("other image or stopped is recreated", func(t *testing.T) {
		if why := adoptMismatch(runningDriver(want...), "openedge-driver-mqtt:v2", "net-1", want); why == "" {
			t.Fatal("a driver on another image was adopted")
		}
		c := runningDriver(want...)
		c.State.Running = false
		if why := adoptMismatch(c, testImage, "net-1", want); why == "" {
			t.Fatal("a stopped driver was adopted")
		}
	})

	t.Run("incomplete inspect is not adopted", func(t *testing.T) {
		if why := adoptMismatch(&container.InspectResponse{}, testImage, "net-1", want); why == "" {
			t.Fatal("an empty inspect was adopted")
		}
	})
}
