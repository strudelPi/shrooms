package mesh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vpavlin/shrooms/internal/invite"
	"github.com/vpavlin/shrooms/internal/state"
)

// Holding an invite open, on the node that is already a member.
//
// The alternative was for `shrooms invite` to start a Logos Delivery node of
// its own, which worked and was unpleasant: three seconds of dialling, a page
// of library logging that cannot be turned off, and a second Core node joining
// the fleet for the sake of two messages. The daemon is already connected.
//
// The split is the same one the admin key exists for. The daemon holds the
// network key and the connection and never sees the admin key; the CLI holds
// the admin key and signs, and never sees the network key. Neither half can
// admit a device alone.

// invites are the invite topics this node is currently listening on, by content
// topic. Normally empty, and at most one entry outside a test.
type invites struct {
	mu sync.Mutex
	by map[string]*heldInvite

	// admitting is which device each open exchange is admitting, kept after
	// HoldInvite has returned so a credential offered later can be checked
	// against it. See rememberAdmitting.
	admitting map[string]admitting

	// bus is what the invite path subscribes and publishes through. Nil means
	// the mesh's own node; a test that has no node supplies one here.
	bus inviteBus
}

// inviteBus is the part of a rendezvous node the invite path uses.
type inviteBus interface {
	Subscribe(contentTopic string) error
	Unsubscribe(contentTopic string) error
	Send(contentTopic string, payload []byte, ephemeral bool) (string, error)
}

// admitting is the device keys one exchange handed out, and when to forget them.
type admitting struct {
	devicePub []byte
	wgPub     []byte
	until     time.Time
}

type heldInvite struct {
	secret invite.Secret
	reqs   chan *invite.Request

	// until is when this exchange is over whatever has happened, and the topic
	// can be dropped. Zero while HoldInvite is still waiting for a request: the
	// context it was given decides that expiry.
	until time.Time

	// The response as it was published, and the ephemeral key it was sealed
	// to, once ReplyInvite has run. Kept so that a repeat of the request it
	// answered can be answered again. See handleInvite.
	answered   []byte
	answeredTo []byte
}

// Hold subscribes to an invite's topic and returns the first request that opens
// under the token.
//
// One request, then done: an invite admits one device, and the decision is
// local to this node because nobody else is listening on that topic. Returns
// ctx.Err() if the invite expires first.
//
// The topic stays subscribed after this returns, for the token's remaining
// life. The device being admitted retries its request every few seconds until
// it hears an answer, and the answer is published exactly once — so if the
// device was not yet able to hear when it went out, the exchange used to be
// over: the inviter printed "Admitted" and the joiner waited out its deadline.
// Staying on the topic is what lets handleInvite answer the repeats.
func (m *Mesh) HoldInvite(ctx context.Context, s invite.Secret) (*invite.Request, error) {
	name := s.Topic()

	held := &heldInvite{secret: s, reqs: make(chan *invite.Request, 1)}
	m.inv.mu.Lock()
	if m.inv.by == nil {
		m.inv.by = make(map[string]*heldInvite)
	}
	// Swept here rather than on a timer, like rememberAdmitting: a finished
	// exchange whose topic outlived its token costs one idle subscription until
	// the next invite, and a timer would have to know when the node is gone.
	expired := m.sweepInvitesLocked(time.Now())
	if _, busy := m.inv.by[name]; busy {
		m.inv.mu.Unlock()
		return nil, errors.New("that invite is already open")
	}
	m.inv.by[name] = held
	m.inv.mu.Unlock()
	for _, t := range expired {
		m.unsubscribeInvite(t)
	}

	if err := m.inviteBus().Subscribe(name); err != nil {
		m.forgetInvite(name, held)
		return nil, fmt.Errorf("subscribe to the invite topic: %w", err)
	}
	// The fleet is logged on both ends deliberately: if they differ, both sides
	// work perfectly and never meet, and this is the only place it shows.
	m.log.Info("holding an invite open", "preset", m.cfg.Preset, "cluster", m.cfg.ClusterID)

	select {
	case <-ctx.Done():
		m.forgetInvite(name, held)
		return nil, ctx.Err()
	case req := <-held.reqs:
		m.rememberAdmitting(name, req)
		// The token's life, as the caller set it, or the default when the
		// context carries no deadline. The same horizon rememberAdmitting uses,
		// because it is the same exchange.
		until := time.Now().Add(invite.DefaultTTL)
		if d, ok := ctx.Deadline(); ok {
			until = d
		}
		m.inv.mu.Lock()
		held.until = until
		m.inv.mu.Unlock()
		return req, nil
	}
}

