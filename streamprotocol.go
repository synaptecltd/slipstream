package streamprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/google/uuid"
)

// TODO add delta-delta coding

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

// Encoder defines a stream protocol instance
type Encoder struct {
	ID                 uuid.UUID
	samplingRate       int
	samplesPerPacket   int
	encodedSamples     int
	buf                []byte
	len                int
	HeaderBytes        int
	Int32Count         int
	prevSamples        Dataset
	qualityHistory     [][]qualityHistory
	qualityOffsetBytes int
}

// Decoder defines a stream protocol instance for decoding
type Decoder struct {
	ID               uuid.UUID
	samplingRate     int
	samplesPerPacket uint32
	Int32Count       int
	Ch               chan DatasetWithQuality
	Out              []DatasetWithQuality
	quality          [][]qualityHistory
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
		// set capacity to avoid some possible allocations during encoding
		s.qualityHistory[i] = make([]qualityHistory, 1, 8)
		s.qualityHistory[i][0].value = 0
		s.qualityHistory[i][0].samples = 0
	}

	return s
}

// NewDecoder creates a stream protocol decoder instance for pre-allocated output
func NewDecoder(ID uuid.UUID, int32Count int, samplingRate int) *Decoder {
	d := &Decoder{
		ID:           ID,
		Int32Count:   int32Count,
		samplingRate: samplingRate,
		Ch:           nil,
		Out:          make([]DatasetWithQuality, samplingRate),
		quality:      make([][]qualityHistory, int32Count),
	}

	// initialise each set of outputs in data stucture
	for i := range d.Out {
		d.Out[i].Int32s = make([]int32, int32Count)
		d.Out[i].Q = make([]uint32, int32Count)
	}

	for i := range d.quality {
		d.quality[i] = []qualityHistory{{value: 0, samples: 0}}
	}

	return d
}

// NewChannelDecoder creates a stream protocol decoder instance to sends each output to a channel
// func NewChannelDecoder(ID uuid.UUID, int32Count int, samplingRate int) *Decoder {
// 	// estimate 1 second as suitable channel buffer capacity
// 	channelCapacity := samplingRate

// 	d := &Decoder{
// 		ID:           ID,
// 		Int32Count:   int32Count,
// 		samplingRate: samplingRate,
// 		Ch:           make(chan DatasetWithQuality, channelCapacity),
// 		Out:          nil,
// 	}

// 	return d
// }

func (s *Decoder) decodeFirstSample(data *DatasetWithQuality, quality *[][]qualityHistory, buf []byte, bufStart int, qualityOffset uint32) int {
	var valSigned int64 = 0
	var valUnsigned uint64 = 0
	var lenB int = 0
	var lenQuality uint32 = qualityOffset
	var length int = bufStart

	// get first samples
	for i := 0; i < s.Int32Count; i++ {
		// data.Int32s[i] = int32(binary.BigEndian.Uint32(buf[length:]))
		valSigned, lenB = binary.Varint(buf[length:])
		data.Int32s[i] = int32(valSigned)
		length += lenB
	}

	// populate quality structure
	for i := 0; i < s.Int32Count; i++ {
		valUnsigned, lenB = binary.Uvarint(buf[lenQuality:])
		(*quality)[i][0].value = uint32(valUnsigned)
		lenQuality += uint32(lenB)
		valUnsigned, lenB = binary.Uvarint(buf[lenQuality:])
		(*quality)[i][0].samples = uint32(valUnsigned)
		lenQuality += uint32(lenB)
		// fmt.Println("  decoded:", quality[i][0].value, quality[i][0].samples)

		if (*quality)[i][0].samples != 0 {
			// fmt.Println("  quality[i][0].samples != 0", quality[i][0].value, quality[i][0].samples)
			// decode each quality change and store in structure
			totalSamples := (*quality)[i][0].samples
			for j := 1; ; j++ {
				// create and populate new array item
				(*quality)[i] = append((*quality)[i], qualityHistory{value: 0, samples: 0})

				// fmt.Println(i, j, len(quality[i]), "totalSamples", totalSamples, "length:", lenQuality, totalLength)
				valUnsigned, lenB = binary.Uvarint(buf[lenQuality:])
				(*quality)[i][j].value = uint32(valUnsigned)
				lenQuality += uint32(lenB)
				valUnsigned, lenB = binary.Uvarint(buf[lenQuality:])
				(*quality)[i][j].samples = uint32(valUnsigned)
				lenQuality += uint32(lenB)
				// fmt.Println("      decoded:", quality[i][j].value, quality[i][j].samples)

				totalSamples += (*quality)[i][j].samples
				if totalSamples >= s.samplesPerPacket || j+1 >= len((*quality)[i]) {
					break
				}
			}
		}
	}

	// extract first quality value
	for i := 0; i < s.Int32Count; i++ {
		data.Q[i] = (*quality)[i][0].value
	}

	return length - bufStart
}

