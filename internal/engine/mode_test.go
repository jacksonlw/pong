package engine_test

import (
	"testing"

	"github.com/jacksonlw/pong/enginetest"
)

func onMode(t *testing.T, c *enginetest.Computer, mode string) {
	t.Helper()
	enginetest.Eventually(t, c.Name+" on "+mode, func() bool {
		s, _ := c.C.Devices()
		return s.Mode == mode
	})
}
