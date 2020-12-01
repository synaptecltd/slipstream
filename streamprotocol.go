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

// Simple8bThresholdSamples defines the number of samples per message required before using simple-8b encoding
const Simple8bThresholdSamples = 16

// DeltaEncodingLayers defines the number of layers of delta encoding. 0 is no delta encoding (just use varint), 1 is delta encoding, etc.
const DeltaEncodingLayers = 4

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

// TODO can make generic n-delta encoding?
// TODO need to modify decoder to allow dynamic sizes
// TODO need to adapt to only encode data up to s.encodedSamples, not full range of s.diffs
// TODO should zero out diffs values
// TODO add mutex to encoder (and decoder?) function so can run as goroutine - done for encoder

// Encoder defines a stream protocol instance
type Encoder struct {
	ID                uuid.UUID
	SamplingRate      int
	SamplesPerMessage int
	Int32Count        int
	buf               []byte
	bufA              []byte
	bufB              []byte
	useBufA           bool
	len               int
	encodedSamples    int

	prevData    []Dataset
	prevSamples Dataset
	prevDelta   Dataset
	prevDelta2  Dataset
	prevDelta3  Dataset

	qualityHistory [][]qualityHistory
	usingSimple8b  bool
	diffs          [][]uint64
	values         [][]int32
	simple8bValues []uint64
	mutex          sync.Mutex
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

	deltaSum [][]int32

	delta2Sum     []int32
	delta3Sum     []int32
	delta4Sum     []int32
	usingSimple8b bool
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
		bufA:              make([]byte, bufSize),
		bufB:              make([]byte, bufSize),
		Int32Count:        int32Count,
		simple8bValues:    make([]uint64, samplesPerMessage),
	}

	// initialise ping-pong buffer
	s.useBufA = true
	s.buf = s.bufA

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
	s.prevData = make([]Dataset, DeltaEncodingLayers)
	for i := range s.prevData {
		s.prevData[i].Int32s = make([]int32, int32Count)
	}

	// TODO old versions to remove
	s.prevSamples.Int32s = make([]int32, int32Count)
	s.prevDelta.Int32s = make([]int32, int32Count)
	s.prevDelta2.Int32s = make([]int32, int32Count)
	s.prevDelta3.Int32s = make([]int32, int32Count)

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
		delta2Sum:         make([]int32, int32Count),
		delta3Sum:         make([]int32, int32Count),
		delta4Sum:         make([]int32, int32Count),
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

