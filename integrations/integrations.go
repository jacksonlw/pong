// Package integrations holds what Pong gives agents: the standing instruction
// (embedded here) and the Ping-writing skill in skill/pong, which the Harness
// plugins bundle.
package integrations

import "embed"

// StandingInstruction is what every agent is told about Pong: to write as it
// would without Pong, and the Thoughts and Notes it sends. Pong takes the
// questions and finished work from the turn itself.
//
//go:embed standing-instruction.md
var StandingInstruction string

// AskingInstruction is added for a Harness whose prompts and turn ends Pong
// cannot read, where the agent's own Pings are the only way they reach Pong.
//
//go:embed asking-instruction.md
var AskingInstruction string

// ClaudeCode holds the Claude Code marketplace and plugin under claude-code/,
// and the skill under skill/, which the plugin bundles when it is written out.
//
//go:embed all:claude-code skill
var ClaudeCode embed.FS