// DecodeToChannel decodes a stream protocol message
// func (s *Decoder) DecodeToChannel(buf []byte, totalLength int) error {
// 	var length int = 16
// 	// var valSigned int64 = 0
// 	var valUnsigned uint64 = 0
// 	var lenB int = 0
// 	var quality [][]qualityHistory = make([][]qualityHistory, s.Int32Count)
// 	for i := range quality {
// 		quality[i] = []qualityHistory{{value: 0, samples: 0}}
// 	}

// 	// check ID
// 	res := bytes.Compare(buf[:length], s.ID[:])
// 	if res != 0 {
// 		// fmt.Println("ID no match", buf[:length], s.ID[:])
// 		return errors.New("IDs did not match")
// 	}

// 	// decode timestamp
// 	startTime := binary.BigEndian.Uint64(buf[length:])
// 	length += 8

// 	// decode sampling rate
// 	// samplingRate := binary.BigEndian.Uint32(buf[length:])
// 	valUnsigned, lenB = binary.Uvarint(buf[length:])
// 	samplingRate := uint32(valUnsigned)
// 	if samplingRate == 99999999 {
// 		fmt.Println("test")
// 	}
// 	length += lenB

// 	// decode number of data samples
// 	// samplesPerPacket := binary.BigEndian.Uint32(buf[length:])
// 	valUnsigned, lenB = binary.Uvarint(buf[length:])
// 	s.samplesPerPacket = uint32(valUnsigned)
// 	length += lenB
// 	// fmt.Println("samplesPerPacket:", samplesPerPacket, samplingRate)

// 	// decode quality offset
// 	qualityOffset := binary.BigEndian.Uint32(buf[length:])
// 	length += 4

// 	// initialise output structure
// 	var data DatasetWithQuality = DatasetWithQuality{
// 		T:      startTime,
// 		Int32s: make([]int32, s.Int32Count),
// 		Q:      make([]uint32, s.Int32Count),
// 	}

// 	length += s.decodeFirstSample(&data, &quality, buf, length, qualityOffset)

// 	// send first set of samples
// 	// slices within struct must be copied into new memory
// 	// fmt.Println("  value", data.Int32s[0], "length:", length)
// 	dataCopy := DatasetWithQuality{
// 		T:      data.T,
// 		Int32s: make([]int32, s.Int32Count),
// 		Q:      make([]uint32, s.Int32Count),
// 	}
// 	copy(dataCopy.Int32s, data.Int32s)
// 	copy(dataCopy.Q, data.Q)
// 	s.Ch <- dataCopy
// 	// time.Sleep(time.Millisecond * 1)
// 	// data.Int32s[0] = -42

// 	if s.samplesPerPacket == 1 {
// 		// fmt.Println("done decoding message", data.T, samplesPerPacket, length, totalLength, qualityOffset)
// 		// fmt.Println("encoded:", buf[:], len(buf))
// 		return nil
// 	}

// 	// loop through remaining samples
// 	var totalSamples uint32 = 1
// 	var prevData DatasetWithQuality = DatasetWithQuality{}
// 	for {
// 		//      UtcTime<SS SS SS SS QQ MM MM MM>
// 		//      -7-2: 6.1.2.9 TimeStamp type
// 		//      The UtcTime type shall be an OCTET STRING of length eight (8) octets. The value shall be encoded as defined in RFC 1305.
// 		data.T = startTime + uint64(totalSamples)
// 		// if data.T >= uint64(s.samplingRate) {
// 		// 	data.T = data.T - uint64(s.samplingRate)
// 		// }
// 		for i := 0; i < s.Int32Count; i++ {
// 			diff, lenB := binary.Varint(buf[length:])
// 			// fmt.Println("  decoded diff:", diff, lenB)
// 			// fmt.Println("    prev value:", data.Int32s[i], "new value:", data.Int32s[i]+int32(diff))
// 			length += lenB
// 			data.Int32s[i] = data.Int32s[i] + int32(diff)
// 			// if len(quality[i]) > 1 {
// 			// 	fmt.Println("Len > 1")
// 			// }
// 			data.Q[i], _ = getQualityFromHistory(&quality[i], totalSamples)
// 		}

