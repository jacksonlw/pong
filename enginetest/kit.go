package enginetest

import (
	"github.com/jacksonlw/pong/internal/engine"
)

// Tests in another module cannot import the Engine's internal packages. These are the Engine's
// own types and values under the test kit's name, so such a test can still build what it sends
// and read what comes back.
type (
	Answer          = engine.Answer
	Config          = engine.Config
	Context         = engine.Context
	Launch          = engine.Launch
	NewPing         = engine.NewPing
	NewResource     = engine.NewResource
	Ping            = engine.Ping
	Project         = engine.Project
	RegisterSession = engine.RegisterSession
	Runner          = engine.Runner
	SendPong        = engine.SendPong
)

const (
	DeliveryLeft        = engine.DeliveryLeft
	DeliveryUndelivered = engine.DeliveryUndelivered
	KindChange          = engine.KindChange
	KindDocument        = engine.KindDocument
	SessionWorking      = engine.SessionWorking
	StateAnswered       = engine.StateAnswered
	StateWaiting        = engine.StateWaiting
)
