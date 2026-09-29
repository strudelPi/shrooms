package mesh

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/identity"
	"github.com/vpavlin/shrooms/internal/invite"
	"github.com/vpavlin/shrooms/internal/state"
)

// An invite's answer is published once, ephemerally, and a joiner whose filter
// subscription was not live at that instant never sees it. On 2026-09-29 three
// devices in a row were "admitted" by the inviter and heard nothing, because
// every retry after that went to a topic the inviter had already unsubscribed
// from, and would have been dropped anyway because the invite was spent.
//
// These run the real inviter — HoldInvite, then ReplyInvite called the way the
// daemon's /invite/reply calls it, then handleInvite for everything that
// arrives — against the real joiner, invite.Redeem, over a bus that can lose a
// message. Nothing about the exchange is reimplemented here.

// lossyBus connects one inviter and any number of joiners, and can drop the
// inviter's next few publishes.
type lossyBus struct {
	mu        sync.Mutex
	inviter   *Mesh
	joiners   []chan invite.Message
	drop      int
	published [][]byte
	requests  [][]byte
}

// The inviter's side.
func (b *lossyBus) Subscribe(string) error   { return nil }
func (b *lossyBus) Unsubscribe(string) error { return nil }
func (b *lossyBus) Send(topic string, payload []byte, _ bool) (string, error) {
	b.mu.Lock()
	b.published = append(b.published, append([]byte(nil), payload...))
	lost := b.drop > 0
	if lost {
		b.drop--
	}
	joiners := append([]chan invite.Message(nil), b.joiners...)
	b.mu.Unlock()
	if lost {
		return "", nil
	}
	for _, ch := range joiners {
		select {
		case ch <- invite.Message{Topic: topic, Payload: append([]byte(nil), payload...)}:
		default:
		}
	}
	return "", nil
}

func (b *lossyBus) publishes() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]byte(nil), b.published...)
}

// joinerEnd is one joining device's view of the bus: an invite.Transport.
type joinerEnd struct {
	b    *lossyBus
	msgs chan invite.Message
}

func (b *lossyBus) joiner() *joinerEnd {
	j := &joinerEnd{b: b, msgs: make(chan invite.Message, 64)}
	b.mu.Lock()
	b.joiners = append(b.joiners, j.msgs)
	b.mu.Unlock()
	return j
}

func (j *joinerEnd) Subscribe(string) error          { return nil }
func (j *joinerEnd) Unsubscribe(string) error        { return nil }
func (j *joinerEnd) Messages() <-chan invite.Message { return j.msgs }
func (j *joinerEnd) Send(topic string, payload []byte, _ bool) (string, error) {
	j.b.mu.Lock()
	j.b.requests = append(j.b.requests, append([]byte(nil), payload...))
	j.b.mu.Unlock()
	j.b.inviter.handleInvite(topic, payload, time.Now())
	return "", nil
}

// fastInvites runs the exchange at millisecond scale and restores the real
// intervals afterwards.
func fastInvites(t *testing.T) {
	t.Helper()
	retry, every, max := invite.RetryEvery, inviteResendEvery, inviteResendCap
	invite.RetryEvery = 20 * time.Millisecond
	inviteResendEvery = 5 * time.Millisecond
	t.Cleanup(func() {
		invite.RetryEvery, inviteResendEvery, inviteResendCap = retry, every, max
	})
}

type exchange struct {
	m    *Mesh
	bus  *lossyBus
	s    invite.Secret
	cred []byte
	done chan error // the inviter's side: HoldInvite then ReplyInvite
}

