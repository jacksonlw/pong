// Package integrations holds what Pong gives agents: the standing instruction
// (embedded here) and the Ping-writing skill in skill/pong, which the Harness
// plugins bundle.
package integrations

import "embed"

// StandingInstruction tells an agent to send questions and finished work
// through Pong's tools.
//
//go:embed standing-instruction.md
var StandingInstruction string

// ClaudeCode holds the Claude Code marketplace and plugin under claude-code/,
// and the skill under skill/, which the plugin bundles when it is written out.
//
//go:embed all:claude-code skill
var ClaudeCode embed.FS
