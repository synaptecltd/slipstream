package streamprotocol

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Dataset defines lists of variables to be encoded
type Dataset struct {
	Int32s []int32
	// float32s []float32
}

// DatasetWithQuality defines lists of decoded variables with quality
type DatasetWithQuality struct {
	Int32s []int32
	Q      []uint32
}

// Encoder defines lists of variables to be encoded
type qualityHistory struct {
	value   uint32
	samples uint32
}

// Encoder defines a stream protocol instance
type Encoder struct {
	ID               uuid.UUID
	samplingRate     int
	samplesPerPacket int
	encodedSamples   int
	buf              []byte
	len              int
	dataLen          int
	totalTime        time.Duration
	Int32Count       int
	prevSamples      Dataset
	prevDiffs        Dataset
	qualityHistory   [][]qualityHistory
}

// Decoder defines a stream protocol instance for decoding
type Decoder struct {
	ID               uuid.UUID
	samplingRate     int
	samplesPerPacket int
	Int32Count       int
	Ch               chan DatasetWithQuality
}

// NewEncoder creates a stream protocol encoder instance
func NewEncoder(ID uuid.UUID, int32Count int, samplingRate int, samplesPerPacket int) *Encoder {
	// estimate buffer space required
	bufSize := samplesPerPacket * int32Count * 8

	s := &Encoder{
		ID:               ID,
		samplingRate:     samplingRate,
		samplesPerPacket: samplesPerPacket,
		// buf:              new(bytes.Buffer),
		buf: make([]byte, bufSize),
	}

	s.setStreamDataset(int32Count)

	s.qualityHistory = make([][]qualityHistory, int32Count)
	for i := range s.qualityHistory {
		s.qualityHistory[i] = []qualityHistory{{value: 0, samples: 0}}
	}

	return s
}

func (s *Encoder) setStreamDataset(int32Count int) {
	s.Int32Count = int32Count
	s.prevSamples.Int32s = make([]int32, int32Count)
	s.prevDiffs.Int32s = make([]int32, int32Count)
}

// NewDecoder creates a stream protocol decoder instance
func NewDecoder(samplesPerPacket int) *Decoder {

	d := &Decoder{
		samplesPerPacket: samplesPerPacket,
		Int32Count:       0,
		Ch:               make(chan DatasetWithQuality, samplesPerPacket*2),
	}

	return d
}

// Decode decodes a stream protocol message
func (s *Encoder) Decode(buf []byte) {
}

// Encode encodes the next set of samples
func (s *Encoder) Encode(data Dataset, q []uint32, smpCnt uint64) ([]byte, int) {
	start := time.Now()

	if s.encodedSamples == 0 {
		s.len = 0
		// s.buf.WriteString(s.ID.String())
		s.len += copy(s.buf[s.len:], s.ID[:])
		// binary.PutVarint(s.buf, smpCnt)

		// encode timestamp
		binary.BigEndian.PutUint64(s.buf[s.len:], smpCnt)
		s.len += 8

		// encode sampling rate
		binary.BigEndian.PutUint32(s.buf[s.len:], uint32(s.samplingRate))
		s.len += 4

		// encode number of data samples
		binary.BigEndian.PutUint32(s.buf[s.len:], uint32(s.samplesPerPacket))
		s.len += 4

		// encode first set of values
		for i := range data.Int32s {
			binary.BigEndian.PutUint32(s.buf[s.len:], uint32(data.Int32s[i]))
			s.len += 4
		}

		// encode first set of quality
		for i := range q {
			// 	binary.BigEndian.PutUint32(s.buf[s.len:], q[i])
			// 	s.len += 4
			s.qualityHistory[i][0].value = q[i]
			s.qualityHistory[i][0].samples = 1
		}
	} else if s.encodedSamples == 1 {
		for i := range data.Int32s {
			var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
			s.prevDiffs.Int32s[i] = diff
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
	} else {
		for i := range data.Int32s {
			var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i] - s.prevDiffs.Int32s[i]
			s.prevDiffs.Int32s[i] = diff
			lenB := binary.PutVarint(s.buf[s.len:], int64(diff))
			s.dataLen += lenB
			s.len += lenB
			// fmt.Println(data.Ia1, s.prevSample.Ia1, s.prevDiff.Ia1, diff, lenB)
			// s.prevDiff.Ia1 = int32(diff)
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
		// encode final quality values using RLE
		for i := range q {
			if len(s.qualityHistory[i]) == 1 {
				// special case, no change in quality value
				binary.BigEndian.PutUint32(s.buf[s.len:], 0)
				s.len += 4
				binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][0].value)
				s.len += 4
			} else {
				// otherwise, encode each value
				fmt.Println("multiple Q values", len(s.qualityHistory[i]))
				for j := range s.qualityHistory[i] {
					fmt.Println("  ", j, s.qualityHistory[i][j].samples, s.qualityHistory[i][j].value)
					binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][j].samples)
					s.len += 4
					binary.BigEndian.PutUint32(s.buf[s.len:], s.qualityHistory[i][j].value)
					s.len += 4
				}
			}
		}

		// reset quality history
		for i := range s.qualityHistory {
			s.qualityHistory[i] = []qualityHistory{{value: 0, samples: 0}}
		}

		inputData := float64(s.samplesPerPacket * s.Int32Count * 8)
		efficiency := float64(s.dataLen) / inputData
		fmt.Println(s.encodedSamples, "samples,", s.len, s.dataLen, efficiency, s.totalTime.Microseconds(), "µs")

		finalLen := s.len

		s.encodedSamples = 0
		s.len = 0
		s.dataLen = 0
		s.totalTime = 0

		// send data
		return s.buf[0:finalLen], finalLen
	}

	return nil, 0
}
