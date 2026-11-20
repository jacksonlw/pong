// Package harness holds the contract every Harness adapter meets. Each
// function takes the human's home folder, so tests run against a temporary one.
package harness

// Status is what an adapter reports about its Harness.
type Status struct {
	Detected  bool   // the Harness is installed on this computer
	Connected bool   // Pong's integration is installed in it
	Note      string // what the human should know, in a sentence or two
	Version   string // the Harness's version, "" when unknown
	// Warning is what needs the human: a version Pong was not tested with,
	// or a manual step still outstanding. "" when nothing does.
	Warning string
}

// Adapter installs, removes and reports on Pong's integration in one Harness.
// pong is the absolute path of the pong command the Harness should launch.
type Adapter interface {
	Name() string
	Detect(home string) bool
	Install(home, pong string) error
	Remove(home string) error
	Status(home string) Status
}
