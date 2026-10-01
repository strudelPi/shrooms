package mesh

import (
	"sort"
	"time"

	"github.com/vpavlin/shrooms/internal/cred"
)

// Credentials that are about to run out, or have.
//
// Two outages in a fortnight were nothing more than this. k11's membership of
// "home" lapsed on 2026-09-25 and took immich, jellyfin and Home Assistant with
// it; nobody had looked at k11. A week later the VPS was six days from the
// same. Every node already knows every member's expiry — it checks it on each
// announce — so the place to see them all is any one node's status, not each
// device in turn.

// DueWithin is how close to expiry a credential is reported as due. The same
// window `shrooms admin renew` sweeps by default, so what status calls due is
// exactly what that command renews: a warning whose fix ignores it would be
// worse than none.
var DueWithin = cred.RenewBefore(cred.DefaultLife)

// Due is one member whose credential runs out within DueWithin, or has.
type Due struct {
	Mesh     string    `json:"mesh"`
	Name     string    `json:"name"`
	Self     bool      `json:"self,omitempty"`
	NotAfter time.Time `json:"not_after"`
	Expired  bool      `json:"expired,omitempty"`
	// Fix is the command that renews it, run where the mesh's admin key is.
	Fix string `json:"fix"`
}

// DueAmong picks the members of one mesh that are due, soonest first.
//
// members is what Mesh.Members reports, which lists this device first; a
// member with no expiry — a mesh with no authority — is never due.
func DueAmong(label string, members []Member, now time.Time) []Due {
	var out []Due
	for i, m := range members {
		if m.NotAfter.IsZero() || m.NotAfter.Sub(now) > DueWithin {
			continue
		}
		out = append(out, Due{
			Mesh:     label,
			Name:     m.Name,
			Self:     i == 0,
			NotAfter: m.NotAfter,
			Expired:  !now.Before(m.NotAfter),
			Fix:      "shrooms admin renew --mesh " + label,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].NotAfter.Before(out[j].NotAfter) })
	return out
}
