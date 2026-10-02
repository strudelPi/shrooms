package main

import (
	"log/slog"
	"testing"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/vpavlin/shrooms/internal/identity"
	"github.com/vpavlin/shrooms/internal/wg"
)

func testInstance(t *testing.T, label string, ephemeral bool) *instance {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	dev, err := wg.NewDevice(tuntest.NewChannelTUN().TUN(), id.WGPriv, 0, device.NewLogger(device.LogLevelSilent, ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	port, err := dev.ListenPort()
	if err != nil {
		t.Fatal(err)
	}
	in := &instance{label: label, dev: dev, ephemeral: ephemeral}
	in.port.Store(uint32(port))
	return in
}

// After a network change, every mesh whose port is ephemeral gets a fresh one,
// and the rest keep theirs: a relay or an advertised endpoint that moved would
// strand everybody who was told to reach it (docs/stale-tether-nat.md).
// Driven on real WireGuard devices, because the socket the device actually
// holds is the whole point.
func TestANetworkChangeMovesOnlyEphemeralPorts(t *testing.T) {
	edge := testInstance(t, "office", true)
	pinned := testInstance(t, "relay", false)
	edgeWas, pinnedWas := edge.listenPort(), pinned.listenPort()

	moveEphemeralPorts(slog.New(slog.DiscardHandler), []*instance{edge, pinned, nil})

	if edge.listenPort() == edgeWas {
		t.Errorf("the ephemeral mesh kept port %d through a network change", edgeWas)
	}
	if bound, _ := edge.dev.ListenPort(); bound != edge.listenPort() {
		t.Errorf("the daemon thinks %d, the device holds %d", edge.listenPort(), bound)
	}
	if pinned.listenPort() != pinnedWas {
		t.Errorf("the pinned mesh moved from %d to %d", pinnedWas, pinned.listenPort())
	}
	if bound, _ := pinned.dev.ListenPort(); bound != pinnedWas {
		t.Errorf("the pinned mesh's device moved from %d to %d", pinnedWas, bound)
	}
}