// newExchange opens an invite on a real Mesh, answering it the way the daemon
// does: HoldInvite hands the request to whoever signs, and ReplyInvite is
// called with only the ephemeral key and the name.
func newExchange(t *testing.T, life time.Duration) *exchange {
	t.Helper()
	nk, err := identity.NewNetworkKey()
	if err != nil {
		t.Fatal(err)
	}
	m := &Mesh{
		nk:  nk,
		cfg: state.DefaultConfig().ForMesh(state.Mesh{Label: "office"}, 51820),
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	bus := &lossyBus{inviter: m}
	m.inv.bus = bus
	s, err := invite.New()
	if err != nil {
		t.Fatal(err)
	}
	x := &exchange{m: m, bus: bus, s: s, cred: []byte("a credential for the device that asked"),
		done: make(chan error, 1)}

	ctx, cancel := context.WithTimeout(context.Background(), life)
	t.Cleanup(cancel)
	go func() {
		req, err := m.HoldInvite(ctx, s)
		if err != nil {
			x.done <- err
			return
		}
		x.done <- m.ReplyInvite(s, &invite.Request{EphPub: req.EphPub, Name: req.Name}, x.cred)
	}()
	// HoldInvite subscribes before it waits; let it get there.
	waitFor(t, func() bool {
		m.inv.mu.Lock()
		defer m.inv.mu.Unlock()
		_, held := m.inv.by[s.Topic()]
		return held
	})
	return x
}

func joining(t *testing.T, name string) *invite.Request {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	return &invite.Request{DevicePub: id.DevicePub, WGPub: id.WGPub[:], Name: name}
}

func (x *exchange) redeem(t *testing.T, req *invite.Request, within time.Duration) (*invite.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	return invite.Redeem(ctx, x.bus.joiner(), x.s, req)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// (a) The answer that admits the device is lost, and the device still gets in —
// on the identical bytes, re-sent, not a second credential.
func TestALostInviteAnswerIsSentAgain(t *testing.T) {
	fastInvites(t)
	x := newExchange(t, time.Minute)
	x.bus.drop = 1 // the one publish that used to be the only one

	resp, err := x.redeem(t, joining(t, "x6"), 3*time.Second)
	if err != nil {
		t.Fatalf("the device that was admitted never heard its answer: %v", err)
	}
	if !bytes.Equal(resp.NetworkKey, x.m.nk[:]) || !bytes.Equal(resp.Credential, x.cred) {
		t.Fatal("the answer it heard was not the one it was admitted with")
	}
	if err := <-x.done; err != nil {
		t.Fatalf("inviter: %v", err)
	}

	pubs := x.bus.publishes()
	if len(pubs) < 2 {
		t.Fatalf("published %d times; the lost answer was never sent again", len(pubs))
	}
	for i, p := range pubs[1:] {
		if !bytes.Equal(p, pubs[0]) {
			t.Fatalf("re-send %d is not the stored answer byte for byte — it was re-sealed", i+1)
		}
	}
}

// (b) Somebody else with the same QR code, after it was used: nothing.
func TestAnotherHolderOfTheTokenGetsNothing(t *testing.T) {
	fastInvites(t)
	x := newExchange(t, time.Minute)
	if _, err := x.redeem(t, joining(t, "x6"), 3*time.Second); err != nil {
		t.Fatalf("first device: %v", err)
	}
	if err := <-x.done; err != nil {
		t.Fatalf("inviter: %v", err)
	}
	before := len(x.bus.publishes())

	if _, err := x.redeem(t, joining(t, "someone-else"), 300*time.Millisecond); err == nil {
		t.Fatal("a second holder of a spent invite was answered")
	}
	if after := len(x.bus.publishes()); after != before {
		t.Fatalf("the inviter published %d more times for a device it never admitted", after-before)
	}
}

// (c) Replaying the admitted device's own request yields the stored ciphertext,
// which nobody without that device's ephemeral private key can open.
func TestAReplayedRequestGetsOnlyTheSealedAnswer(t *testing.T) {
	fastInvites(t)
	x := newExchange(t, time.Minute)
	if _, err := x.redeem(t, joining(t, "x6"), 3*time.Second); err != nil {
		t.Fatalf("first device: %v", err)
	}
	if err := <-x.done; err != nil {
		t.Fatalf("inviter: %v", err)
	}
	original := x.bus.publishes()[0]
	x.bus.mu.Lock()
	captured := append([]byte(nil), x.bus.requests[0]...)
	x.bus.mu.Unlock()

	n := len(x.bus.publishes())
	x.m.handleInvite(x.s.Topic(), captured, time.Now().Add(time.Second))
	waitFor(t, func() bool { return len(x.bus.publishes()) > n })
	replayed := x.bus.publishes()[n]
	if !bytes.Equal(replayed, original) {
		t.Fatal("a replayed request produced something other than the stored answer")
	}

	eavesdropper, _, err := invite.NewEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invite.OpenResponse(x.s, eavesdropper, replayed, time.Now()); err == nil {
		t.Fatal("the re-sent answer opened without the joining device's private key")
	}
}

// (d) A replay cannot make the node chatter: one copy per interval, a cap in
// all, and nothing once the invite's life is over.
func TestResendingIsBoundedInRateAndLife(t *testing.T) {
	fastInvites(t)
	inviteResendEvery = time.Second // driven by the clock passed in, not by sleeping
	inviteResendCap = 3
	life := time.Minute
	x := newExchange(t, life)
	opened := time.Now()
	if _, err := x.redeem(t, joining(t, "x6"), 3*time.Second); err != nil {
		t.Fatalf("first device: %v", err)
	}
	if err := <-x.done; err != nil {
		t.Fatalf("inviter: %v", err)
	}
	x.bus.mu.Lock()
	req := append([]byte(nil), x.bus.requests[0]...)
	x.bus.mu.Unlock()
	topic := x.s.Topic()
	base := len(x.bus.publishes())
	settle := func() int { time.Sleep(30 * time.Millisecond); return len(x.bus.publishes()) - base }

	now := time.Now()
	for i := 0; i < 5; i++ {
		x.m.handleInvite(topic, req, now) // all inside one interval of the first send
	}
	if got := settle(); got != 0 {
		t.Fatalf("re-sent %d times within the rate limit", got)
	}

	for i := 1; i <= 10; i++ {
		x.m.handleInvite(topic, req, now.Add(time.Duration(i)*time.Second))
	}
	if got := settle(); got != inviteResendCap {
		t.Fatalf("re-sent %d times, want the cap of %d", got, inviteResendCap)
	}

	// Past the invite's deadline the answer is forgotten, cap or no cap.
	inviteResendCap = 100
	x.m.handleInvite(topic, req, opened.Add(life+time.Second))
	x.m.handleInvite(topic, req, opened.Add(life+2*time.Second))
	if got := settle(); got != 3 {
		t.Fatalf("re-sent after the invite expired (%d total)", got)
	}
	x.m.inv.mu.Lock()
	_, kept := x.m.inv.answered[topic]
	x.m.inv.mu.Unlock()
	if kept {
		t.Fatal("an expired invite's answer is still held")
	}
}

// (e) The answer arriving twice is harmless: the joiner takes the first and is
// done, and the copies after it change nothing.
func TestDuplicateAnswersAfterSuccessAreIgnored(t *testing.T) {
	fastInvites(t)
	x := newExchange(t, time.Minute)
	req := joining(t, "x6")
	resp, err := x.redeem(t, req, 3*time.Second)
	if err != nil {
		t.Fatalf("first device: %v", err)
	}
	if err := <-x.done; err != nil {
		t.Fatalf("inviter: %v", err)
	}
	x.bus.mu.Lock()
	captured := append([]byte(nil), x.bus.requests[0]...)
	x.bus.mu.Unlock()
	// Two more copies go out to a joiner that has already finished.
	for i := 1; i <= 2; i++ {
		x.m.handleInvite(x.s.Topic(), captured, time.Now().Add(time.Duration(i)*time.Second))
	}
	if !bytes.Equal(resp.Credential, x.cred) {
		t.Fatal("the joiner's answer changed")
	}
}

// The admitted device itself, starting a fresh attempt — the app restarted in
// the middle, say — has the same device key and a NEW ephemeral key. The stored
// answer is sealed to the old one and could not open for it, so nothing is
// sent. This is the limit of the re-send, and why the join screen must not
// offer a restart while an attempt is still running.
func TestANewAttemptByTheSameDeviceGetsNothing(t *testing.T) {
	fastInvites(t)
	x := newExchange(t, time.Minute)
	req := joining(t, "x6")
	if _, err := x.redeem(t, req, 3*time.Second); err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	if err := <-x.done; err != nil {
		t.Fatalf("inviter: %v", err)
	}
	before := len(x.bus.publishes())

	// Same device keys; Redeem draws a new ephemeral key every time it is called.
	again := &invite.Request{DevicePub: req.DevicePub, WGPub: req.WGPub, Name: req.Name}
	if _, err := x.redeem(t, again, 300*time.Millisecond); err == nil {
		t.Fatal("a new attempt with a new ephemeral key was answered")
	}
	if after := len(x.bus.publishes()); after != before {
		t.Fatalf("published %d times for an ephemeral key the answer was not sealed to", after-before)
	}
}
