package streamprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/stevenblair/encoding/bitops"
	"github.com/stevenblair/encoding/simple8b"
)

// Simple8bThresholdSamples define the number of samples per message required before using simple-8b encoding
const Simple8bThresholdSamples = 16

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

// TODO need to modify decoder to allow dynamic sizes
// TODO need to adapt to only encode data up to s.encodedSamples, not full range of s.diffs
// TODO should zero out diffs values
// TODO add mutex to encoder (and decoder?) function so can run as goroutine - done for encoder
// TODO explore option of gzip (or other) for large files
// TODO use 32-bit varint functions
// TODO no point copying bytes for .T after first sample

// Encoder defines a stream protocol instance
type Encoder struct {
	ID                uuid.UUID
	SamplingRate      int
	SamplesPerMessage int
	Int32Count        int
	buf               []byte
	len               int
	encodedSamples    int
	prevSamples       Dataset
	prevDiff          Dataset
	qualityHistory    [][]qualityHistory
	usingSimple8b     bool
	diffs             [][]uint64
	values            [][]int32
	simple8bValues    []uint64
	mutex             sync.Mutex
}

// Decoder defines a stream protocol instance for decoding
type Decoder struct {
	ID                uuid.UUID
	samplingRate      int
	samplesPerMessage int
	encodedSamples    int
	Int32Count        int
	Out               []DatasetWithQuality
	startTimestamp    uint64
	deltaDeltaSum     []int64
	usingSimple8b     bool
}

// NewEncoder creates a stream protocol encoder instance
func NewEncoder(ID uuid.UUID, int32Count int, samplingRate int, samplesPerMessage int) *Encoder {
	// estimate maximum buffer space required
	headerSize := 36
	bufSize := headerSize + samplesPerMessage*int32Count*8 + int32Count*4

	s := &Encoder{
		ID:                ID,
		SamplingRate:      samplingRate,
		SamplesPerMessage: samplesPerMessage,
		buf:               make([]byte, bufSize),
		Int32Count:        int32Count,
		simple8bValues:    make([]uint64, samplesPerMessage),
	}

	if samplesPerMessage > Simple8bThresholdSamples {
		s.usingSimple8b = true
		s.diffs = make([][]uint64, int32Count)
		for i := range s.diffs {
			s.diffs[i] = make([]uint64, samplesPerMessage)
		}
	} else {
		s.values = make([][]int32, samplesPerMessage)
		for i := range s.values {
			s.values[i] = make([]int32, int32Count)
		}
	}

	// storage for delta-delta encoding
	s.prevSamples.Int32s = make([]int32, int32Count)
	s.prevDiff.Int32s = make([]int32, int32Count)

	s.qualityHistory = make([][]qualityHistory, int32Count)
	for i := range s.qualityHistory {
		// set capacity to avoid some possible allocations during encoding
		s.qualityHistory[i] = make([]qualityHistory, 1, 16)
		s.qualityHistory[i][0].value = 0
		s.qualityHistory[i][0].samples = 0
	}

	return s
}

// NewDecoder creates a stream protocol decoder instance for pre-allocated output
func NewDecoder(ID uuid.UUID, int32Count int, samplingRate int, samplesPerMessage int) *Decoder {
	d := &Decoder{
		ID:                ID,
		Int32Count:        int32Count,
		samplingRate:      samplingRate,
		samplesPerMessage: samplesPerMessage,
		Out:               make([]DatasetWithQuality, samplesPerMessage),
		deltaDeltaSum:     make([]int64, int32Count),
	}

	if samplesPerMessage > Simple8bThresholdSamples {
		d.usingSimple8b = true
	}

	// initialise each set of outputs in data stucture
	for i := range d.Out {
		d.Out[i].Int32s = make([]int32, int32Count)
		d.Out[i].Q = make([]uint32, int32Count)
	}

	return d
}

