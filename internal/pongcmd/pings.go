package pongcmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

type answersFlag []engine.Answer

func (a *answersFlag) String() string { return "" }
func (a *answersFlag) Set(s string) error {
	*a = append(*a, engine.Answer{Label: s})
	return nil
}

// flags returns a flag set with the shared -dir flag.
// Flags is a subcommand's flag set with its -dir flag, for a build's own subcommands.
func Flags(name string, stderr io.Writer) (*flag.FlagSet, *string) { return flags(name, stderr) }

// Parse parses args as parse does, for a build's own subcommands.
func Parse(fs *flag.FlagSet, args []string, max int) ([]string, error) { return parse(fs, args, max) }

func flags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	def, _ := engine.DefaultDir()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs, fs.String("dir", def, "data directory (also $PONG_HOME)")
}

// parse parses args with flags before, between or after the positional
// arguments, which it returns. More than max positional arguments is an error.
func parse(fs *flag.FlagSet, args []string, max int) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(pos) > max {
		fmt.Fprintf(fs.Output(), "pong %s: unexpected %q (quote a label or text of several words)\n", fs.Name(), pos[max])
		return nil, errors.New("too many arguments")
	}
	return pos, nil
}

func connect(dir string, stderr io.Writer) *client.Client {
	c, err := client.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, "pong: the Engine is not running")
		return nil
	}
	return c
}

// tagsFlag reads -tag TEXT|ICON|COLOUR; parts after TEXT may be left empty or
// out. A fourth part is taken as a link, which the Engine refuses on a Tag.
type tagsFlag []engine.Tag

func (a *tagsFlag) String() string { return "" }
func (a *tagsFlag) Set(s string) error {
	p := append(strings.SplitN(s, "|", 4), "", "", "")
	*a = append(*a, engine.Tag{Text: p[0], Icon: p[1], Colour: p[2], Link: p[3]})
	return nil
}

type stringsFlag []string

func (a *stringsFlag) String() string     { return "" }
func (a *stringsFlag) Set(s string) error { *a = append(*a, s); return nil }

// ping sends a Ping, waits for its Pong and prints it. With -note it sends a
// Note, prints its id and returns at once. If the Ping ends without a Pong
// (handed back, withdrawn, answered in the Harness) it prints the state and
// exits 3.
func ping(args []string, stdout, stderr io.Writer) int {
	fs, dir := flags("ping", stderr)
	cwd, _ := os.Getwd()
	var n client.NewPing
	fs.StringVar(&n.Session, "session", "", "Session identifier (required)")
	fs.StringVar(&n.Folder, "folder", cwd, "the Session's working folder")
	fs.StringVar(&n.Question, "question", "", "the question, or the Note's text (required)")
	var answers answersFlag
	fs.Var(&answers, "answer", "an answer; repeat for several")
	fs.StringVar(&n.Recommended, "recommended", "", "label of the recommended answer")
	fs.BoolVar(&n.AllowSeveral, "several", false, "the human may tick several answers")
	fs.StringVar(&n.Approves, "approves", "", `"plan" or "action" when approving`)
	fs.StringVar(&n.Context, "context", "", "a short paragraph of background")
	fs.StringVar(&n.Why, "why", "", "why you are asking now")
	fs.BoolVar(&n.MoreComing, "more-coming", false, "another Ping from this Session will follow")
	fs.BoolVar(&n.BlocksAgent, "blocks", false, "the agent cannot continue until the Pong arrives")
	fs.BoolVar(&n.Finished, "finished", false, "the Ping hands over finished work")
	fs.BoolVar(&n.IsNote, "note", false, "send a Note: needs no Pong, returns at once")
	var files, texts stringsFlag
	fs.Var(&files, "file", "attach a file as a Resource: KIND:TITLE:PATH; repeat for several")
	fs.Var(&texts, "text", "attach inline text as a Resource: KIND:TITLE:TEXT; repeat for several")
	var tags tagsFlag
	fs.Var(&tags, "tag", "add a Tag: TEXT|ICON|COLOUR|LINK, leaving out what you don't need; repeat for several")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	n.Tags = tags
	for i, list := range []stringsFlag{files, texts} {
		for _, a := range list {
			parts := strings.SplitN(a, ":", 3)
			if len(parts) != 3 {
				fmt.Fprintf(stderr, "pong: %q is not KIND:TITLE:%s\n", a, map[int]string{0: "PATH", 1: "TEXT"}[i])
				return 2
			}
			r := engine.NewResource{Kind: parts[0], Title: parts[1]}
			if i == 0 {
				r.File = parts[2]
				if !filepath.IsAbs(r.File) {
					r.File = filepath.Join(cwd, r.File)
				}
			} else {
				r.Content = parts[2]
			}
			n.Resources = append(n.Resources, r)
		}
	}
	if n.Session == "" || n.Question == "" {
		fmt.Fprintln(stderr, "usage: pong ping -session ID -question TEXT [-answer LABEL]... [-tag TEXT|ICON|COLOUR|LINK]... [-note] [flags]")
		return 2
	}
	n.Answers = answers
	c := connect(*dir, stderr)
	if c == nil {
		return 1
	}
	p, err := c.CreatePing(n)
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	if n.IsNote {
		fmt.Fprintf(stdout, "Note sent: %s\n", p.ID)
		return 0
	}
	p, err = c.Wait(p.ID)
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	if p.Pong == nil {
		fmt.Fprintf(stdout, "state: %s\n", p.State)
		return 3
	}
	printPong(stdout, p.Pong)
	return 0
}

