package engine

import (
	"fmt"
	"os"

	"github.com/jacksonlw/pong/wire"
)

// partsTo sends a File's parts to another computer ahead of what names it,
// in order on the way to it, queued while it cannot be reached.
func (a *across) partsTo(computer string) func(sum string, offset, total int64, data []byte) error {
	return func(sum string, offset, total int64, data []byte) error {
		_, err := a.node.SendOrQueue(computer, fmt.Sprintf("file:%s:%d", sum, offset),
			&wire.FilePart{File: sum, Offset: offset, Total: total, Data: data})
		return err
	}
}

// onFilePart keeps a part of a File another Device sends to one of this
// computer's Sessions (takePart). The node drops envelopes from anyone
// outside the set.
func (a *across) onFilePart(from string, m wire.Message) {
	p := m.(*wire.FilePart)
	if err := takePart(a.s.dir, p.File, p.Offset, p.Total, p.Data); err != nil {
		fmt.Fprintln(os.Stderr, "pong-engine: a File part from another Device was not kept:", err)
	}
}
