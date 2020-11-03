package streamprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/google/uuid"
)

// Dataset defines lists of variables to be encoded
type Dataset struct {
	Int32s []int32
	// can extend with other data types
}

// DatasetWithQuality defines lists of decoded variables with a timestamp and quality
type DatasetWithQuality struct {
	Iteration uint32 // TODO remove?
	T         uint64
	Int32s    []int32
	Q         []uint32
}

// Encoder defines a stream protocol instance
type Encoder struct {
	ID                 uuid.UUID
	samplingRate       int
	samplesPerPacket   int
	encodedSamples     int
	buf                []byte // TODO better to use bytes.Buffer?
	len                int
	dataLen            int
	Int32Count         int
	prevSamples        Dataset
	qualityHistory     [][]qualityHistory
	qualityOffsetBytes int
}

// Decoder defines a stream protocol instance for decoding
type Decoder struct {
	ID               uuid.UUID
	samplingRate     int
	samplesPerPacket int
	Int32Count       int
	Ch               chan DatasetWithQuality
}

type qualityHistory struct {
	value   uint32
	samples uint32
}

// NewEncoder creates a stream protocol encoder instance
func NewEncoder(ID uuid.UUID, int32Count int, samplingRate int, samplesPerPacket int) *Encoder {
	// estimate buffer space required
	headerSize := 36
	bufSize := headerSize + samplesPerPacket*int32Count*8 + int32Count*4

	s := &Encoder{
		ID:               ID,
		samplingRate:     samplingRate,
		samplesPerPacket: samplesPerPacket,
		buf:              make([]byte, bufSize),
		Int32Count:       int32Count,
	}
	s.prevSamples.Int32s = make([]int32, int32Count)

	s.qualityHistory = make([][]qualityHistory, int32Count)
	for i := range s.qualityHistory {
		s.qualityHistory[i] = []qualityHistory{{value: 0, samples: 0}}
	}

	return s
}

// NewDecoder creates a stream protocol decoder instance
func NewDecoder(ID uuid.UUID, int32Count int, samplingRate int) *Decoder {
	// estimate 1 second as suitable channel buffer capacity
	channelCapacity := samplingRate

	d := &Decoder{
		ID:           ID,
		Int32Count:   int32Count,
		samplingRate: samplingRate,
		Ch:           make(chan DatasetWithQuality, channelCapacity),
	}

	return d
}

