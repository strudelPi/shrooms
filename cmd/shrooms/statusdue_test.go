package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vpavlin/shrooms/internal/mesh"
)

// status says nothing about credentials until one is due, and then says which,
// when, and the one command per mesh that renews them.

func TestStatusIsQuietWhenNothingIsDue(t *testing.T) {
	var b bytes.Buffer
	printDue(&b, nil, "mesh", time.Now())
	if b.Len() != 0 {
		t.Errorf("printed with nothing due:\n%s", b.String())
	}
}

func TestStatusNamesWhatIsDueAndHowToRenewIt(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	day := 24 * time.Hour
	due := []mesh.Due{
		{Mesh: "home", Name: "k11", NotAfter: now.Add(-2 * day), Expired: true, Fix: "shrooms admin renew --mesh home"},
		{Mesh: "home", Name: "vps", NotAfter: now.Add(6 * day), Fix: "shrooms admin renew --mesh home"},
		{Mesh: "office", Name: "laptop", Self: true, NotAfter: now.Add(9 * day), Fix: "shrooms admin renew --mesh office"},
	}
	var b bytes.Buffer
	printDue(&b, due, "mesh", now)
	out := b.String()
	for _, want := range []string{
		"k11.home.mesh", "EXPIRED 2 days ago",
		"vps.home.mesh", "ends in 6 days",
		"laptop.office.mesh (this device)",
		"shrooms admin renew --mesh home",
		"shrooms admin renew --mesh office",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status does not say %q:\n%s", want, out)
		}
	}
	// One renew per mesh: it sweeps the whole mesh, so repeating it per
	// device would suggest running it twice.
	if n := strings.Count(out, "shrooms admin renew --mesh home"); n != 1 {
		t.Errorf("the home renew command appears %d times:\n%s", n, out)
	}
}