// sweepInvitesLocked drops every finished exchange whose token has expired and
// returns their topics, for the caller to unsubscribe once the lock is released.
// The caller holds m.inv.mu.
func (m *Mesh) sweepInvitesLocked(now time.Time) []string {
	var gone []string
	for t, h := range m.inv.by {
		if !h.until.IsZero() && now.After(h.until) {
			delete(m.inv.by, t)
			gone = append(gone, t)
		}
	}
	return gone
}

// forgetInvite drops one exchange and its topic. The pointer is compared so a
// stale caller cannot tear down a later hold of the same token.
func (m *Mesh) forgetInvite(topic string, held *heldInvite) {
	m.inv.mu.Lock()
	if cur, ok := m.inv.by[topic]; ok && cur == held {
		delete(m.inv.by, topic)
	}
	m.inv.mu.Unlock()
	m.unsubscribeInvite(topic)
}

// unsubscribeInvite is best-effort: a stale subscription costs a little traffic
// on one topic, where failing here would abort a completed enrolment.
func (m *Mesh) unsubscribeInvite(topic string) {
	if err := m.inviteBus().Unsubscribe(topic); err != nil {
		m.log.Debug("could not unsubscribe from an invite topic", "err", err)
	}
}

// inviteBus is the node, unless a test has put something else there.
func (m *Mesh) inviteBus() inviteBus {
	if m.inv.bus != nil {
		return m.inv.bus
	}
	return m.node
}

// rememberAdmitting records which device an exchange is admitting.
//
// HoldInvite hands the request to whoever will sign for it and has no way to
// tell, when a credential comes back, whether it names the device that
// actually asked to join — unless it wrote the keys down.
//
// That check is what lets /invite/reply be reachable from the socket group
// (ADR-033). Anybody may mint a token and walk a device of their own through
// the exchange, so the question is not who is calling but whether the admin
// signed for the device in front of us.
//
// Kept for the token's remaining life, keyed by topic because the token is what
// names the exchange and both ends already derive it.
func (m *Mesh) rememberAdmitting(topic string, req *invite.Request) {
	if req == nil {
		return
	}
	m.inv.mu.Lock()
	defer m.inv.mu.Unlock()
	if m.inv.admitting == nil {
		m.inv.admitting = map[string]admitting{}
	}
	now := time.Now()
	// Swept here rather than on a timer: this runs once per enrolment, which is
	// rare, and an entry nobody asks about costs two keys until the next one.
	for t, a := range m.inv.admitting {
		if now.After(a.until) {
			delete(m.inv.admitting, t)
		}
	}
	m.inv.admitting[topic] = admitting{
		devicePub: append([]byte(nil), req.DevicePub...),
		wgPub:     append([]byte(nil), req.WGPub...),
		until:     now.Add(invite.DefaultTTL),
	}
}

// Admitting reports the device keys this node handed out for a token's
// exchange, so a credential offered for it can be checked against them.
func (m *Mesh) Admitting(topic string) (devicePub, wgPub []byte, ok bool) {
	m.inv.mu.Lock()
	defer m.inv.mu.Unlock()
	a, ok := m.inv.admitting[topic]
	if !ok || time.Now().After(a.until) {
		return nil, nil, false
	}
	return a.devicePub, a.wgPub, true
}

// answerDeferred replies to a first-round request with enough to identify the
// mesh and nothing else, so the joiner can derive the identity it will use here
// and ask again for a credential naming it (ADR-017).
//
// It sends the mesh *id* rather than the network key. The id is a one-way hash
// of the key (state.NetworkID) and is the only thing the joiner needs from this
// round: it derives a per-mesh identity from it and comes back. Sending the key
// here handed the mesh's secret to anyone holding the token, on a round that by
// design does not consume the invite and answers as often as it is asked - so
// the "one device, once" the token promises was never enforced on this path.
//
// Answered by the daemon rather than surfaced to the CLI, because nothing in it
// needs the admin key. The invite stays open for the second request, which is
// the one that carries a secret and the one worth a person looking at.
func (m *Mesh) answerDeferred(s invite.Secret, req *invite.Request) {
	if err := m.replyMeshOnly(s, req); err != nil {
		m.log.Warn("could not answer the first round of an invite", "err", err)
		return
	}
	m.log.Info("told a joining device which mesh this is; waiting for its per-mesh keys",
		"name", req.Name)
}

