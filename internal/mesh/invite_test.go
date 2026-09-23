package mesh

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/invite"
)

// The inviting side, without a rendezvous node.
//
// What is being pinned is the second answer. The joiner publishes its request
// the moment it has subscribed, and an Edge node's subscription is not live
// then; the inviter answers within a round trip; the answer lands where nobody
// is listening. The joiner retries every few seconds, and until this the
// retries went unanswered — the daemon had dropped the topic the moment it
// handed the first request over — so the inviter printed "Admitted" and the
// phone waited out its deadline, every time.

type fakeInviteBus struct {
	mu     sync.Mutex
	subs   []string
	unsubs []string
	sent   chan []byte
}

func (b *fakeInviteBus) Subscribe(t string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, t)
	return nil
}

func (b *fakeInviteBus) Unsubscribe(t string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.unsubs = append(b.unsubs, t)
	return nil
}

func (b *fakeInviteBus) Send(_ string, p []byte, _ bool) (string, error) {
	b.sent <- append([]byte(nil), p...)
	return "hash", nil
}

func (b *fakeInviteBus) has(list *[]string, t string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range *list {
		if s == t {
			return true
		}
	}
	return false
}

// nothingSent fails the test if the bus publishes within the window.
func nothingSent(t *testing.T, bus *fakeInviteBus, why string) {
	t.Helper()
	select {
	case <-bus.sent:
		t.Fatalf("published a response when it should not have: %s", why)
	case <-time.After(150 * time.Millisecond):
	}
}

func oneSent(t *testing.T, bus *fakeInviteBus, why string) []byte {
	t.Helper()
	select {
	case p := <-bus.sent:
		return p
	case <-time.After(2 * time.Second):
		t.Fatalf("nothing published: %s", why)
		return nil
	}
}

func requestFrom(t *testing.T, s invite.Secret, name string) (blob, ephPriv []byte) {
	t.Helper()
	priv, pub, err := invite.NewEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	blob, err = invite.SealRequest(s, &invite.Request{
		DevicePub: bytes.Repeat([]byte{1}, 32),
		WGPub:     bytes.Repeat([]byte{2}, 32),
		EphPub:    pub,
		Name:      name,
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return blob, priv
}

func TestAnAdmittedDeviceThatAsksAgainIsAnsweredAgain(t *testing.T) {
	bus := &fakeInviteBus{sent: make(chan []byte, 8)}
	m := &Mesh{log: slog.New(slog.DiscardHandler)}
	m.inv.bus = bus
	s, _ := invite.New()
	topic := s.Topic()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var held *invite.Request
	var holdErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		held, holdErr = m.HoldInvite(ctx, s)
	}()
	for !bus.has(&bus.subs, topic) {
		time.Sleep(time.Millisecond)
	}

	// The phone asks. Its request reaches the daemon and is handed over.
	blob, priv := requestFrom(t, s, "phone")
	if !m.handleInvite(topic, blob, time.Now()) {
		t.Fatal("a request on the held topic was not recognised as ours")
	}
	<-done
	if holdErr != nil {
		t.Fatal(holdErr)
	}
	if held.Name != "phone" {
		t.Fatalf("held %q, want the phone's request", held.Name)
	}

	// It asks again before the admin has signed. Nothing to say yet.
	m.handleInvite(topic, blob, time.Now())
	nothingSent(t, bus, "a repeat before the reply has nothing to repeat")

	// The CLI signs and the daemon publishes.
	if err := m.ReplyInvite(s, held, bytes.Repeat([]byte{5}, 300)); err != nil {
		t.Fatal(err)
	}
	first := oneSent(t, bus, "the reply itself")

	// The phone did not hear that, and asks again. It is told again — the
	// same bytes, so it is still readable by the phone and by nobody else.
	m.handleInvite(topic, blob, time.Now())
	again := oneSent(t, bus, "the repeat after admission is what this test exists for")
	if !bytes.Equal(first, again) {
		t.Error("the repeated response differs from the one published")
	}
	resp, err := invite.OpenResponse(s, priv, again, time.Now())
	if err != nil {
		t.Fatalf("the phone cannot open the repeated response: %v", err)
	}
	if len(resp.Credential) != 300 {
		t.Errorf("repeated response carries %d bytes of credential, want 300", len(resp.Credential))
	}

	// Somebody else holding the token gets nothing: it admitted one device.
	other, _ := requestFrom(t, s, "intruder")
	m.handleInvite(topic, other, time.Now())
	nothingSent(t, bus, "a different device on a used invite")

	// And the topic is still held, which is what makes the repeat possible.
	if bus.has(&bus.unsubs, topic) {
		t.Error("dropped the invite topic before the token expired")
	}
	if _, _, ok := m.Admitting(topic); !ok {
		t.Error("forgot which device it was admitting")
	}
}

// Once the token is over, so is the exchange: the topic goes and a late request
// is not answered, whoever sends it.
func TestAUsedInviteIsDroppedWhenItsTokenExpires(t *testing.T) {
	bus := &fakeInviteBus{sent: make(chan []byte, 8)}
	m := &Mesh{log: slog.New(slog.DiscardHandler)}
	m.inv.bus = bus
	s, _ := invite.New()
	topic := s.Topic()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	var held *invite.Request
	go func() {
		defer close(done)
		held, _ = m.HoldInvite(ctx, s)
	}()
	for !bus.has(&bus.subs, topic) {
		time.Sleep(time.Millisecond)
	}
	blob, _ := requestFrom(t, s, "phone")
	m.handleInvite(topic, blob, time.Now())
	<-done
	if err := m.ReplyInvite(s, held, nil); err != nil {
		t.Fatal(err)
	}
	oneSent(t, bus, "the reply")

	// The token's life is over.
	m.inv.mu.Lock()
	m.inv.by[topic].until = time.Now().Add(-time.Second)
	m.inv.mu.Unlock()

	if !m.handleInvite(topic, blob, time.Now()) {
		t.Error("a message on an expired invite topic should still be claimed, and dropped")
	}
	nothingSent(t, bus, "a repeat after the token expired")
	if !bus.has(&bus.unsubs, topic) {
		t.Error("kept listening on a topic whose token has expired")
	}
	m.inv.mu.Lock()
	_, still := m.inv.by[topic]
	m.inv.mu.Unlock()
	if still {
		t.Error("kept the expired exchange")
	}
}

// An invite nobody used goes away when it expires, as it always did.
func TestAnUnusedInviteIsDroppedOnExpiry(t *testing.T) {
	bus := &fakeInviteBus{sent: make(chan []byte, 8)}
	m := &Mesh{log: slog.New(slog.DiscardHandler)}
	m.inv.bus = bus
	s, _ := invite.New()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := m.HoldInvite(ctx, s); err == nil {
		t.Fatal("an invite nobody used did not expire")
	}
	if !bus.has(&bus.unsubs, s.Topic()) {
		t.Error("left the topic of an expired invite subscribed")
	}
	if m.handleInvite(s.Topic(), []byte("x"), time.Now()) {
		t.Error("still claiming messages on a forgotten invite topic")
	}
}
