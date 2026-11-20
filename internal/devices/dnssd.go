package devices

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DNSSD advertises and browses _pong._tcp through the system's own DNS-SD
// tools: dns-sd on the Mac, avahi-publish and avahi-browse on Linux.
// Browsing runs in the background and Browse returns what was last found.
type DNSSD struct {
	Name string // shown in the advertised instance name

	mu      sync.Mutex
	adv     *exec.Cmd
	found   []string
	started bool
	stop    chan struct{}
}

const serviceType = "_pong._tcp"

func (d *DNSSD) Advertise(id string, port int) error {
	instance := fmt.Sprintf("Pong %s %s", d.Name, id[:6])
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("dns-sd", "-R", instance, serviceType, "local", strconv.Itoa(port), "id="+id)
	} else {
		cmd = exec.Command("avahi-publish", "-s", instance, serviceType, strconv.Itoa(port), "id="+id)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	d.mu.Lock()
	d.adv = cmd
	d.mu.Unlock()
	go cmd.Wait()
	return nil
}

func (d *DNSSD) Browse() []string {
	d.mu.Lock()
	if !d.started {
		d.started = true
		d.stop = make(chan struct{})
		go d.browseLoop(d.stop)
	}
	out := append([]string{}, d.found...)
	d.mu.Unlock()
	return out
}

func (d *DNSSD) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.adv != nil && d.adv.Process != nil {
		d.adv.Process.Kill()
	}
	if d.stop != nil {
		close(d.stop)
		d.stop = nil
	}
}

func (d *DNSSD) browseLoop(stop chan struct{}) {
	for {
		var found []string
		if runtime.GOOS == "darwin" {
			found = browseDarwin()
		} else {
			found = browseAvahi()
		}
		d.mu.Lock()
		d.found = found
		d.mu.Unlock()
		select {
		case <-stop:
			return
		case <-time.After(10 * time.Second):
		}
	}
}

// lines runs a command for at most wait and returns its output lines.
func lines(wait time.Duration, name string, args ...string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, _ := cmd.StdoutPipe()
	if cmd.Start() != nil {
		return nil
	}
	var ls []string
	s := bufio.NewScanner(out)
	for s.Scan() {
		ls = append(ls, s.Text())
	}
	cmd.Wait()
	return ls
}

// browseLine matches a dns-sd -B "Add" line; the instance name is the rest.
var browseLine = regexp.MustCompile(`\sAdd\s+\d+\s+\d+\s+\S+\s+_pong\._tcp\.\s+(.+)$`)

// reachLine matches dns-sd -L's "can be reached at host:port".
var reachLine = regexp.MustCompile(`can be reached at (\S+?)\.?:(\d+)`)

func browseDarwin() []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines(1500*time.Millisecond, "dns-sd", "-B", serviceType, "local") {
		m := browseLine.FindStringSubmatch(l)
		if m == nil || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		for _, r := range lines(1500*time.Millisecond, "dns-sd", "-L", m[1], serviceType, "local") {
			if rm := reachLine.FindStringSubmatch(r); rm != nil {
				out = append(out, net.JoinHostPort(strings.TrimSuffix(rm[1], "."), rm[2]))
				break
			}
		}
	}
	return out
}

func browseAvahi() []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range lines(5*time.Second, "avahi-browse", "-rpt", serviceType) {
		f := strings.Split(l, ";")
		if len(f) < 9 || f[0] != "=" {
			continue
		}
		a := net.JoinHostPort(f[7], f[8])
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}
