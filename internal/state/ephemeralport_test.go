package state

import "testing"

// Which WireGuard ports may move (docs/stale-tether-nat.md). A port is moved
// only when nobody is told to reach it: a relay's, an advertised endpoint's
// and a delivery Core node's stay put.
func TestEphemeralPortIsDecidedByRole(t *testing.T) {
	const base = 51820
	edge := Config{Mode: "Edge", ListenPort: base}
	mesh := func(port uint16) Mesh { return Mesh{Label: "office", ListenPort: port} }

	cases := []struct {
		name string
		cfg  Config
		m    Mesh
		want bool
	}{
		{"an Edge node's port is nobody's contract", edge, mesh(base + 2), true},
		{"pinned by joining is still nobody's contract", edge, Mesh{Label: "home", ListenPort: base + 3}, true},
		{"a Core node keeps its port", Config{Mode: "Core", ListenPort: base}, mesh(base + 2), false},
		{"unset mode means Core, and stays put", Config{ListenPort: base}, mesh(base + 2), false},
		{"a relay keeps its port: peers are sent there", edge, Mesh{Label: "office", ListenPort: base + 2, Relay: true}, false},
		{"a mesh's own advertise keeps its port",
			edge, Mesh{Label: "office", ListenPort: base + 2, Advertise: []string{"203.0.113.7:51822"}}, false},
		{"the device-wide advertise belongs to the mesh on the base port",
			Config{Mode: "Edge", ListenPort: base, Advertise: []string{"203.0.113.7:51820"}}, mesh(base), false},
		{"…and not to a mesh on another port",
			Config{Mode: "Edge", ListenPort: base, Advertise: []string{"203.0.113.7:51820"}}, mesh(base + 2), true},
	}
	for _, c := range cases {
		if got := c.cfg.EphemeralPort(c.m); got != c.want {
			t.Errorf("%s: EphemeralPort = %v, want %v", c.name, got, c.want)
		}
	}
}
