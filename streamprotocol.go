package streamprotocol

import (
	"encoding/binary"
	"time"

	"github.com/google/uuid"
)

// Encoder defines lists of variables to be encoded
type Encoder struct {
	Int32s []int32
	// float32s []float32
}

// StreamProtocol defines a stream protocol instance
type StreamProtocol struct {
	ID               uuid.UUID
	samplingRate     int
	samplesPerPacket int
	encodedSamples   int
	buf              []byte
	len              int
	dataLen          int
	totalTime        time.Duration
	// prevSample       sampleReactionRaw
	// prevDiff         sampleReactionRaw

	int32Count  int
	prevSamples Encoder
	prevDiffs   Encoder
	// buf              *bytes.Buffer
}

// NewStreamProtocol creates a stream protocol instance
func NewStreamProtocol(ID uuid.UUID, int32Count int, samplingRate int, samplesPerPacket int) *StreamProtocol {
	// estimate buffer space required
	bufSize := samplesPerPacket * int32Count * 8

	s := &StreamProtocol{
		ID:               ID,
		samplingRate:     samplingRate,
		samplesPerPacket: samplesPerPacket,
		// buf:              new(bytes.Buffer),
		buf: make([]byte, bufSize),
	}

	s.setStreamDataset(int32Count)

	return s
}

func (s *StreamProtocol) setStreamDataset(int32Count int) {
	s.int32Count = int32Count
	s.prevSamples.Int32s = make([]int32, int32Count)
	s.prevDiffs.Int32s = make([]int32, int32Count)
}

// EncodeStreamProtocol encodes the next set of samples
func (s *StreamProtocol) EncodeStreamProtocol(data Encoder, q []uint32, smpCnt uint64) {
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
			binary.BigEndian.PutUint32(s.buf[s.len:], q[i])
			s.len += 4
		}
	} else if s.encodedSamples == 1 {
		for i := range data.Int32s {
			var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i]
			s.prevDiffs.Int32s[i] = diff
			len := binary.PutVarint(s.buf[s.len:], int64(diff))
			s.dataLen += len
			s.len += len
			// fmt.Println(data.Ia1, s.prevSample.Ia1, diff, len)
		}
	} else {
		for i := range data.Int32s {
			var diff int32 = data.Int32s[i] - s.prevSamples.Int32s[i] - s.prevDiffs.Int32s[i]
			s.prevDiffs.Int32s[i] = diff
			len := binary.PutVarint(s.buf[s.len:], int64(diff))
			s.dataLen += len
			s.len += len
			// fmt.Println(data.Ia1, s.prevSample.Ia1, s.prevDiff.Ia1, diff, len)
			// s.prevDiff.Ia1 = int32(diff)
		}
	}

	// TODO add quality

	elapsed := time.Since(start)
	s.totalTime += elapsed

	s.encodedSamples++
	if s.encodedSamples >= s.samplesPerPacket {
		// fmt.Println(s.encodedSamples, "samples,", s.len, s.dataLen, float64(s.dataLen)/float64(s.samplesPerPacket), s.totalTime.Microseconds(), "µs")

		// send data

		s.encodedSamples = 0
		s.len = 0
		s.dataLen = 0
		s.totalTime = 0
	}
}