// DecodeToChannel decodes a stream protocol message
func (s *Decoder) DecodeToChannel(buf []byte, totalLength int) error {
	length := 16
	var lenQuality uint32 = 0
	var quality [][]qualityHistory = make([][]qualityHistory, s.Int32Count)
	for i := range quality {
		quality[i] = []qualityHistory{{value: 0, samples: 0}}
	}

	// check ID
	res := bytes.Compare(buf[:length], s.ID[:])
	if res != 0 {
		// fmt.Println("ID no match", buf[:length], s.ID[:])
		return errors.New("IDs did not match")
	}

	// decode timestamp
	startTime := binary.BigEndian.Uint64(buf[length:])
	length += 8

	// decode sampling rate
	// samplingRate := binary.BigEndian.Uint32(buf[length:])	// TODO restore for calculating timestamps
	length += 4

	// decode number of data samples
	samplesPerPacket := binary.BigEndian.Uint32(buf[length:])
	length += 4

	// decode quality offset
	qualityOffset := binary.BigEndian.Uint32(buf[length:])
	length += 4

	// initialise output structure
	var data DatasetWithQuality = DatasetWithQuality{
		Iteration: 0,
		T:         startTime,
		Int32s:    make([]int32, s.Int32Count),
		Q:         make([]uint32, s.Int32Count),
	}

	// get first samples
	for i := 0; i < s.Int32Count; i++ {
		data.Int32s[i] = int32(binary.BigEndian.Uint32(buf[length:]))
		length += 4
	}

	// populate quality structure
	lenQuality = qualityOffset
	for i := 0; i < s.Int32Count; i++ {
		val, lenB := binary.Uvarint(buf[lenQuality:])
		// quality[i][0].value = binary.BigEndian.Uint32(buf[lenQuality:])
		quality[i][0].value = uint32(val)
		lenQuality += uint32(lenB)
		val, lenB = binary.Uvarint(buf[lenQuality:])
		// quality[i][0].samples = binary.BigEndian.Uint32(buf[lenQuality:])
		quality[i][0].samples = uint32(val)
		lenQuality += uint32(lenB)
		if quality[i][0].samples != 0 {
			// decode each quality change and store in structure
			totalSamples := quality[i][0].samples
			for j := 1; ; j++ {
				// create and populate new array item
				quality[i] = append(quality[i], qualityHistory{value: 0, samples: 0})
				// fmt.Println(i, j, len(quality[i]), "totalSamples", totalSamples, "length:", lenQuality, totalLength)
				// quality[i][j].value = binary.BigEndian.Uint32(buf[lenQuality:])
				// lenQuality += 4
				// quality[i][j].samples = binary.BigEndian.Uint32(buf[lenQuality:])
				// lenQuality += 4
				val, lenB := binary.Uvarint(buf[lenQuality:])
				quality[i][j].value = uint32(val)
				lenQuality += uint32(lenB)
				val, lenB = binary.Uvarint(buf[lenQuality:])
				quality[i][j].samples = uint32(val)
				lenQuality += uint32(lenB)

				totalSamples += quality[i][j].samples
				if totalSamples >= samplesPerPacket {
					break
				}
			}
		}
	}

	// extract first quality value
	for i := 0; i < s.Int32Count; i++ {
		data.Q[i] = quality[i][0].value
	}

	// send first set of samples
	// slices within struct must be copied into new memory
	// fmt.Println("  value before sending on ch:", data.Int32s[0])
	dataCopy := DatasetWithQuality{
		Iteration: 0,
		T:         data.T,
		Int32s:    make([]int32, s.Int32Count),
		Q:         make([]uint32, s.Int32Count),
	}
	copy(dataCopy.Int32s, data.Int32s)
	copy(dataCopy.Q, data.Q)
	s.Ch <- dataCopy
	// time.Sleep(time.Millisecond * 1)
	// data.Int32s[0] = -42

	if samplesPerPacket == 1 {
		// fmt.Println("done decoding message", data.T, samplesPerPacket, length, totalLength, qualityOffset)
		// fmt.Println("encoded:", buf[:], len(buf))
		return nil
	}

	// loop through remaining samples
	var totalSamples uint32 = 1
	var prevData DatasetWithQuality = DatasetWithQuality{}
	for {
		// TODO check IEC 61850 64-bit time spec
		// TODO calculate T from samplingRate and totalSamples)
		//      UtcTime<SS SS SS SS QQ MM MM MM>
		//      -7-2: 6.1.2.9 TimeStamp type
		//      The UtcTime type shall be an OCTET STRING of length eight (8) octets. The value shall be encoded as defined in RFC 1305.
		data.Iteration = totalSamples
		data.T = startTime + uint64(totalSamples)
		// if data.T >= uint64(s.samplingRate) {
		// 	data.T = data.T - uint64(s.samplingRate)
		// }
		for i := 0; i < s.Int32Count; i++ {
			diff, lenB := binary.Varint(buf[length:])
			// fmt.Println("  decoded diff:", diff, lenB)
			// fmt.Println("    prev value:", data.Int32s[i], "new value:", data.Int32s[i]+int32(diff))
			length += lenB
			data.Int32s[i] = data.Int32s[i] + int32(diff)
			data.Q[i] = getQualityFromHistory(&quality[i], totalSamples)
		}

		// copy and send data on channel
		dataCopy := DatasetWithQuality{
			Iteration: data.Iteration,
			T:         data.T,
			Int32s:    make([]int32, s.Int32Count),
			Q:         make([]uint32, s.Int32Count),
		}
		copy(dataCopy.Int32s, data.Int32s)
		copy(dataCopy.Q, data.Q)
		s.Ch <- dataCopy

		totalSamples++
		if totalSamples >= samplesPerPacket {
			// fmt.Println("done decoding message", data.T, totalSamples, samplesPerPacket, length, totalLength, qualityOffset)
			// fmt.Println("encoded:", buf[:], len(buf))
			return nil
		}

		prevData.T = data.T
		copy(prevData.Int32s, data.Int32s)
		copy(prevData.Q, data.Q)
	}
}

func getQualityFromHistory(q *[]qualityHistory, sample uint32) uint32 {
	// TODO
	return 0
}

