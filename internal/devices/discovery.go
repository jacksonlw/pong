package devices

// Discovery finds other computers' network listeners.
type Discovery interface {
	// Advertise makes this Engine findable as _pong._tcp with its identifier.
	Advertise(id string, port int) error
	// Browse returns the addresses (host:port) of the Engines found now.
	Browse() []string
	Close()
}

// Static is a fixed list of addresses, standing in for DNS-SD in tests.
type Static []string

func (Static) Advertise(string, int) error { return nil }
func (s Static) Browse() []string          { return s }
func (Static) Close()                      {}
