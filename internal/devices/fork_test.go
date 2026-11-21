package devices_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/jacksonlw/pong/internal/devices"
	"github.com/jacksonlw/pong/wire"
)

// forkSet starts a computer alone and puts it into a set of three: the
// laptop's genesis, the laptop admitting a box, the laptop admitting this
// computer. It returns the computer, the set and the laptop's and box's keys.
func forkSet(t *testing.T) (*devices.Node, wire.Chain, *wire.Keys, *wire.Keys) {
	t.Helper()
	n, err := devices.Start(devices.Config{Dir: t.TempDir(), Name: "Desk", Platform: wire.PlatformMac, NetAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	laptop, _ := wire.NewKeys()
	box, _ := wire.NewKeys()
	c := wire.NewChain(laptop, "Laptop", wire.PlatformMac, true, 1)
	c, _ = c.Append(laptop, wire.Admit, info(box, "Box"), 2)
	c, err = c.Append(laptop, wire.Admit, n.Self(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n.Adopt(c, true); err != nil {
		t.Fatal(err)
	}
	return n, c, laptop, box
}

func info(k *wire.Keys, name string) wire.DeviceInfo {
	p := k.Public()
	return wire.DeviceInfo{ID: k.ID(), Name: name, Platform: wire.PlatformMac, SealKey: p.Seal, SignKey: p.Sign, Engine: true}
}

func members(n *devices.Node) []string {
	var out []string
	for _, d := range n.Set().Devices {
		out = append(out, d.Name)
	}
	slices.Sort(out)
	return out
}

// A stolen box, revoked while this computer was away, writes a record at the
// place of its revoke and reaches this computer first. When the revoke
// arrives it wins: the box and the Device it admitted are out.
func TestAStolenDeviceLosesItsPlaceWhenItsRevokeArrives(t *testing.T) {
	n, c, laptop, box := forkSet(t)
	thiefsFriend, _ := wire.NewKeys()
	thief, _ := c.Append(box, wire.Admit, info(thiefsFriend, "Friend"), 4)
	if changed, err := n.Adopt(thief, false); err != nil || !changed {
		t.Fatalf("the box's record, the first word here, was not taken: %v", err)
	}
	revoke, _ := c.Append(laptop, wire.Revoke, info(box, "Box"), 4)
	if changed, err := n.Adopt(revoke, false); err != nil || !changed {
		t.Fatalf("the revoke did not win: %v", err)
	}
	if got := members(n); !slices.Equal(got, []string{"Desk", "Laptop"}) {
		t.Fatalf("the set is %v", got)
	}
	// Offered the box's chain again, this computer keeps the revoke and says so.
	var f *wire.Fork
	if _, err := n.Adopt(thief, false); !errors.As(err, &f) || f.Pick != "ours" {
		t.Fatalf("the losing chain: %v", err)
	}
	if n.Set().Fork != nil {
		t.Fatal("a fork the rule settles was shown to the human")
	}
}

// When the laptop and the box revoke each other, only the human can say
// which is theirs: the fork waits, and either answer settles it.
func TestTwoDevicesThatRevokeEachOtherWaitForTheHuman(t *testing.T) {
	for _, keep := range []string{"ours", "theirs"} {
		t.Run(keep, func(t *testing.T) {
			n, c, laptop, box := forkSet(t)
			held, _ := c.Append(laptop, wire.Revoke, info(box, "Box"), 4)
			if _, err := n.Adopt(held, false); err != nil {
				t.Fatal(err)
			}
			counter, _ := c.Append(box, wire.Revoke, info(laptop, "Laptop"), 4)
			var f *wire.Fork
			if _, err := n.Adopt(counter, false); !errors.As(err, &f) || !f.Mutual {
				t.Fatalf("a mutual revoke: %v", err)
			}
			fork := n.Set().Fork
			if fork == nil || !fork.Mutual || len(fork.Ours) != 1 || fork.Ours[0].Name != "Laptop" || fork.Theirs[0].Name != "Box" {
				t.Fatalf("the fork shown: %+v", fork)
			}
			if err := n.SettleFork(keep); err != nil {
				t.Fatal(err)
			}
			want := []string{"Desk", "Laptop"}
			if keep == "theirs" {
				want = []string{"Box", "Desk"}
			}
			if got := members(n); !slices.Equal(got, want) || n.Set().Fork != nil {
				t.Fatalf("after keeping %s: %v, fork %+v", keep, got, n.Set().Fork)
			}
		})
	}
}