// Encode encodes the next set of samples
func (s *Encoder) Encode(data Dataset, q []uint32, t uint64) ([]byte, int) {
	// TODO refactor to use DatasetWithQuality

	if s.encodedSamples == 0 {
		s.len = 0
		s.len += copy(s.buf[s.len:], s.ID[:])

		// encode timestamp
		binary.BigEndian.PutUint64(s.buf[s.len:], t)
		s.len += 8

		// encode sampling rate
		binary.BigEndian.PutUint32(s.buf[s.len:], uint32(s.samplingRate))
		s.len += 4

		// TODO should encode number of variables, so that wireshark, etc, can interpret

		// encode number of data samples
		binary.BigEndian.PutUint32(s.buf[s.len:], uint32(s.samplesPerPacket))
		s.len += 4

		// reserve space for the quality section offset
		// need to encode offset to quality section to decode synchronously with data
		s.qualityOffsetBytes = s.len
		s.len += 4

		// encode first set of values
		for i := range data.Int32s {
			binary.BigEndian.PutUint32(s.buf[s.len:], uint32(data.Int32s[i]))
			s.len += 4
			// save previous value
			s.prevSamples.Int32s[i] = data.Int32s[i]
			// fmt.Println("  encode first set of values:", s.len)
		}

		// record first set of quality
		for i := range q {
			s.qualityHistory[i][0].value = q[i]
			s.qualityHistory[i][0].samples = 1
		}
	} else {
		for i := range data.Int32s {
			var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
			// fmt.Println("values:", s.encodedSamples, data.Int32s[i], s.prevSamples.Int32s[i], diff)
			lenB := binary.PutVarint(s.buf[s.len:], int64(diff))
			// lenB := varInt32(s.buf[s.len:], diff)
			// s.dataLen += lenB
			s.len += lenB
			// save previous value
			s.prevSamples.Int32s[i] = data.Int32s[i]
		}
		for i := range q {
			if s.qualityHistory[i][len(s.qualityHistory[i])-1].value == q[i] {
				s.qualityHistory[i][len(s.qualityHistory[i])-1].samples++
			} else {
				s.qualityHistory[i] = append(s.qualityHistory[i], qualityHistory{value: q[i], samples: 1})
			}
		}
	}

	s.encodedSamples++
	if s.encodedSamples >= s.samplesPerPacket {
		// encode the start offset of the quality section in the header
		binary.BigEndian.PutUint32(s.buf[s.qualityOffsetBytes:], uint32(s.len))

		// encode final quality values using RLE
		for i := range q {
			if len(s.qualityHistory[i]) == 1 {
				// special case, no change in quality value (encode samples as 0)
				// binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][0].value)
				lenB := binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][0].value))
				s.len += lenB
				lenB = binary.PutUvarint(s.buf[s.len:], 0)
				// binary.BigEndian.PutUint32(s.buf[s.len:], 0)
				s.len += lenB
			} else {
				// otherwise, encode each value
				// fmt.Println("encode multiple Q values", len(s.qualityHistory[i]))
				for j := range s.qualityHistory[i] {
					lenB := binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][0].value))
					s.len += lenB
					lenB = binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][j].samples))
					s.len += lenB
					// binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][j].value)
					// s.len += 4
					// binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][j].samples)
					// s.len += 4
				}
			}
		}

		// reset quality history
		for i := range s.qualityHistory {
			s.qualityHistory[i] = []qualityHistory{{value: 0, samples: 0}}
		}

		// // reset previous values
		// for i := range s.prevSamples.Int32s {
		// 	s.prevSamples.Int32s[i] = 0
		// }

		// inputData := float64(s.samplesPerPacket * s.Int32Count * 8)
		// efficiency := float64(s.dataLen) / inputData
		// fmt.Println(s.encodedSamples, "samples,", s.len, s.dataLen, efficiency, s.totalTime.Microseconds(), "µs")
		// fmt.Println(s.buf[0:s.len])

		finalLen := s.len

		s.qualityOffsetBytes = 0
		s.encodedSamples = 0
		s.len = 0
		s.dataLen = 0

		// send data
		return s.buf[0:finalLen], finalLen
	}

	return nil, 0
}

// // modified from https://techoverflow.net/blog/2013/01/25/efficiently-encoding-variable-length-integers-in-cc/
// func varInt32(output []byte, value int32) int {
// 	var value2 uint32 = (uint32(value) >> 31) ^ (uint32(value) << 1)
// 	var outputSize int = 0
// 	// while more than 7 bits of data are left, occupy the last output byte
// 	// and set the next byte flag
// 	for value2 > 127 {
// 		// |128: Set the next byte flag
// 		output[outputSize] = (uint8(value2)) | 128
// 		// remove the seven bits we just wrote
// 		value2 >>= 7
// 		outputSize++
// 	}
// 	outputSize++
// 	output[outputSize] = (uint8(value2)) & 127
// 	return outputSize
// }

// var sevenbits = [...]byte{
// 	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
// 	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
// 	0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
// 	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
// 	0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
// 	0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
// 	0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f,
// 	0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f,
// }

// func appendSleb128(b []byte, v int64) []byte {
// 	// If it's less than or equal to 7-bit
// 	if v >= 0 && v <= 0x3f {
// 		return append(b, sevenbits[v])
// 	} else if v < 0 && v >= ^0x3f {
// 		return append(b, sevenbits[0x80+v])
// 	}

// 	for {
// 		c := uint8(v & 0x7f)
// 		s := uint8(v & 0x40)
// 		v >>= 7

// 		if (v != -1 || s == 0) && (v != 0 || s != 0) {
// 			c |= 0x80
// 		}

// 		b = append(b, c)

// 		if c&0x80 == 0 {
// 			break
// 		}
// 	}

// 	return b
// }