// DecodeToBuffer decodes to a pre-allocated buffer
func (s *Decoder) DecodeToBuffer(buf []byte, totalLength int) error {
	var length int = 16
	var valSigned int64 = 0
	var valUnsigned uint64 = 0
	var lenB int = 0

	// check ID
	res := bytes.Compare(buf[:length], s.ID[:])
	if res != 0 {
		return errors.New("IDs did not match")
	}

	// decode timestamp
	s.startTimestamp = binary.BigEndian.Uint64(buf[length:])
	length += 8

	// the first timestamp is the starting value encoded in the header
	s.Out[0].T = s.startTimestamp

	// decode number of samples
	valSigned, lenB = binary.Varint(buf[length:])
	s.encodedSamples = int(valSigned)
	length += lenB

	if s.usingSimple8b {
		// for simple-8b encoding, iterate through every value
		decodeCounter := 0
		indexTs := 0
		indexVariable := 0

		decodedUnit64s, _ := simple8b.ForEach(buf[length:], func(v uint64) bool {
			// manage 2D slice indices
			indexTs = decodeCounter % s.samplesPerMessage
			if decodeCounter > 0 && indexTs == 0 {
				indexVariable++
			}

			// get signed value back with zig-zag decoding
			decodedValue := bitops.ZigZagDecode64(v)

			// delta-delta decoding
			if indexTs == 0 {
				s.Out[indexTs].Int32s[indexVariable] = int32(decodedValue)
				s.deltaDeltaSum[indexVariable] = 0
			} else {
				s.Out[indexTs].T = uint64(indexTs)
				s.deltaDeltaSum[indexVariable] += decodedValue
				s.Out[indexTs].Int32s[indexVariable] = s.Out[indexTs-1].Int32s[indexVariable] + int32(s.deltaDeltaSum[indexVariable])
			}

			decodeCounter++

			// all variables and timesteps have been decoded
			if decodeCounter == s.samplesPerMessage*s.Int32Count {
				// stop decoding
				return false
			}

			return true
		})

		// add length of decoded unit64 blocks (8 bytes each)
		length += decodedUnit64s * 8
	} else {
		// get first set of samples using delta-delta encoding
		for i := 0; i < s.Int32Count; i++ {
			valSigned, lenB = binary.Varint(buf[length:])
			s.Out[0].Int32s[i] = int32(valSigned)
			length += lenB
			s.deltaDeltaSum[i] = 0
		}
	}

	// decode remaining delta-delta encoded values
	var totalSamples int = 1
	if s.samplesPerMessage > 1 {
		for {
			// encode the sample number relative to the starting timestamp
			s.Out[totalSamples].T = uint64(totalSamples)

			if !s.usingSimple8b {
				for i := 0; i < s.Int32Count; i++ {
					diff, lenB := binary.Varint(buf[length:])
					s.deltaDeltaSum[i] = s.deltaDeltaSum[i] + diff
					length += lenB
					s.Out[totalSamples].Int32s[i] = s.Out[totalSamples-1].Int32s[i] + int32(s.deltaDeltaSum[i])
				}
			}
			totalSamples++

			if totalSamples >= s.samplesPerMessage {
				break
			}
		}
	}

	// populate quality structure
	for i := 0; i < s.Int32Count; i++ {
		sampleNumber := 0
		for sampleNumber < s.samplesPerMessage {
			valUnsigned, lenB = binary.Uvarint(buf[length:])
			length += lenB
			s.Out[sampleNumber].Q[i] = uint32(valUnsigned)

			valUnsigned, lenB = binary.Uvarint(buf[length:])
			length += lenB

			if valUnsigned == 0 {
				// write all remaining Q values for this variable
				for j := sampleNumber + 1; j < len(s.Out); j++ {
					s.Out[j].Q[i] = s.Out[sampleNumber].Q[i]
				}
				sampleNumber = s.samplesPerMessage
			} else {
				// write up to valUnsigned remaining Q values for this variable
				for j := sampleNumber + 1; j < int(valUnsigned); j++ {
					s.Out[j].Q[i] = s.Out[sampleNumber].Q[i]
				}
				sampleNumber += int(valUnsigned)
			}
		}
	}

	return nil
}

// copied from encoding/binary/varint.go to provide 32-bit version which avoid casting
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

