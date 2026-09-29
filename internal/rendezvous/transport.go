// Package rendezvous adapts a Logos Delivery node to the interfaces the
// protocol packages define.
//
// It exists so that internal/invite can stay free of cgo — which is what makes
// the enrolment protocol testable without a rendezvous node — while the CLI,
// the daemon and the Android binding all drive it through the same adapter
// rather than three copies of one.
package rendezvous

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vpavlin/shrooms/internal/invite"
	"github.com/vpavlin/shrooms/internal/waku"
)

// A Transport carries the enrolment exchange over a Logos Delivery node.
//
// It comes in two forms, and which one is right is decided by a property of the
// process rather than of the exchange: **the node's event channel hands each
// event to exactly one receiver.** A second `range node.Events()` anywhere in
// the same process is therefore not an observer, it is a thief.
//
//   - InviteTransport reads the node itself. For a node dedicated to the
//     exchange — `shrooms join` on a machine with no daemon, which starts a
//     node, enrols, and throws it away.
//
//   - Fed does not read the node. For a process where something else already
//     does, which is every daemon: whoever owns the node reads it once and
//     calls Deliver.
type Transport struct {
	node    *waku.Node
	msgs    chan invite.Message
	dropped atomic.Uint64
	once    sync.Once

	// cold marks a node started for the exchange alone, which is the only kind
	// worth waiting on: a daemon's node has been connected for hours and may
	// have announced so long before this transport existed, so waiting for an
	// event it will not repeat would only cost the bound.
	cold bool
	// connected closes the first time the node reports a fleet connection.
	connected     chan struct{}
	connectedOnce sync.Once
}

// buffered generously. The exchange reads promptly, but a Core node carries
// every shard in the cluster — measured at 745 messages where 693 belonged to
// another application — and a response dropped for want of a slot looks exactly
// like nobody answering.
const buffered = 512

// InviteTransport wraps a node the exchange has to itself.
//
// **Start it as soon as the node exists, not when the exchange begins.** The
// node's channel is bounded and its C callback drops events when it is full,
// so a node nobody is reading fills with the fleet's traffic within seconds and
// then discards everything that arrives — including the response the exchange
// is waiting for. That cost a working invite exchange once already.
func InviteTransport(node *waku.Node) invite.Transport {
	t := &Transport{node: node, msgs: make(chan invite.Message, buffered),
		cold: true, connected: make(chan struct{})}
	go func() {
		defer t.Close()
		for ev := range t.node.Events() {
			t.Deliver(ev)
		}
	}()
	return t
}

// Fed wraps a node that something else reads, and is driven by Deliver.
//
// This is what the daemon needs, and not having it made enrolment a coin toss.
// The invite transport was created for every daemon at startup and read
// node.Events() alongside the mesh, so each got about half of everything. Round
// one of an enrolment survived that — the joiner retries every five seconds and
// the inviter answers a first-round request every time it sees one. Round two
// did not: the credential response is published exactly ONCE, because the
// inviter drops repeat requests once it has answered, so a single lost message
// ended the exchange with
//
//	the mesh answered but did not issue a credential: context deadline exceeded
//
// on the joiner while the inviter printed "Admitted". Retrying appeared to be
// the only cure, and it worked about half the time, which is the signature of
// the whole thing.
//
// Meshes were given a single reader when the same channel semantics were found
// making a two-mesh node see half its peers (daemon.go, "One reader, every
// mesh"). That fix stopped one reader short. This is the rest of it.
//
// The "exactly once" is history since 2026-09-29: the inviter now keeps the
// answer until the invite's deadline and re-sends it to the device that asked
// (internal/mesh, answerAgain), so a lost answer is recovered by the joiner's
// ordinary retries. One reader is still right; it is no longer the only thing
// standing between a lost message and a spent invite.
func Fed(node *waku.Node) *Transport {
	return &Transport{node: node, msgs: make(chan invite.Message, buffered),
		connected: make(chan struct{})}
}

// Deliver offers one event to the exchange. Safe to call for every event on the
// node: anything that is not an invite message is dropped by the reader.
func (t *Transport) Deliver(ev waku.Event) {
	msg, _, ok := waku.ParseMessage(ev.JSON)
	if !ok {
		t.noteStatus(ev.JSON)
		return
	}
	select {
	case t.msgs <- invite.Message{Topic: msg.ContentTopic, Payload: msg.Payload}:
	default:
		// Dropped rather than blocked: blocking here would stall the node's own
		// reader, which puts the drop one level down where it is invisible —
		// and for a Fed transport it would stall every mesh in the process.
		// Counted so it is not.
		t.dropped.Add(1)
	}
}

// noteStatus records a fleet connection from a status event.
//
// "Connected" or "PartiallyConnected": either means the node has fleet peers it
// can subscribe through, which is what receiving the answer needs.
func (t *Transport) noteStatus(raw string) {
	if t.connected == nil || waku.EventType(raw) != waku.EventConnectionStatus {
		return
	}
	var e struct {
		ConnectionStatus string `json:"connectionStatus"`
	}
	if json.Unmarshal([]byte(raw), &e) != nil {
		return
	}
	if e.ConnectionStatus == "Connected" || e.ConnectionStatus == "PartiallyConnected" {
		t.connectedOnce.Do(func() { close(t.connected) })
	}
}

// WaitReady implements invite.ReadyWaiter. A node that is not cold is assumed
// ready, for the reason on the field.
func (t *Transport) WaitReady(ctx context.Context, max time.Duration) bool {
	if !t.cold {
		return true
	}
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-t.connected:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// Close says no more events are coming, so an exchange waiting on one stops
// rather than sitting until its deadline.
func (t *Transport) Close() { t.once.Do(func() { close(t.msgs) }) }

// Dropped reports messages discarded because the reader was behind.
func (t *Transport) Dropped() uint64 { return t.dropped.Load() }

func (t *Transport) Subscribe(topic string) error   { return t.node.Subscribe(topic) }
func (t *Transport) Unsubscribe(topic string) error { return t.node.Unsubscribe(topic) }

func (t *Transport) Send(topic string, payload []byte, ephemeral bool) (string, error) {
	return t.node.Send(topic, payload, ephemeral)
}

func (t *Transport) Messages() <-chan invite.Message { return t.msgs }
