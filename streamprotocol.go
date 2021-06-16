package streamprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"

	gzip "github.com/klauspost/compress/gzip"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/stevenblair/encoding/bitops"
	"github.com/stevenblair/encoding/simple8b"
)

// Simple8bThresholdSamples defines the number of samples per message required before using simple-8b encoding
const Simple8bThresholdSamples = 16

// TODO value of 5 is good in streamprotocol_test.go, but slightly worse for microcontroller data (0.36 Mbps vs 0.3 Mbps)
// DefaultDeltaEncodingLayers defines the default number of layers of delta encoding. 0 is no delta encoding (just use varint), 1 is delta encoding, etc.
const DefaultDeltaEncodingLayers = 3

// HighDeltaEncodingLayers defines the number of layers of delta encoding for high sampling rate scenarios.
const HighDeltaEncodingLayers = 3

// MaxHeaderSize is the size of the message header in bytes
const MaxHeaderSize = 36

// UseGzipThresholdSamples is the minimum number of samples per message to use gzip on the payload
const UseGzipThresholdSamples = 250

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

// Decoder defines a stream protocol instance for decoding
type Decoder struct {
	ID                  uuid.UUID
	samplingRate        int
	samplesPerMessage   int
	encodedSamples      int
	Int32Count          int
	gzBuf               *bytes.Buffer
	Out                 []DatasetWithQuality
	startTimestamp      uint64
	usingSimple8b       bool
	deltaEncodingLayers int
	deltaSum            [][]int32
	mutex               sync.Mutex

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

// NewDecoder creates a stream protocol decoder instance for pre-allocated output
func NewDecoder(ID uuid.UUID, int32Count int, samplingRate int, samplesPerMessage int) *Decoder {
	d := &Decoder{
		ID:                ID,
		Int32Count:        int32Count,
		samplingRate:      samplingRate,
		samplesPerMessage: samplesPerMessage,
		Out:               make([]DatasetWithQuality, samplesPerMessage),
	}

	// d.useXOR = true

	if samplesPerMessage > Simple8bThresholdSamples {
		d.usingSimple8b = true
	}

	// TODO make this conditional on message size to reduce memory use
	bufSize := samplesPerMessage*int32Count*8 + int32Count*4
	d.gzBuf = bytes.NewBuffer(make([]byte, 0, bufSize))

	d.deltaEncodingLayers = getDeltaEncoding(samplingRate)

	// storage for delta-delta decoding
	d.deltaSum = make([][]int32, d.deltaEncodingLayers-1)
	for i := range d.deltaSum {
		d.deltaSum[i] = make([]int32, int32Count)
	}

	// initialise each set of outputs in data stucture
	for i := range d.Out {
		d.Out[i].Int32s = make([]int32, int32Count)
		d.Out[i].Q = make([]uint32, int32Count)
	}

	d.spatialRef = make([]int, int32Count)
	for i := range d.spatialRef {
		d.spatialRef[i] = -1
	}

	return d
}

// SetXOR uses XOR delta instead of arithmetic delta
func (s *Encoder) SetXOR(xor bool) {
	s.useXOR = xor
}

// SetXOR uses XOR delta instead of arithmetic delta
func (s *Decoder) SetXOR(xor bool) {
	s.useXOR = xor
}

// SetSpatialRefs automatically maps adjacent sets of three-phase currents for spatial compression
func (s *Encoder) SetSpatialRefs(count int, countV int, countI int, includeNeutral bool) {
	s.spatialRef = createSpatialRefs(count, countV, countI, includeNeutral)
}

// SetSpatialRefs automatically maps adjacent sets of three-phase currents for spatial compression
func (s *Decoder) SetSpatialRefs(count int, countV int, countI int, includeNeutral bool) {
	s.spatialRef = createSpatialRefs(count, countV, countI, includeNeutral)
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

// DecodeToBuffer decodes to a pre-allocated buffer
func (s *Decoder) DecodeToBuffer(buf []byte, totalLength int) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

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

	actualSamples := min(s.encodedSamples, s.samplesPerMessage)

	// TODO inspect performance here
	s.gzBuf.Reset()
	if s.samplesPerMessage > UseGzipThresholdSamples {
		gr, err := gzip.NewReader(bytes.NewBuffer(buf[length:]))
		if err != nil {
			return err
		}

		_, errRead := io.Copy(s.gzBuf, gr)
		// origLen, errRead := gr.Read((buf[length:]))
		if errRead != nil {
			return errRead
		}
		gr.Close()
	} else {
		s.gzBuf = bytes.NewBuffer(buf[length:])
	}
	// log.Debug().Int("gz len", totalLength).Int64("original len", origLen).Msg("decoding")
	outBytes := s.gzBuf.Bytes()
	length = 0

	if s.usingSimple8b {
		// for simple-8b encoding, iterate through every value
		decodeCounter := 0
		indexTs := 0
		i := 0

		decodedUnit64s, _ := simple8b.ForEach( /*buf[length:]*/ outBytes[length:], func(v uint64) bool {
			// manage 2D slice indices
			indexTs = decodeCounter % actualSamples
			if decodeCounter > 0 && indexTs == 0 {
				i++
			}

			// get signed value back with zig-zag decoding
			decodedValue := int32(bitops.ZigZagDecode64(v))

			if indexTs == 0 {
				s.Out[indexTs].Int32s[i] = decodedValue
			} else {
				s.Out[indexTs].T = uint64(indexTs)

				// delta decoding
				maxIndex := min(indexTs, s.deltaEncodingLayers-1) - 1
				if s.useXOR {
					s.deltaSum[maxIndex][i] ^= decodedValue
				} else {
					s.deltaSum[maxIndex][i] += decodedValue
				}

				for k := maxIndex; k >= 1; k-- {
					if s.useXOR {
						s.deltaSum[k-1][i] ^= s.deltaSum[k][i]
					} else {
						s.deltaSum[k-1][i] += s.deltaSum[k][i]
					}
				}

				if s.useXOR {
					s.Out[indexTs].Int32s[i] = s.Out[indexTs-1].Int32s[i] ^ s.deltaSum[0][i]
				} else {
					s.Out[indexTs].Int32s[i] = s.Out[indexTs-1].Int32s[i] + s.deltaSum[0][i]
				}
			}

			decodeCounter++

			// all variables and timesteps have been decoded
			if decodeCounter == actualSamples*s.Int32Count {
				// take care of spatial references (cannot do this piecemeal above because it disrupts the previous value history)
				for indexTs := range s.Out {
					for i := range s.Out[indexTs].Int32s {
						if s.spatialRef[i] >= 0 {
							s.Out[indexTs].Int32s[i] += s.Out[indexTs].Int32s[s.spatialRef[i]]
						}
					}
				}

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
			valSigned, lenB = varint32( /*buf[length:]*/ outBytes[length:])
			s.Out[0].Int32s[i] = int32(valSigned)
			length += lenB
		}

		// decode remaining delta-delta encoded values
		if actualSamples > 1 {
			var totalSamples int = 1
			for {
				// encode the sample number relative to the starting timestamp
				s.Out[totalSamples].T = uint64(totalSamples)

				// delta decoding
				for i := 0; i < s.Int32Count; i++ {
					decodedValue, lenB := varint32( /*buf[length:]*/ outBytes[length:])
					length += lenB

					maxIndex := min(totalSamples, s.deltaEncodingLayers-1) - 1
					if s.useXOR {
						s.deltaSum[maxIndex][i] ^= decodedValue
					} else {
						s.deltaSum[maxIndex][i] += decodedValue
					}

					for k := maxIndex; k >= 1; k-- {
						if s.useXOR {
							s.deltaSum[k-1][i] ^= s.deltaSum[k][i]
						} else {
							s.deltaSum[k-1][i] += s.deltaSum[k][i]
						}
					}

					if s.useXOR {
						s.Out[totalSamples].Int32s[i] = s.Out[totalSamples-1].Int32s[i] ^ s.deltaSum[0][i]
					} else {
						s.Out[totalSamples].Int32s[i] = s.Out[totalSamples-1].Int32s[i] + s.deltaSum[0][i]
					}
				}
				totalSamples++

				if totalSamples >= actualSamples {
					// take care of spatial references (cannot do this piecemeal above because it disrupts the previous value history)
					for indexTs := range s.Out {
						for i := range s.Out[indexTs].Int32s {
							// skip the first time index
							if s.spatialRef[i] >= 0 {
								s.Out[indexTs].Int32s[i] += s.Out[indexTs].Int32s[s.spatialRef[i]]
							}
						}
					}

					// end decoding
					break
				}
			}
		}
	}

	// populate quality structure
	for i := 0; i < s.Int32Count; i++ {
		sampleNumber := 0
		for sampleNumber < actualSamples {
			valUnsigned, lenB = uvarint32( /*buf[length:]*/ outBytes[length:])
			length += lenB
			s.Out[sampleNumber].Q[i] = uint32(valUnsigned)

			valUnsigned, lenB = uvarint32( /*buf[length:]*/ outBytes[length:])
			length += lenB

			if valUnsigned == 0 {
				// write all remaining Q values for this variable
				for j := sampleNumber + 1; j < len(s.Out); j++ {
					s.Out[j].Q[i] = s.Out[sampleNumber].Q[i]
				}
				sampleNumber = actualSamples
			} else {
				// write up to valUnsigned remaining Q values for this variable
				for j := sampleNumber + 1; j < int(valUnsigned); j++ {
					s.Out[j].Q[i] = s.Out[sampleNumber].Q[i]
				}
				sampleNumber += int(valUnsigned)
			}
		}
	}

	for j := range s.deltaSum {
		for i := 0; i < s.Int32Count; i++ {
			s.deltaSum[j][i] = 0
		}
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

// func abs(a int32) int32 {
// 	if a < 0 {
// 		return -a
// 	}
// 	return a
// }

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

// StopEncode ends the encoding early, but does not write to the file
func (s *Encoder) StopEncode() {
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
	if s.SamplesPerMessage > UseGzipThresholdSamples {
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