// ReplyInvite seals a response under the token and publishes it.
//
// The mesh's own values — network key, admin keys, name suffix — are filled in
// here rather than by the caller, so the CLI never handles the network key and
// nothing but a well-formed invite response can be published on this topic.
//
// What was published is kept against the exchange, so that if the device asks
// again — because it had not yet heard when this went out — it is told again.
func (m *Mesh) ReplyInvite(s invite.Secret, req *invite.Request, credential []byte) error {
	if req == nil {
		return errors.New("no request to reply to")
	}
	resp := m.inviteResponse()
	resp.NetworkKey = m.nk[:]
	resp.Credential = credential
	blob, err := invite.SealResponse(s, req.EphPub, resp)
	if err != nil {
		return err
	}
	name := s.Topic()
	if _, err := m.inviteBus().Send(name, blob, true); err != nil {
		return fmt.Errorf("publish the invite response: %w", err)
	}
	m.inv.mu.Lock()
	if held, ok := m.inv.by[name]; ok {
		held.answered = blob
		held.answeredTo = append([]byte(nil), req.EphPub...)
	}
	m.inv.mu.Unlock()
	m.log.Info("admitted a device", "name", req.Name, "credential", len(credential) > 0)
	return nil
}

// repeatAnswer publishes an exchange's response again, for a device that asked
// again. The same bytes: the response is sealed to the ephemeral key the
// request carried, and a repeat carries the same one.
func (m *Mesh) repeatAnswer(topic string, blob []byte, name string) {
	if _, err := m.inviteBus().Send(topic, blob, true); err != nil {
		m.log.Warn("could not repeat an invite response", "err", err)
		return
	}
	m.log.Info("a device asked again after being admitted; answered again", "name", name)
}

// inviteResponse is everything an invite answer carries that is not a secret:
// which mesh this is, who may sign for it, and the DNS suffix.
func (m *Mesh) inviteResponse() *invite.Response {
	resp := &invite.Response{
		MeshID:    state.NetworkID(m.nk),
		Timestamp: time.Now().Unix(),
	}
	if m.authority != nil {
		for _, k := range m.authority.Keys {
			resp.AdminKeys = append(resp.AdminKeys, append([]byte(nil), k...))
		}
	}
	return resp
}

// replyMeshOnly answers the first round. No network key and no credential: this
// round does not consume the invite, so whatever it sends can be had as many
// times as the token is presented.
func (m *Mesh) replyMeshOnly(s invite.Secret, req *invite.Request) error {
	if req == nil {
		return errors.New("no request to reply to")
	}
	blob, err := invite.SealResponse(s, req.EphPub, m.inviteResponse())
	if err != nil {
		return err
	}
	if _, err := m.inviteBus().Send(s.Topic(), blob, true); err != nil {
		return fmt.Errorf("publish the invite response: %w", err)
	}
	return nil
}

// handleInvite offers a message to any invite being held. Reports whether it
// belonged to one, so the ordinary control-plane path can skip it.
func (m *Mesh) handleInvite(contentTopic string, payload []byte, now time.Time) bool {
	m.inv.mu.Lock()
	held, ok := m.inv.by[contentTopic]
	if ok && !held.until.IsZero() && now.After(held.until) {
		// Over, whatever this is. Dropped here because a message on the topic
		// is the first moment since it expired that anyone has looked.
		delete(m.inv.by, contentTopic)
		m.inv.mu.Unlock()
		m.unsubscribeInvite(contentTopic)
		return true
	}
	m.inv.mu.Unlock()
	if !ok {
		return false
	}

	// The token is what opens it. Our own response comes back to us on the same
	// topic and does not open as a request, which is the only filtering needed.
	req, err := invite.OpenRequest(held.secret, payload, now)
	if err != nil {
		return true // ours by topic, not readable as a request: drop it quietly
	}
	// A first-round request is answered here and does not consume the invite:
	// it asks which mesh this is, and the answer holds no secret the token does
	// not already imply. The device comes back with keys derived for this mesh,
	// and that request is the one that admits it.
	//
	// A device that asks twice gets told twice, which costs two sealed messages
	// on a topic only it and this node are listening to.
	if req.Deferred {
		go m.answerDeferred(held.secret, req)
		return true
	}

	// Once answered, an invite admits nobody else — but it does answer the
	// same device again. The joiner repeats its request until it hears back,
	// and the repeat carries the same ephemeral key as the request that was
	// answered, so what goes out is the same sealed bytes and readable by the
	// same device only. This is what makes the exchange survive a response the
	// joiner could not yet hear: an Edge node's subscription is not live the
	// moment Subscribe returns, and the first answer lands in that gap.
	//
	// A request under a different key is a different device, or the same one
	// starting over, and gets nothing: the token has been used.
	m.inv.mu.Lock()
	answered, answeredTo := held.answered, held.answeredTo
	m.inv.mu.Unlock()
	if answered != nil {
		if bytes.Equal(req.EphPub, answeredTo) {
			go m.repeatAnswer(contentTopic, answered, req.Name)
		} else {
			m.log.Debug("ignoring a request on a used invite", "name", req.Name)
		}
		return true
	}

	select {
	case held.reqs <- req:
	default: // already taken, not yet answered; the repeat will be, once it is
	}
	return true
}
