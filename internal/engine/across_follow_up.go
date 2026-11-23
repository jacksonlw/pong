package engine

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/jacksonlw/pong/wire"
)

// onFollowUp takes text another Device wrote in the Thread of one of this
// computer's Sessions, by the rules of the local API's follow-up (followUp),
// and answers as a start is answered. Any Device in the set may send it, the
// phone too, as any may send a Pong; the node drops envelopes from anyone
// else, and one carried twice. It runs off the connection's read loop, since
// opening an ended Session's Harness again takes a moment.
func (c *computers) onFollowUp(from string, m wire.Message) {
	go func() {
		req := m.(*wire.FollowUp)
		reply := wire.StartResult{RequestID: req.RequestID}
		var in FollowUp
		if json.Unmarshal(req.FollowUp, &in) != nil || strings.TrimSpace(in.Prompt) == "" {
			reply.Reasons = []string{"prompt is required: say what the Session should do next"}
		} else if prompt, err := c.st.promptWithFiles(in.Prompt, req.SessionID, in.Files); err != nil {
			reply.Reasons = []string{err.Error()}
		} else if sess, err := c.st.followUp(SessionRef{req.Harness, req.SessionID}, prompt); errors.Is(err, errNotFound) {
			reply.Reasons = []string{"no such Session on this computer"}
		} else if err != nil {
			reply.Reasons = []string{err.Error()}
		} else {
			reply.OK = true
			reply.Session, _ = json.Marshal(sess)
		}
		c.a.node.Send(from, &reply)
		c.report("")
	}()
}