// DecodeToBuffer decodes to a pre-allocated buffer
func (s *Decoder) DecodeToBuffer(buf []byte, totalLength int) error {
	var length int = 16
	var valSigned int32 = 0
	var valUnsigned uint32 = 0
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
	valSigned, lenB = varint32(buf[length:])
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
			decodedValue := int32(bitops.ZigZagDecode64(v))

			if indexTs > 0 {
				s.Out[indexTs].T = uint64(indexTs)
			}

			// delta-delta decoding
			if indexTs == 0 {
				s.delta2Sum[indexVariable] = 0
				s.Out[indexTs].Int32s[indexVariable] = decodedValue
			} else if indexTs == 1 {
				s.delta2Sum[indexVariable] += decodedValue
				s.Out[indexTs].Int32s[indexVariable] = s.Out[indexTs-1].Int32s[indexVariable] + s.delta2Sum[indexVariable]
			} else if indexTs == 2 {
				s.delta3Sum[indexVariable] += decodedValue
				s.delta2Sum[indexVariable] += s.delta3Sum[indexVariable]
				s.Out[indexTs].Int32s[indexVariable] = s.Out[indexTs-1].Int32s[indexVariable] + s.delta2Sum[indexVariable]
			} else {
				s.delta4Sum[indexVariable] += decodedValue
				s.delta3Sum[indexVariable] += s.delta4Sum[indexVariable]
				s.delta2Sum[indexVariable] += s.delta3Sum[indexVariable]
				s.Out[indexTs].Int32s[indexVariable] = s.Out[indexTs-1].Int32s[indexVariable] + s.delta2Sum[indexVariable]
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
			valSigned, lenB = varint32(buf[length:])
			s.Out[0].Int32s[i] = int32(valSigned)
			length += lenB
			s.delta2Sum[i] = 0 // TODO don't need if zeroing at end
		}

		// decode remaining delta-delta encoded values
		if s.samplesPerMessage > 1 {
			var totalSamples int = 1
			for {
				// encode the sample number relative to the starting timestamp
				s.Out[totalSamples].T = uint64(totalSamples)

				for i := 0; i < s.Int32Count; i++ {
					decodedValue, lenB := varint32(buf[length:])
					length += lenB
					if totalSamples == 1 {
						s.delta2Sum[i] += decodedValue
					} else if totalSamples == 2 {
						s.delta3Sum[i] += decodedValue
						s.delta2Sum[i] += s.delta3Sum[i]
					} else {
						s.delta4Sum[i] += decodedValue
						s.delta3Sum[i] += s.delta4Sum[i]
						s.delta2Sum[i] += s.delta3Sum[i]
					}
					s.Out[totalSamples].Int32s[i] = s.Out[totalSamples-1].Int32s[i] + s.delta2Sum[i]
				}
				totalSamples++

				if totalSamples >= s.samplesPerMessage {
					break
				}
			}
		}
	}

	// populate quality structure
	for i := 0; i < s.Int32Count; i++ {
		sampleNumber := 0
		for sampleNumber < s.samplesPerMessage {
			valUnsigned, lenB = uvarint32(buf[length:])
			length += lenB
			s.Out[sampleNumber].Q[i] = uint32(valUnsigned)

			valUnsigned, lenB = uvarint32(buf[length:])
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

	for i := 0; i < s.Int32Count; i++ {
		s.delta2Sum[i] = 0
		s.delta3Sum[i] = 0
		s.delta4Sum[i] = 0
	}

	return nil
}

func (s *Encoder) encodeSingleSample(index int, value int32) {
	if s.usingSimple8b {
		s.diffs[index][s.encodedSamples] = bitops.ZigZagEncode64(int64(value))
	} else {
		s.values[s.encodedSamples][index] = value
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
		// for i := range data.Int32s {
		// if s.usingSimple8b {
		// 	s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(data.Int32s[i]))
		// } else {
		// 	// s.len += putVarint32(s.buf[s.len:], int64(data.Int32s[i]))
		// 	s.values[s.encodedSamples][i] = data.Int32s[i]
		// }

		// // save previous value
		// // s.prevSamples.Int32s[i] = data.Int32s[i] // TODO old
		// s.prevData[0].Int32s[i] = data.Int32s[i]
		// }

		// record first set of quality
		for i := range data.Q {
			s.qualityHistory[i][0].value = data.Q[i]
			s.qualityHistory[i][0].samples = 1
		}
	} else {
		// write the next quality value
		for i := range data.Q {
			if s.qualityHistory[i][len(s.qualityHistory[i])-1].value == data.Q[i] {
				s.qualityHistory[i][len(s.qualityHistory[i])-1].samples++
			} else {
				s.qualityHistory[i] = append(s.qualityHistory[i], qualityHistory{value: data.Q[i], samples: 1})
			}
		}
	}

	for i := range data.Int32s {
		// if s.encodedSamples < len(s.prevData) {
		// 	// special cases for delta encoding
		// } else {
		// 	// remaining samples for delta coding
		// }

		deltaN := make([]int32, DeltaEncodingLayers)

		j := s.encodedSamples

		// prepare data for delta encoding
		if j > 0 {
			deltaN[0] = data.Int32s[i] - s.prevData[0].Int32s[i]
		}
		// for k := min(j, DeltaEncodingLayers); k >= 2; k-- {
		for k := 1; k < min(j, DeltaEncodingLayers); k++ {
			deltaN[k] = deltaN[k-1] - s.prevData[k].Int32s[i]
		}

		if j == 0 {
			s.encodeSingleSample(i, data.Int32s[i])
		} else {
			s.encodeSingleSample(i, deltaN[min(j-1, DeltaEncodingLayers-1)])
		}

		// if j == 2 {
		// 	fmt.Println(j, i, data.Int32s[i])
		// 	// fmt.Println(s.prevData)
		// }

		// if j == 0 {
		// 	s.encodeSingleSample(i, data.Int32s[i])
		// } else if j == 1 {
		// 	s.encodeSingleSample(i, deltaN[j-1])
		// } else if j == 2 {
		// 	// deltaN[j-1] = deltaN[j-2] - s.prevData[j-1].Int32s[i]

		// 	s.encodeSingleSample(i, deltaN[j-1])
		// } else if j == 3 {
		// 	// deltaN[j-2] = deltaN[j-3] - s.prevData[j-2].Int32s[i]
		// 	// deltaN[j-1] = deltaN[j-2] - s.prevData[j-1].Int32s[i]

		// 	s.encodeSingleSample(i, deltaN[j-1])
		// } else {
		// 	// deltaN[j-3] = deltaN[j-4] - s.prevData[j-3].Int32s[i]
		// 	// deltaN[j-2] = deltaN[j-3] - s.prevData[j-2].Int32s[i]
		// 	// deltaN[j-1] = deltaN[j-2] - s.prevData[j-1].Int32s[i]

		// 	s.encodeSingleSample(i, deltaN[j-1])
		// }

		// save samples and deltas for next iteration
		s.prevData[0].Int32s[i] = data.Int32s[i]
		for k := 1; k <= min(j, DeltaEncodingLayers-1); k++ {
			s.prevData[k].Int32s[i] = deltaN[k-1]
		}

		// if s.encodedSamples == 1 {
		// 	// var delta int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
		// 	var delta int32 = data.Int32s[i] - s.prevData[0].Int32s[i]

		// 	if s.usingSimple8b {
		// 		s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(delta))
		// 	} else {
		// 		// s.len += putVarint32(s.buf[s.len:], int64(diff))
		// 		s.values[s.encodedSamples][i] = delta
		// 	}

		// 	// save previous value
		// 	// s.prevSamples.Int32s[i] = data.Int32s[i]
		// 	s.prevData[0].Int32s[i] = data.Int32s[i]
		// 	s.prevDelta.Int32s[i] = delta
		// 	// s.prevDelta2.Int32s[i] = delta
		// } else if s.encodedSamples == 2 {
		// 	// delta-delta encoding
		// 	// var delta int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
		// 	var delta int32 = data.Int32s[i] - s.prevData[0].Int32s[i]
		// 	var delta2 int32 = delta - s.prevDelta.Int32s[i]

		// 	if s.usingSimple8b {
		// 		s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(delta2))
		// 	} else {
		// 		// s.len += putVarint32(s.buf[s.len:], int64(diff2))
		// 		s.values[s.encodedSamples][i] = delta2
		// 	}

		// 	// save previous value
		// 	// s.prevSamples.Int32s[i] = data.Int32s[i]
		// 	s.prevData[0].Int32s[i] = data.Int32s[i]
		// 	s.prevDelta.Int32s[i] = delta
		// 	s.prevDelta2.Int32s[i] = delta2
		// } else if s.encodedSamples == 3 {
		// 	// delta-delta encoding
		// 	// var delta int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
		// 	var delta int32 = data.Int32s[i] - s.prevData[0].Int32s[i]
		// 	var delta2 int32 = delta - s.prevDelta.Int32s[i]
		// 	var delta3 int32 = delta2 - s.prevDelta2.Int32s[i]

		// 	if s.usingSimple8b {
		// 		s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(delta3))
		// 	} else {
		// 		// s.len += putVarint32(s.buf[s.len:], int64(diff2))
		// 		s.values[s.encodedSamples][i] = delta3
		// 	}

		// 	// save previous value
		// 	// s.prevSamples.Int32s[i] = data.Int32s[i]
		// 	s.prevData[0].Int32s[i] = data.Int32s[i]
		// 	s.prevDelta.Int32s[i] = delta
		// 	s.prevDelta2.Int32s[i] = delta2
		// 	s.prevDelta3.Int32s[i] = delta3
		// } else {
		// 	// delta-delta encoding
		// 	// var delta int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
		// 	var delta int32 = data.Int32s[i] - s.prevData[0].Int32s[i]
		// 	var delta2 int32 = delta - s.prevDelta.Int32s[i]
		// 	var delta3 int32 = delta2 - s.prevDelta2.Int32s[i]
		// 	var delta4 int32 = delta3 - s.prevDelta3.Int32s[i]

		// 	if s.usingSimple8b {
		// 		s.diffs[i][s.encodedSamples] = bitops.ZigZagEncode64(int64(delta4))
		// 	} else {
		// 		// s.len += putVarint32(s.buf[s.len:], int64(diff2))
		// 		s.values[s.encodedSamples][i] = delta4
		// 	}

		// 	// save previous value
		// 	s.prevData[0].Int32s[i] = data.Int32s[i]
		// 	// s.prevSamples.Int32s[i] = data.Int32s[i]
		// 	s.prevDelta.Int32s[i] = delta
		// 	s.prevDelta2.Int32s[i] = delta2
		// 	s.prevDelta3.Int32s[i] = delta3
		// }
	}

	s.encodedSamples++
	if s.encodedSamples >= s.SamplesPerMessage {
		// fmt.Println("end encode", s.encodedSamples, s.SamplesPerMessage)
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
	// check encoder is in the correct state for writing
	if s.encodedSamples < s.SamplesPerMessage {
		return nil, 0, nil
	}

	// write encoded samples
	s.len += putVarint32(s.buf[s.len:], int32(s.encodedSamples))

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
				s.len += putVarint32(s.buf[s.len:], s.values[i][j])
			}
		}
	}

	// encode final quality values using RLE
	for i := range s.qualityHistory {
		// override final number of samples to zero
		s.qualityHistory[i][len(s.qualityHistory[i])-1].samples = 0

		// otherwise, encode each value
		for j := range s.qualityHistory[i] {
			s.len += putUvarint32(s.buf[s.len:], s.qualityHistory[i][j].value)
			s.len += putUvarint32(s.buf[s.len:], s.qualityHistory[i][j].samples)
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

	// send data and swap ping-pong buffer
	if s.useBufA {
		s.useBufA = false
		s.buf = s.bufB
		return s.bufA[0:finalLen], finalLen, nil
	}

	s.useBufA = true
	s.buf = s.bufA
	return s.bufB[0:finalLen], finalLen, nil
}