func printPong(w io.Writer, p *engine.Pong) {
	if p.Answer != "" {
		fmt.Fprintf(w, "answer: %s\n", p.Answer)
	}
	for _, a := range p.Answers {
		fmt.Fprintf(w, "ticked: %s\n", a)
	}
	if p.Text != "" {
		fmt.Fprintf(w, "text: %s\n", p.Text)
	}
	if p.NewSession {
		fmt.Fprintln(w, "new session: yes")
	}
}

// inbox lists waiting Pings.
func inbox(args []string, stdout, stderr io.Writer) int {
	fs, dir := flags("inbox", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	c := connect(*dir, stderr)
	if c == nil {
		return 1
	}
	ps, err := c.Pings(engine.StateWaiting)
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	if len(ps) == 0 {
		fmt.Fprintln(stdout, "No waiting Pings.")
	}
	for _, p := range ps {
		mark := ""
		if p.IsNote {
			mark = "[note] "
		} else if p.BlocksAgent {
			mark = "[blocking] "
		}
		fmt.Fprintf(stdout, "%s  %s%s  (Session %s, %s)\n", p.ID, mark, p.Question, p.Session, p.Folder)
		for _, a := range p.Answers {
			fmt.Fprintf(stdout, "    - %s\n", a.Label)
		}
	}
	return 0
}

// sendPong sends a Pong to a waiting Ping.
func sendPong(args []string, stdout, stderr io.Writer) int {
	fs, dir := flags("pong", stderr)
	answer := fs.String("answer", "", "the chosen answer's label")
	text := fs.String("text", "", "typed text")
	var ticked stringsFlag
	fs.Var(&ticked, "tick", "a ticked answer's label; repeat for several")
	newSession := fs.Bool("new-session", false, "the Pong should start a new Session")
	usage := "usage: pong pong PING-ID [-answer LABEL] [-tick LABEL]... [-text TEXT] [-new-session]"
	pos, err := parse(fs, args, 1)
	if err != nil {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	id := pos[0]
	c := connect(*dir, stderr)
	if c == nil {
		return 1
	}
	p, err := c.Pong(id, client.SendPong{Answer: *answer, Answers: ticked, Text: *text, NewSession: *newSession})
	if err != nil {
		fmt.Fprintln(stderr, "pong:", err)
		return 1
	}
	fmt.Fprintf(stdout, "Pong sent to %s\n", p.ID)
	return 0
}