// 		// copy and send data on channel
// 		dataCopy := DatasetWithQuality{
// 			T:      data.T,
// 			Int32s: make([]int32, s.Int32Count),
// 			Q:      make([]uint32, s.Int32Count),
// 		}
// 		copy(dataCopy.Int32s, data.Int32s)
// 		copy(dataCopy.Q, data.Q)
// 		s.Ch <- dataCopy

// 		totalSamples++
// 		if totalSamples >= s.samplesPerPacket {
// 			// fmt.Println("done decoding message", data.T, totalSamples, samplesPerPacket, length, totalLength, qualityOffset)
// 			// fmt.Println("encoded:", buf[:], len(buf))
// 			return nil
// 		}

// 		prevData.T = data.T
// 		copy(prevData.Int32s, data.Int32s)
// 		copy(prevData.Q, data.Q)
// 	}
// }

// DecodeToBuffer decodes to a pre-allocated buffer
func (s *Decoder) DecodeToBuffer(buf []byte, totalLength int) error {
	var length int = 16
	var valUnsigned uint64 = 0
	var lenB int = 0

	// TODO move to New() func
	// var quality [][]qualityHistory = make([][]qualityHistory, s.Int32Count)
	// for i := range quality {
	// 	quality[i] = []qualityHistory{{value: 0, samples: 0}}
	// }

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
	_, lenB = binary.Uvarint(buf[length:]) // TODO restore for calculating timestamps
	length += lenB

	// decode number of variables
	_, lenB = binary.Uvarint(buf[length:])
	length += lenB

	// decode number of data samples
	valUnsigned, lenB = binary.Uvarint(buf[length:])
	s.samplesPerPacket = uint32(valUnsigned)
	length += lenB
	// fmt.Println("samplesPerPacket:", samplesPerPacket, samplingRate)

	// decode quality offset
	qualityOffset := binary.BigEndian.Uint32(buf[length:])
	length += 4

	length += s.decodeFirstSample(&s.Out[0], &s.quality, buf, length, qualityOffset)

	if s.samplesPerPacket == 1 {
		// fmt.Println("done decoding message", data.T, samplesPerPacket, length, totalLength, qualityOffset)
		// fmt.Println("encoded:", buf[:], len(buf))
		return nil
	}

	// loop through remaining samples
	var totalSamples uint32 = 1
	var prevData DatasetWithQuality = DatasetWithQuality{}
	for {
		// TODO check IEC 61850 64-bit time spec
		// TODO check STTP 64-bit time spec
		// TODO calculate T from samplingRate and totalSamples)
		//      UtcTime<SS SS SS SS QQ MM MM MM>
		//      -7-2: 6.1.2.9 TimeStamp type
		//      The UtcTime type shall be an OCTET STRING of length eight (8) octets. The value shall be encoded as defined in RFC 1305.
		s.Out[totalSamples].T = startTime + uint64(totalSamples)
		// if data.T >= uint64(s.samplingRate) {
		// 	data.T = data.T - uint64(s.samplingRate)
		// }
		for i := 0; i < s.Int32Count; i++ {
			diff, lenB := binary.Varint(buf[length:])
			// fmt.Println("  decoded diff:", diff, lenB)
			// fmt.Println("    prev value:", data.Int32s[i], "new value:", data.Int32s[i]+int32(diff))
			length += lenB
			s.Out[totalSamples].Int32s[i] = s.Out[totalSamples].Int32s[i] + int32(diff)
			// if len(quality[i]) > 1 {
			// 	fmt.Println("Len > 1")
			// }
			s.Out[totalSamples].Q[i], _ = getQualityFromHistory(&s.quality[i], totalSamples)
		}

		prevData.T = s.Out[totalSamples].T
		copy(prevData.Int32s, s.Out[totalSamples].Int32s)
		copy(prevData.Q, s.Out[totalSamples].Q)

		totalSamples++
		if totalSamples >= s.samplesPerPacket {
			// fmt.Println("done decoding message", data.T, totalSamples, samplesPerPacket, length, totalLength, qualityOffset)
			// fmt.Println("encoded:", buf[:], len(buf))
			return nil
		}
	}
}

func getQualityFromHistory(q *[]qualityHistory, sample uint32) (uint32, error) {
	// simple case where quality does not change, so return the first value
	if len(*q) == 1 {
		// fmt.Println(sample, (*q)[0].value)
		return (*q)[0].value, nil
	}

	var startRange uint32 = 0
	var endRange uint32 = 0
	for i := range *q {
		if (*q)[i].samples == 0 {
			// if i > 0 {
			// 	fmt.Println("zero value", sample, (*q)[i].samples, (*q)[i].value, position)
			// }
			return (*q)[i].value, nil
		}
		startRange = endRange
		endRange += (*q)[i].samples
		if sample >= startRange && sample < endRange {
			// if i > 0 {
			// fmt.Println(sample, (*q)[i].samples, (*q)[i].value, position)
			// }
			return (*q)[i].value, nil
		}
	}

	// default quality value
	return (*q)[len(*q)-1].value, errors.New("Could not decode quality value")
}

