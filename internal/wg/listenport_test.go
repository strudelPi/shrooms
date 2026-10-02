package wg

import (
	"net"
	"strconv"
	"testing"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/vpavlin/shrooms/internal/identity"
)

// held reports whether a UDP port on this machine is taken — by trying to take
// it, which is what a stranded NAT translation cares about: whether the socket
// behind the port is still ours.
func held(t *testing.T, port uint16) bool {
	t.Helper()
	pc, err := net.ListenPacket("udp4", ":"+strconv.Itoa(int(port)))
	if err != nil {
		return true
	}
	pc.Close()
	return false
}

// A device moves to a fresh port in place: the old socket is released, the new
// one is held, and the device says which it is — the only way to know after
// asking for port 0 (docs/stale-tether-nat.md).
func TestADeviceMovesToAFreshPort(t *testing.T) {
	id, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	dev, err := NewDevice(tuntest.NewChannelTUN().TUN(), id.WGPriv, 0, device.NewLogger(device.LogLevelSilent, ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })

	first, err := dev.ListenPort()
	if err != nil || first == 0 {
		t.Fatalf("after binding port 0 the device reports %d, %v", first, err)
	}
	if !held(t, first) {
		t.Fatalf("port %d is reported but not held", first)
	}

	second, err := dev.SetListenPort(0)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatalf("moved to the same port %d: a stranded translation would survive", first)
	}
	if !held(t, second) {
		t.Errorf("new port %d is not held", second)
	}
	if held(t, first) {
		t.Errorf("old port %d is still held after the move", first)
	}
	if now, _ := dev.ListenPort(); now != second {
		t.Errorf("device reports %d, the move returned %d", now, second)
	}
}
