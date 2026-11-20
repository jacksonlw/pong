package wire

import "encoding/binary"

// buf builds the exact bytes that are signed. Strings and byte strings are a
// big-endian uint32 length followed by their bytes; integers are big-endian.
type buf []byte

func (b *buf) str(s string)   { b.u32(uint32(len(s))); *b = append(*b, s...) }
func (b *buf) bytes(p []byte) { b.u32(uint32(len(p))); *b = append(*b, p...) }
func (b *buf) raw(p []byte)   { *b = append(*b, p...) }
func (b *buf) u8(v uint8)     { *b = append(*b, v) }
func (b *buf) u32(v uint32)   { *b = binary.BigEndian.AppendUint32(*b, v) }
func (b *buf) u64(v uint64)   { *b = binary.BigEndian.AppendUint64(*b, v) }
func (b *buf) i64(v int64)    { b.u64(uint64(v)) }
func (b *buf) boolean(v bool) {
	if v {
		b.u8(1)
	} else {
		b.u8(0)
	}
}
