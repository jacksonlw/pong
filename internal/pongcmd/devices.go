package pongcmd

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/devices"
	"github.com/jacksonlw/pong/internal/engine"
)

// devicesCmd is `pong devices [pair|approve]`.
func devicesCmd(args []string, stdout, stderr io.Writer) int {
	sub := ""
	if len(args) > 0 && (args[0] == "pair" || args[0] == "approve" || args[0] == "revoke") {
		sub, args = args[0], args[1:]
	}
	fs, dir := flags("devices", stderr)
	to := fs.String("to", "", "pair: the address (host:port) of a computer in the set, when it is not found on the network")
	wait := fs.Duration("wait", 5*time.Minute, "pair: how long to wait for the approval")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return 2
	}
	c, err := client.Load(*dir)
	if err != nil {
		fmt.Fprintln(stderr, "pong: the Engine is not running")
		return 1
	}
	switch sub {
	case "pair":
		return pairCmd(c, *to, *wait, stdout, stderr)
	case "approve":
		return approveCmd(c, pos, stdout, stderr)
	case "revoke":
		return revokeCmd(c, pos, stdout, stderr)
	}
	s, err := c.Devices()
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	printSet(s, stdout)
	if cs, err := c.Computers(); err == nil {
		printComputers(cs, stdout)
	}
	return 0
}

// printComputers says, for each computer, its Harnesses, the Projects it
// takes Sessions in from other Devices, and how many Sessions it runs.
func printComputers(cs []engine.ComputerState, w io.Writer) {
	for _, c := range cs {
		if !c.Reachable {
			continue
		}
		fmt.Fprintf(w, "\n%s runs %d Session%s.\n", c.Name, c.Running, map[bool]string{true: "", false: "s"}[c.Running == 1])
		for _, h := range c.Harnesses {
			state := "not installed"
			switch {
			case h.Installed && h.Connected:
				state = "connected"
			case h.Installed:
				state = "installed, not connected"
			}
			v := ""
			if h.Version != "" {
				v = " " + h.Version
			}
			fmt.Fprintf(w, "  %s%s: %s", h.Name, v, state)
			if h.Warning != "" {
				fmt.Fprintf(w, " (%s)", h.Warning)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "  Takes Sessions from other Devices in %d Project%s.\n", len(c.Projects), map[bool]string{true: "", false: "s"}[len(c.Projects) == 1])
	}
}

func printSet(s devices.Set, w io.Writer) {
	switch {
	case s.Mode == "cloud":
		fmt.Fprintln(w, "Connected through Pong Cloud: your Devices reach each other from any network, sealed so Pong's server cannot read them.")
	case s.Account && s.Why != "":
		fmt.Fprintf(w, "Connected on Local, because %s: directly over this network, nothing through Pong's servers.\n", s.Why)
	default:
		fmt.Fprintln(w, "Connected on Local: directly over this network, nothing through Pong's servers.")
	}
	for _, d := range s.Devices {
		state := "unreachable"
		switch {
		case d.This:
			state = "this computer"
		case d.Reachable:
			state = "reachable"
		}
		fmt.Fprintf(w, "%s (%s): %s\n", d.Name, platformName(d.Platform), state)
	}
	for _, a := range s.Waiting {
		fmt.Fprintf(w, "Asking to join: %s (%s), code %s. Approve with: pong devices approve %s\n", a.Name, platformName(a.Platform), a.Code, a.ID)
	}
}

func platformName(p string) string {
	switch p {
	case "mac":
		return "Mac"
	case "linux":
		return "Linux"
	case "ios":
		return "iPhone"
	}
	return p
}

func pairCmd(c *client.Client, to string, wait time.Duration, stdout, stderr io.Writer) int {
	if to == "" {
		found, err := c.FoundDevices()
		if err != nil {
			fmt.Fprintln(stderr, "pong:", err)
			return 1
		}
		var out []devices.Found
		for _, f := range found {
			if !f.InSet && f.Device.Engine {
				out = append(out, f)
			}
		}
		switch len(out) {
		case 0:
			fmt.Fprintln(stdout, "No other computer running Pong was found on this network. If you know one's address, run: pong devices pair -to <address:port>")
			return 1
		case 1:
			to = out[0].Address
		default:
			fmt.Fprintln(stdout, "Found more than one computer. Pick one with -to:")
			for _, f := range out {
				fmt.Fprintf(stdout, "  pong devices pair -to %s   # %s (%s)\n", f.Address, f.Device.Name, platformName(f.Device.Platform))
			}
			return 1
		}
	}
	p, err := c.Pair(to)
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Asked %s to add this computer. Check that it shows the code %s, then approve it there.\n", p.Target.Name, p.Code)
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		s, err := c.Devices()
		if err != nil {
			continue
		}
		if s.Pairing == nil {
			fmt.Fprintln(stdout, "The connection to "+p.Target.Name+" was lost before it approved this computer. Run pong devices pair again.")
			return 1
		}
		if s.Pairing.Joined {
			fmt.Fprintln(stdout, "Approved. This computer is now in your set:")
			printSet(s, stdout)
			return 0
		}
	}
	fmt.Fprintln(stdout, "No approval came. Run pong devices pair again when you are at "+p.Target.Name+".")
	return 1
}

func approveCmd(c *client.Client, pos []string, stdout, stderr io.Writer) int {
	s, err := c.Devices()
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	if len(pos) == 0 {
		if len(s.Waiting) == 0 {
			fmt.Fprintln(stdout, "No Device is asking to join.")
			return 0
		}
		for _, a := range s.Waiting {
			fmt.Fprintf(stdout, "%s (%s) asks to join with the code %s. If the code matches, run: pong devices approve %s\n", a.Name, platformName(a.Platform), a.Code, a.ID)
		}
		return 0
	}
	if _, err := c.ApproveDevice(pos[0]); err != nil {
		var ce *client.Error
		if errors.As(err, &ce) && ce.Status == 404 {
			fmt.Fprintln(stderr, "pong: no Device with that identifier is asking to join")
			return 1
		}
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	for _, a := range s.Waiting {
		if a.ID == pos[0] {
			fmt.Fprintf(stdout, "Approved %s. It is now in your set.\n", a.Name)
		}
	}
	return 0
}

// revokeCmd takes a Device out of the set, named by its name or identifier.
func revokeCmd(c *client.Client, pos []string, stdout, stderr io.Writer) int {
	if len(pos) == 0 {
		fmt.Fprintln(stderr, "usage: pong devices revoke <name or id>")
		return 2
	}
	s, err := c.Devices()
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	var match []devices.Device
	for _, d := range s.Devices {
		if d.ID == pos[0] || d.Name == pos[0] {
			match = append(match, d)
		}
	}
	switch len(match) {
	case 0:
		fmt.Fprintf(stderr, "pong: no Device named %s is in your set\n", pos[0])
		return 1
	case 1:
	default:
		fmt.Fprintf(stderr, "pong: more than one Device is named %s; use its identifier:\n", pos[0])
		for _, d := range match {
			fmt.Fprintf(stderr, "  pong devices revoke %s   # %s (%s)\n", d.ID, d.Name, platformName(d.Platform))
		}
		return 1
	}
	if _, err := c.RevokeDevice(match[0].ID); err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Revoked %s. It can no longer answer or start anything on your other Devices.\n", match[0].Name)
	return 0
}