// Encode encodes the next set of samples
func (s *Encoder) Encode(data *DatasetWithQuality) ([]byte, int, error) {
	if s.encodedSamples == 0 {
		s.len = 0
		s.len += copy(s.buf[s.len:], s.ID[:])

		// encode timestamp
		binary.BigEndian.PutUint64(s.buf[s.len:], data.T)
		s.len += 8

		// encode sampling rate
		s.len += binary.PutUvarint(s.buf[s.len:], uint64(s.samplingRate))

		// encode number of variables, so that wireshark, etc, can interpret
		s.len += binary.PutUvarint(s.buf[s.len:], uint64(s.Int32Count))

		// encode number of data samples
		s.len += binary.PutUvarint(s.buf[s.len:], uint64(s.samplesPerPacket))

		// reserve space for the quality section offset
		// need to encode offset to quality section to decode synchronously with data
		// need to reserve 4 bytes for full flexibility
		s.qualityOffsetBytes = s.len
		s.len += 4

		s.HeaderBytes = s.len

		// encode first set of values
		for i := range data.Int32s {
			// binary.BigEndian.PutUint32(s.buf[s.len:], uint32(data.Int32s[i]))
			s.len += binary.PutVarint(s.buf[s.len:], int64(data.Int32s[i]))
			// save previous value
			s.prevSamples.Int32s[i] = data.Int32s[i]
			// fmt.Println("  encode first set of values:", s.len)
		}

		// record first set of quality
		for i := range data.Q {
			s.qualityHistory[i][0].value = data.Q[i]
			s.qualityHistory[i][0].samples = 1
		}
	} else {
		for i := range data.Int32s {
			var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
			// fmt.Println("values:", s.encodedSamples, data.Int32s[i], s.prevSamples.Int32s[i], diff)
			lenB := binary.PutVarint(s.buf[s.len:], int64(diff))
			s.len += lenB

			// save previous value
			s.prevSamples.Int32s[i] = data.Int32s[i]
		}
		for i := range data.Q {
			if s.qualityHistory[i][len(s.qualityHistory[i])-1].value == data.Q[i] {
				s.qualityHistory[i][len(s.qualityHistory[i])-1].samples++
			} else {
				s.qualityHistory[i] = append(s.qualityHistory[i], qualityHistory{value: data.Q[i], samples: 1})
			}
		}
	}

	s.encodedSamples++
	if s.encodedSamples >= s.samplesPerPacket {
		// encode the start offset of the quality section in the header
		binary.BigEndian.PutUint32(s.buf[s.qualityOffsetBytes:], uint32(s.len))

		// encode final quality values using RLE
		for i := range s.qualityHistory {
			// override final number of samples to zero
			s.qualityHistory[i][len(s.qualityHistory[i])-1].samples = 0
			// fmt.Println("override:", len(s.qualityHistory[i]), s.qualityHistory[i], s.qualityHistory[i][len(s.qualityHistory[i])-1].samples, "value:", s.qualityHistory[i][len(s.qualityHistory[i])-1].value)

			// TODO can refactor if doing override above
			if len(s.qualityHistory[i]) == 1 {
				// special case, no change in quality value (encode samples as 0)
				lenB := binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][0].value))
				s.len += lenB
				lenB = binary.PutUvarint(s.buf[s.len:], 0)
				s.len += lenB
			} else {
				// otherwise, encode each value
				// fmt.Println("encode multiple Q values", len(s.qualityHistory[i]))
				for j := range s.qualityHistory[i] {
					lenB := binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][j].value))
					s.len += lenB
					lenB = binary.PutUvarint(s.buf[s.len:], uint64(s.qualityHistory[i][j].samples))
					s.len += lenB
				}
			}
		}

		// reset quality history
		for i := range s.qualityHistory {
			s.qualityHistory[i][0].value = 0
			s.qualityHistory[i][0].samples = 0
		}

		// reset previous values
		finalLen := s.len
		s.qualityOffsetBytes = 0
		s.encodedSamples = 0
		s.len = 0

		// send data
		return s.buf[0:finalLen], finalLen, nil
	}

	return nil, 0, nil
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
