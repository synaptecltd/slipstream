package slipstream

// Simple8bThresholdSamples defines the number of samples per message required before using simple-8b encoding
const Simple8bThresholdSamples = 16

// DefaultDeltaEncodingLayers defines the default number of layers of delta encoding. 0 is no delta encoding (just use varint), 1 is delta encoding, etc.
const DefaultDeltaEncodingLayers = 3

// HighDeltaEncodingLayers defines the number of layers of delta encoding for high sampling rate scenarios.
const HighDeltaEncodingLayers = 3

// MaxHeaderSize is the size of the message header in bytes
const MaxHeaderSize = 36

// UseGzipThresholdSamples is the minimum number of samples per message to use gzip on the payload
const UseGzipThresholdSamples = 4096

// Dataset defines lists of variables to be encoded
type Dataset struct {
	Int32s []int32
	// can extend with other data types
}

// DatasetWithQuality defines lists of decoded variables with a timestamp and quality
type DatasetWithQuality struct {
	T      uint64
	Int32s []int32
	Q      []uint32
}

type qualityHistory struct {
	value   uint32
	samples uint32
}

func createSpatialRefs(count int, countV int, countI int, includeNeutral bool) []int {
	refs := make([]int, count)
	for i := range refs {
		refs[i] = -1
	}

	inc := 3
	if includeNeutral {
		inc = 4
	}

	for i := range refs {
		if i >= inc {
			if i < countV*inc {
				refs[i] = i - inc
			} else if i >= (countV+1)*inc && i < (countV+countI)*inc {
				refs[i] = i - inc
			}
		}
	}
	// fmt.Println(refs)
	return refs
}

func getDeltaEncoding(samplingRate int) int {
	if samplingRate > 100000 {
		return HighDeltaEncodingLayers
	}

	return DefaultDeltaEncodingLayers
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// copied from encoding/binary/varint.go to provide 32-bit version to avoid casting
func uvarint32(buf []byte) (uint32, int) {
	var x uint32
	var s uint
	for i, b := range buf {
		if b < 0x80 {
			if i > 9 || i == 9 && b > 1 {
				return 0, -(i + 1) // overflow
			}
			return x | uint32(b)<<s, i + 1
		}
		x |= uint32(b&0x7f) << s
		s += 7
	}
	return 0, 0
}

func varint32(buf []byte) (int32, int) {
	ux, n := uvarint32(buf) // ok to continue in presence of error
	x := int32(ux >> 1)
	if ux&1 != 0 {
		x = ^x
	}
	return x, n
}

// PutUvarint encodes a uint64 into buf and returns the number of bytes written.
// If the buffer is too small, PutUvarint will panic.
func putUvarint32(buf []byte, x uint32) int {
	i := 0
	for x >= 0x80 {
		buf[i] = byte(x) | 0x80
		x >>= 7
		i++
	}
	buf[i] = byte(x)
	return i + 1
}

// putVarint encodes an int64 into buf and returns the number of bytes written.
// If the buffer is too small, putVarint will panic.
func putVarint32(buf []byte, x int32) int {
	ux := uint32(x) << 1
	if x < 0 {
		ux = ^ux
	}
	return putUvarint32(buf, ux)
}
