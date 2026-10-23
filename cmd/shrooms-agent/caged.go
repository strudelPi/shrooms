package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/vpavlin/shrooms/internal/agent"
)

// Tasks from caged agents, one cage at a time (ADR-070): a session that does
// not take tasks from every caged agent holds a task from any other for its
// owner, who allows that cage or declines its tasks, here or in the apps.

const cagedUsage = `usage:
  shrooms-agent caged list    SESSION                    which caged agents SESSION takes tasks from, and the tasks held
  shrooms-agent caged allow   SESSION MACHINE/SESSION    take tasks from that cage (MACHINE/* for all of a machine's);
                                                         its held tasks start
  shrooms-agent caged deny    SESSION MACHINE/SESSION    stop taking them; tasks from it are held again
  shrooms-agent caged decline SESSION MACHINE/SESSION    reject the tasks held from it, without allowing or denying it

A session takes tasks from every caged agent when its accept_caged setting is
on, in the Agents apps or PATCH /v1/sessions/SESSION {"accept_caged": true}.`

// cagedMain is `shrooms-agent caged`: who a session on this machine takes
// tasks from among caged agents.
func cagedMain(args []string) error {
	if len(args) < 2 {
		return errors.New(cagedUsage)
	}
	fs := flag.NewFlagSet("caged", flag.ContinueOnError)
	sock := fs.String("socket", "/run/shrooms/shrooms.sock", "the shrooms daemon's control socket")
	if err := fs.Parse(flagsFirst(args[1:])); err != nil {
		return err
	}
	rest := fs.Args()
	c := newA2AClient(*sock)
	ms, err := c.peers()
	if err != nil || len(ms) == 0 {
		return fmt.Errorf("this machine's agent: %v", err)
	}
	base := c.base(ms[0].Addr)
	session := rest[0]
	in, err := sessionInfo(c, base, session)
	if err != nil {
		return err
	}
	allowed := in.AcceptCagedFrom
	body := map[string]any{}
	switch {
	case args[0] == "list":
		printCaged(*in, allowed)
		for _, a := range in.TaskRequests {
			title := a.Title
			if title == "" {
				title = a.Task
			}
			fmt.Printf("  held from %s: %s (%s)\n", a.From, title, a.At.Local().Format("2 Jan 15:04"))
		}
		return nil
	case args[0] == "allow" && len(rest) == 2:
		for _, a := range allowed {
			if a == rest[1] {
				return nil
			}
		}
		allowed = append(allowed, rest[1])
	case args[0] == "deny" && len(rest) == 2:
		kept := []string{}
		for _, a := range allowed {
			if a != rest[1] {
				kept = append(kept, a)
			}
		}
		allowed = kept
	case args[0] == "decline" && len(rest) == 2:
		body["decline_tasks_from"] = rest[1]
	default:
		return errors.New(cagedUsage)
	}
	if _, declining := body["decline_tasks_from"]; !declining {
		if allowed == nil {
			allowed = []string{}
		}
		body["accept_caged_from"] = allowed
	}
	b, _ := json.Marshal(body)
	resp, err := c.http(10*time.Second).Post(base+"/v1/sessions/"+url.PathEscape(session)+"/settings", "application/json", strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if args[0] == "decline" {
		fmt.Printf("declined the tasks %s held from %s\n", session, rest[1])
		return nil
	}
	printCaged(*in, allowed)
	return nil
}

func printCaged(in agent.Info, allowed []string) {
	if in.AcceptCaged {
		fmt.Printf("%s takes tasks from every caged agent (accept_caged)\n", in.Name)
		return
	}
	fmt.Printf("%s takes tasks from caged agents: %s\n", in.Name, orNone(strings.Join(allowed, ", ")))
}

// sessionInfo is one session of this machine's agent, as its list reports it.
func sessionInfo(c a2aClient, base, session string) (*agent.Info, error) {
	resp, err := c.http(10 * time.Second).Get(base + "/v1/sessions")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list struct {
		Sessions []agent.Info `json:"sessions"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	for i := range list.Sessions {
		if list.Sessions[i].Name == session {
			return &list.Sessions[i], nil
		}
	}
	return nil, fmt.Errorf("no session %q on this machine", session)
}
