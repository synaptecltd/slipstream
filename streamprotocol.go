package streamprotocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/google/uuid"
)

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
	dataLen            int
	totalTime          time.Duration
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

// Decode decodes a stream protocol message
func (s *Decoder) Decode(buf []byte, totalLength int) {
	length := 16
	var lenQuality uint32 = 0
	var quality [][]qualityHistory = make([][]qualityHistory, s.Int32Count)
	for i := range quality {
		quality[i] = []qualityHistory{{value: 0, samples: 0}}
	}

	// check ID
	res := bytes.Compare(buf[:length], s.ID[:])
	if res != 0 {
		fmt.Println("ID no match", buf[:length], s.ID[:])
		return
	}

	// decode timestamp
	startTime := binary.BigEndian.Uint64(buf[length:])
	length += 8

	// decode sampling rate
	// samplingRate := binary.BigEndian.Uint32(buf[length:])	// TODO remove from header?
	length += 4

	// decode number of data samples
	samplesPerPacket := binary.BigEndian.Uint32(buf[length:])
	length += 4

	// decode quality offset
	qualityOffset := binary.BigEndian.Uint32(buf[length:])
	length += 4

	// initialise output structure
	var data DatasetWithQuality = DatasetWithQuality{
		T:      startTime,
		Int32s: make([]int32, s.Int32Count),
		Q:      make([]uint32, s.Int32Count),
	}

	// get first samples
	for i := 0; i < s.Int32Count; i++ {
		data.Int32s[i] = int32(binary.BigEndian.Uint32(buf[length:]))
		length += 4
	}

	// populate quality structure
	lenQuality = qualityOffset
	for i := 0; i < s.Int32Count; i++ {
		quality[i][0].value = binary.BigEndian.Uint32(buf[lenQuality:])
		lenQuality += 4
		quality[i][0].samples = binary.BigEndian.Uint32(buf[lenQuality:])
		lenQuality += 4
		if quality[i][0].samples != 0 {
			// decode each quality change and store in structure
			totalSamples := quality[i][0].samples
			for j := 1; ; j++ {
				// create and populate new array item
				quality[i] = append(quality[i], qualityHistory{value: 0, samples: 0})
				// fmt.Println(i, j, len(quality[i]), "totalSamples", totalSamples, "length:", lenQuality, totalLength)
				quality[i][j].value = binary.BigEndian.Uint32(buf[lenQuality:])
				lenQuality += 4
				quality[i][j].samples = binary.BigEndian.Uint32(buf[lenQuality:])
				lenQuality += 4

				totalSamples += quality[i][j].samples
				if totalSamples >= samplesPerPacket {
					break
				}
			}
		}
	}

	// extract first quality value and send first set of samples
	for i := 0; i < s.Int32Count; i++ {
		data.Q[i] = quality[i][0].value
	}
	s.Ch <- data

	// loop through remaining samples
	var totalSamples uint32 = 1
	var prevData DatasetWithQuality = DatasetWithQuality{}
	for {

		// TODO calculate T from samplingRate and totalSamples
		data.T = startTime + uint64(totalSamples)
		if data.T >= uint64(s.samplingRate) {
			data.T = data.T - uint64(s.samplingRate)
		}
		for i := 0; i < s.Int32Count; i++ {
			diff, bytes := binary.Varint(buf[length:])
			length += bytes
			data.Int32s[i] = data.Int32s[i] + int32(diff)
			data.Q[i] = getQualityFromHistory(&quality[i], totalSamples)
		}

		s.Ch <- data

		prevData.T = data.T
		copy(prevData.Int32s, data.Int32s)
		copy(prevData.Q, data.Q)

		totalSamples++
		if totalSamples >= samplesPerPacket {
			break
		}
	}
}

func getQualityFromHistory(q *[]qualityHistory, sample uint32) uint32 {
	// TODO
	return 0
}

// Encode encodes the next set of samples
func (s *Encoder) Encode(data Dataset, q []uint32, t uint64) ([]byte, int) {
	// TODO refactor to use DatasetWithQuality
	start := time.Now()

	if s.encodedSamples == 0 {
		s.len = 0
		s.len += copy(s.buf[s.len:], s.ID[:])

		// encode timestamp
		binary.BigEndian.PutUint64(s.buf[s.len:], t)
		s.len += 8

		// encode sampling rate
		binary.BigEndian.PutUint32(s.buf[s.len:], uint32(s.samplingRate))
		s.len += 4

		// encode number of data samples
		binary.BigEndian.PutUint32(s.buf[s.len:], uint32(s.samplesPerPacket))
		s.len += 4

		// reserve space for the quality section offset
		// need to encode offset to quality to decode synchronously with data
		s.qualityOffsetBytes = s.len
		s.len += 4

		// encode first set of values
		for i := range data.Int32s {
			binary.BigEndian.PutUint32(s.buf[s.len:], uint32(data.Int32s[i]))
			s.len += 4
		}

		// record first set of quality
		for i := range q {
			s.qualityHistory[i][0].value = q[i]
			s.qualityHistory[i][0].samples = 1
		}
	} else /*if s.encodedSamples == 1*/ {
		for i := range data.Int32s {
			var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
			lenB := binary.PutVarint(s.buf[s.len:], int64(diff))
			s.dataLen += lenB
			s.len += lenB
			// fmt.Println(data.Ia1, s.prevSample.Ia1, diff, lenB)
		}
		for i := range q {
			if s.qualityHistory[i][len(s.qualityHistory[i])-1].value == q[i] {
				s.qualityHistory[i][len(s.qualityHistory[i])-1].samples++
			} else {
				s.qualityHistory[i] = append(s.qualityHistory[i], qualityHistory{value: q[i], samples: 1})
			}
		}
	}

	elapsed := time.Since(start)
	s.totalTime += elapsed

	s.encodedSamples++
	if s.encodedSamples >= s.samplesPerPacket {
		// encode the start offset of the quality section in the header
		binary.BigEndian.PutUint32(s.buf[s.qualityOffsetBytes:], uint32(s.len))

		// encode final quality values using RLE
		for i := range q {
			if len(s.qualityHistory[i]) == 1 {
				// special case, no change in quality value (encode samples as 0)
				binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][0].value)
				s.len += 4
				binary.BigEndian.PutUint32(s.buf[s.len:], 0)
				s.len += 4
			} else {
				// otherwise, encode each value
				// fmt.Println("encode multiple Q values", len(s.qualityHistory[i]))
				for j := range s.qualityHistory[i] {
					// fmt.Println("  ", j, s.qualityHistory[i][j].samples, s.qualityHistory[i][j].value)
					binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][j].value)
					s.len += 4
					binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][j].samples)
					s.len += 4
				}
			}
		}

		// reset quality history
		for i := range s.qualityHistory {
			s.qualityHistory[i] = []qualityHistory{{value: 0, samples: 0}}
		}

		// inputData := float64(s.samplesPerPacket * s.Int32Count * 8)
		// efficiency := float64(s.dataLen) / inputData
		// fmt.Println(s.encodedSamples, "samples,", s.len, s.dataLen, efficiency, s.totalTime.Microseconds(), "µs")
		// fmt.Println(s.buf[0:s.len])

		finalLen := s.len

		s.qualityOffsetBytes = 0
		s.encodedSamples = 0
		s.len = 0
		s.dataLen = 0
		s.totalTime = 0

		// send data
		return s.buf[0:finalLen], finalLen
	}

	return nil, 0
}
