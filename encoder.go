package slipstream

import (
	"bytes"
	"encoding/binary"
	"sync"

	"github.com/google/uuid"
	gzip "github.com/klauspost/compress/gzip"
	"github.com/rs/zerolog/log"
	"github.com/synaptecltd/encoding/bitops"
	"github.com/synaptecltd/encoding/simple8b"
)

// Encoder defines a stream protocol instance
type Encoder struct {
	ID                  uuid.UUID
	SamplingRate        int
	SamplesPerMessage   int
	Int32Count          int
	buf                 []byte
	bufA                []byte
	bufB                []byte
	outBufA             *bytes.Buffer
	outBufB             *bytes.Buffer
	useBufA             bool
	len                 int
	encodedSamples      int
	usingSimple8b       bool
	deltaEncodingLayers int
	simple8bValues      []uint64
	prevData            []Dataset
	deltaN              []int32

	qualityHistory [][]qualityHistory
	diffs          [][]uint64
	values         [][]int32
	mutex          sync.Mutex

	useXOR     bool
	spatialRef []int
}

// NewEncoder creates a stream protocol encoder instance
func NewEncoder(ID uuid.UUID, int32Count int, samplingRate int, samplesPerMessage int) *Encoder {
	// estimate maximum buffer space required
	bufSize := MaxHeaderSize + samplesPerMessage*int32Count*8 + int32Count*4

	s := &Encoder{
		ID:                ID,
		SamplingRate:      samplingRate,
		SamplesPerMessage: samplesPerMessage,
		bufA:              make([]byte, bufSize),
		bufB:              make([]byte, bufSize),
		Int32Count:        int32Count,
		simple8bValues:    make([]uint64, samplesPerMessage),
	}

	// s.useXOR = true

	// initialise ping-pong buffer
	s.useBufA = true
	s.buf = s.bufA

	// TODO make this conditional on message size to reduce memory use
	s.outBufA = bytes.NewBuffer(make([]byte, 0, bufSize))
	s.outBufB = bytes.NewBuffer(make([]byte, 0, bufSize))

	s.deltaEncodingLayers = getDeltaEncoding(samplingRate)

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
	s.prevData = make([]Dataset, s.deltaEncodingLayers)
	for i := range s.prevData {
		s.prevData[i].Int32s = make([]int32, int32Count)
	}
	s.deltaN = make([]int32, s.deltaEncodingLayers)

	s.qualityHistory = make([][]qualityHistory, int32Count)
	for i := range s.qualityHistory {
		// set capacity to avoid some possible allocations during encoding
		s.qualityHistory[i] = make([]qualityHistory, 1, 16)
		s.qualityHistory[i][0].value = 0
		s.qualityHistory[i][0].samples = 0
	}

	s.spatialRef = make([]int, int32Count)
	for i := range s.spatialRef {
		s.spatialRef[i] = -1
	}

	return s
}

// SetXOR uses XOR delta instead of arithmetic delta
func (s *Encoder) SetXOR(xor bool) {
	s.useXOR = xor
}

// SetSpatialRefs automatically maps adjacent sets of three-phase currents for spatial compression
func (s *Encoder) SetSpatialRefs(count int, countV int, countI int, includeNeutral bool) {
	s.spatialRef = createSpatialRefs(count, countV, countI, includeNeutral)
}

func (s *Encoder) encodeSingleSample(index int, value int32) {
	if s.usingSimple8b {
		s.diffs[index][s.encodedSamples] = bitops.ZigZagEncode64(int64(value))
	} else {
		s.values[s.encodedSamples][index] = value
	}
}

