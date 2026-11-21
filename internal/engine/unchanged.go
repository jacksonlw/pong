package engine

import (
	"bytes"
	"encoding/json"
)

// sameApartFromTime reports whether a and b are the same once clear has
// cleared the fields that say when each was written.
func sameApartFromTime[T any](a, b T, clear func(*T)) bool {
	clear(&a)
	clear(&b)
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}
