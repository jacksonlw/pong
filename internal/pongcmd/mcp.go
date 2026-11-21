package pongcmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
)

// serveMCP serves Pong's tools over standard input and output, forwarding
// each call to the Engine's local API. The Session comes from -session or
// $PONG_SESSION, else one is made for this process.
func serveMCP(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, dir := flags("mcp", stderr)
	cwd, _ := os.Getwd()
	session := fs.String("session", os.Getenv("PONG_SESSION"), "Session identifier (also $PONG_SESSION)")
	folder := fs.String("folder", cwd, "the Session's working folder")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *session == "" {
		*session = fmt.Sprintf("mcp-%d-%d", os.Getpid(), time.Now().Unix())
	}
	m := engine.MCP{Backend: forward{*dir}, Session: *session, Folder: *folder, Quiet: attended.Unattended()}
	if err := m.ServeStdio(stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "pong mcp:", err)
		return 1
	}
	return 0
}

// forward reaches the Engine afresh on each call, so an Engine started after
// the MCP server is found.
type forward struct{ dir string }

func (f forward) with(do func(*client.Client) (engine.Ping, error)) (engine.Ping, error) {
	c, err := client.Load(f.dir)
	if err != nil {
		return engine.Ping{}, engine.ErrEngineDown
	}
	p, err := do(c)
	var ce *client.Error
	switch {
	case errors.As(err, &ce):
		return p, errors.New(ce.Message)
	case err != nil:
		return p, engine.ErrEngineDown
	}
	return p, nil
}

func (f forward) CreatePing(n engine.NewPing) (engine.Ping, error) {
	return f.with(func(c *client.Client) (engine.Ping, error) { return c.CreatePing(n) })
}

func (f forward) Get(id string) (engine.Ping, error) {
	return f.with(func(c *client.Client) (engine.Ping, error) { return c.Ping(id) })
}

func (f forward) WaitOnce(id string) (engine.Ping, error) {
	return f.with(func(c *client.Client) (engine.Ping, error) { return c.WaitOnce(id) })
}

func (f forward) Withdraw(id string) (engine.Ping, error) {
	return f.with(func(c *client.Client) (engine.Ping, error) { return c.Withdraw(id) })
}
