package mobile

import (
	"net"
	"strconv"
	"testing"

	"github.com/vpavlin/shrooms/internal/waku"
)

// The Android library (v0.38.1) binds fixed defaults — TCP 60000, discovery on
// UDP 9000 — so the phone could not start while another app embedding it held
// them. On 2026-10-01 that app was Loam, and force-stopping it let shrooms
// start. The desktop library picks random ports, so this cannot be reproduced
// with a real node on a laptop; what can be checked is that both of the
// phone's node builds name ports of their own, and that those ports are free.
func TestBothNodeBuildsChooseTheirOwnPorts(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init("phone", "office", dir); err != nil {
		t.Fatal(err)
	}
	cfgPath, _ := paths(dir)
	cfg, st, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]waku.Config{
		"session (Start)":  sessionNodeConfig(cfg, st),
		"shared (joining)": sharedNodeConfig(cfgPath),
	} {
		tcp, okT := c["tcpPort"].(int)
		udp, okU := c["discv5UdpPort"].(int)
		if !okT || !okU {
			t.Errorf("%s: no ports chosen (%v), so the library's fixed defaults apply", name, c)
			continue
		}
		if tcp == 60000 || udp == 9000 {
			t.Errorf("%s: chose the library default (tcp %d, udp %d), which every embedder shares", name, tcp, udp)
		}
		// Free when chosen: both can be bound right now.
		if l, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(tcp))); err != nil {
			t.Errorf("%s: tcp %d is not free: %v", name, tcp, err)
		} else {
			l.Close()
		}
		if pc, err := net.ListenPacket("udp", net.JoinHostPort("", strconv.Itoa(udp))); err != nil {
			t.Errorf("%s: udp %d is not free: %v", name, udp, err)
		} else {
			pc.Close()
		}
	}
}

// A port someone asked for on purpose is left alone.
func TestFreePortsKeepWhatWasAskedFor(t *testing.T) {
	c := withFreePorts(waku.Config{"tcpPort": 4242, "discv5UdpPort": 4243})
	if c["tcpPort"] != 4242 || c["discv5UdpPort"] != 4243 {
		t.Errorf("explicit ports were replaced: %v", c)
	}
}