// Encode encodes the next set of samples. It is called iteratively until the pre-defined number of samples are provided.
func (s *Encoder) Encode(data *DatasetWithQuality) ([]byte, int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// encode header and prepare quality values
	if s.encodedSamples == 0 {
		s.len = 0
		s.len += copy(s.buf[s.len:], s.ID[:])

		// encode timestamp
		binary.BigEndian.PutUint64(s.buf[s.len:], data.T)
		s.len += 8

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
		j := s.encodedSamples // copy for conciseness
		val := data.Int32s[i]

		// check if another data stream is to be used the spatial reference
		if s.spatialRef[i] >= 0 {
			val -= data.Int32s[s.spatialRef[i]]
		}

		// prepare data for delta encoding
		if j > 0 {
			if s.useXOR {
				s.deltaN[0] = val ^ s.prevData[0].Int32s[i]
			} else {
				s.deltaN[0] = val - s.prevData[0].Int32s[i]
			}
		}
		for k := 1; k < min(j, s.deltaEncodingLayers); k++ {
			if s.useXOR {
				s.deltaN[k] = s.deltaN[k-1] ^ s.prevData[k].Int32s[i]
			} else {
				s.deltaN[k] = s.deltaN[k-1] - s.prevData[k].Int32s[i]
			}
		}

		// encode the value
		if j == 0 {
			s.encodeSingleSample(i, val)
		} else {
			s.encodeSingleSample(i, s.deltaN[min(j-1, s.deltaEncodingLayers-1)])
		}

		// save samples and deltas for next iteration
		s.prevData[0].Int32s[i] = val
		for k := 1; k <= min(j, s.deltaEncodingLayers-1); k++ {
			s.prevData[k].Int32s[i] = s.deltaN[k-1]
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

// CancelEncode ends the encoding early, but does not write to the file
func (s *Encoder) CancelEncode() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// reset quality history
	for i := range s.qualityHistory {
		s.qualityHistory[i] = s.qualityHistory[i][:1]
		s.qualityHistory[i][0].value = 0
		s.qualityHistory[i][0].samples = 0
	}

	// reset previous values
	s.encodedSamples = 0
	s.len = 0

	// send data and swap ping-pong buffer
	if s.useBufA {
		s.useBufA = false
		s.buf = s.bufB
	}
}

// internal version does not need the mutex
func (s *Encoder) endEncode() ([]byte, int, error) {
	// write encoded samples
	s.len += putVarint32(s.buf[s.len:], int32(s.encodedSamples))
	actualHeaderLen := s.len

	if s.usingSimple8b {
		for i := range s.diffs {
			// ensure slice only contains up to s.encodedSamples
			actualSamples := min(s.encodedSamples, s.SamplesPerMessage)

			numberOfSimple8b, _ := simple8b.EncodeAllRef(&s.simple8bValues, s.diffs[i][:actualSamples])

			// calculate efficiency of simple8b
			// multiply number of simple8b units by 2 because input is 32-bit, output is 64-bit
			// simple8bRatio := float64(2*numberOfSimple8b) / float64(actualSamples)
			// fmt.Println("simple8b efficiency:", simple8bRatio)

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

	// experiment with Huffman coding
	// var enc huff0.Scratch
	// comp, _, err := huff0.Compress4X(s.buf[0:s.len], &enc)
	// if err == huff0.ErrIncompressible || err == huff0.ErrUseRLE || err == huff0.ErrTooBig {
	// 	log.Error().Err(err).Msg("huff0 error")
	// }
	// log.Debug().Int("huff0 len", len(comp)).Int("original len", s.len).Msg("huff0 output")

	// experiment with gzip
	// TODO determine if bufA/bufB can be replaced with this internal double buffering
	activeOutBuf := s.outBufA
	if !s.useBufA {
		activeOutBuf = s.outBufB
	}

	// TODO inspect performance here
	activeOutBuf.Reset()
	if s.encodedSamples > UseGzipThresholdSamples {
		// do not compress header
		activeOutBuf.Write(s.buf[0:actualHeaderLen])

		gz, _ := gzip.NewWriterLevel(activeOutBuf, gzip.BestCompression) // can test entropy coding by using gzip.HuffmanOnly
		if _, err := gz.Write(s.buf[actualHeaderLen:s.len]); err != nil {
			log.Error().Err(err).Msg("could not write gz")
		}
		if err := gz.Close(); err != nil {
			log.Error().Err(err).Msg("could not close gz")
		}

		// ensure that gzip size is never greater that input for all input sizes
		if activeOutBuf.Len() > s.len && s.encodedSamples == s.SamplesPerMessage {
			log.Error().Int("gz", activeOutBuf.Len()).Int("original", s.len).Int("SamplesPerMessage", s.SamplesPerMessage).Msg("gzip encoding length greater")
		}
	} else {
		activeOutBuf.Write(s.buf[0:s.len])
	}

	// reset previous values
	// finalLen := s.len
	s.encodedSamples = 0
	s.len = 0

	// send data and swap ping-pong buffer
	if s.useBufA {
		s.useBufA = false
		s.buf = s.bufB
		return activeOutBuf.Bytes(), activeOutBuf.Len(), nil
		// return s.bufA[0:finalLen], finalLen, nil
	}

	s.useBufA = true
	s.buf = s.bufA
	return activeOutBuf.Bytes(), activeOutBuf.Len(), nil
	// return s.bufB[0:finalLen], finalLen, nil
}