// Encode encodes the next set of samples. It is called iteratively until the pre-defined number of samples are provided.
func (s *Encoder) Encode(data *DatasetWithQuality) ([]byte, int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.encodedSamples == 0 {
		s.len = 0
		s.len += copy(s.buf[s.len:], s.ID[:])

		// encode timestamp
		binary.BigEndian.PutUint64(s.buf[s.len:], data.T)
		s.len += 8

		// encode first set of values
		// for simple-8b encoding, values must be saved in a data structure first, then encoded into 64-bit blocks later
		// for varint encoding, values can be directly written to the output buffer
		for i := range data.Int32s {
			if s.usingSimple8b {
				s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(data.Int32s[i]))
			} else {
				// s.len += binary.PutVarint(s.buf[s.len:], int64(data.Int32s[i]))
				s.values[s.encodedSamples][i] = data.Int32s[i]
			}

			// save previous value
			s.prevSamples.Int32s[i] = data.Int32s[i]
		}

		// record first set of quality
		for i := range data.Q {
			s.qualityHistory[i][0].value = data.Q[i]
			s.qualityHistory[i][0].samples = 1
		}
	} else {
		for i := range data.Int32s {
			if s.encodedSamples == 1 {
				var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i]

				if s.usingSimple8b {
					s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(diff))
				} else {
					// s.len += binary.PutVarint(s.buf[s.len:], int64(diff))
					s.values[s.encodedSamples][i] = diff
				}

				// save previous value
				s.prevSamples.Int32s[i] = data.Int32s[i]
				s.prevDiff.Int32s[i] = diff
			} else {
				// delta-delta encoding
				var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
				var diff2 int32 = diff - s.prevDiff.Int32s[i]

				if s.usingSimple8b {
					s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(diff2))
				} else {
					// s.len += binary.PutVarint(s.buf[s.len:], int64(diff2))
					s.values[s.encodedSamples][i] = diff2
				}

				// save previous value
				s.prevSamples.Int32s[i] = data.Int32s[i]
				s.prevDiff.Int32s[i] = diff
			}
		}

		// write the next quality value
		for i := range data.Q {
			if s.qualityHistory[i][len(s.qualityHistory[i])-1].value == data.Q[i] {
				s.qualityHistory[i][len(s.qualityHistory[i])-1].samples++
			} else {
				s.qualityHistory[i] = append(s.qualityHistory[i], qualityHistory{value: data.Q[i], samples: 1})
			}
		}
	}

	s.encodedSamples++
	if s.encodedSamples >= s.SamplesPerMessage {
		return s.endEncode()
	}

	return nil, 0, nil
}

// EndEncode ends the encoding early, and completes the buffer so far
func (s *Encoder) EndEncode() ([]byte, int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.endEncode()
}

// internal version does not need the mutex
func (s *Encoder) endEncode() ([]byte, int, error) {

	// write encoded samples
	s.len += binary.PutVarint(s.buf[s.len:], int64(s.encodedSamples))

	if s.usingSimple8b {
		for i := range s.diffs {
			numberOfSimple8b, _ := simple8b.EncodeAllRef(&s.simple8bValues, s.diffs[i])

			for j := 0; j < numberOfSimple8b; j++ {
				binary.BigEndian.PutUint64(s.buf[s.len:], s.simple8bValues[j])
				s.len += 8
			}
		}
	} else {
		for i := 0; i < s.encodedSamples; i++ {
			for j := 0; j < s.Int32Count; j++ {
				s.len += binary.PutVarint(s.buf[s.len:], int64(s.values[i][j]))
			}
		}
	}

	// encode final quality values using RLE
	for i := range s.qualityHistory {
		// override final number of samples to zero
		s.qualityHistory[i][len(s.qualityHistory[i])-1].samples = 0

		// otherwise, encode each value
		for j := range s.qualityHistory[i] {
			s.len += binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][j].value))
			s.len += binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][j].samples))
		}
	}

	// reset quality history
	for i := range s.qualityHistory {
		s.qualityHistory[i] = s.qualityHistory[i][:1]
		s.qualityHistory[i][0].value = 0
		s.qualityHistory[i][0].samples = 0
	}

	// reset previous values
	finalLen := s.len
	s.encodedSamples = 0
	s.len = 0

	// send data
	return s.buf[0:finalLen], finalLen, nil
}
