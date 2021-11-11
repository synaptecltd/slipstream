package slipstream

import (
	"errors"
	gzip "github.com/klauspost/compress/gzip"
	"github.com/synaptecltd/encoding/bitops"

	"bytes"
	"encoding/binary"
	"github.com/google/uuid"
	"io"
	"sync"

	"github.com/synaptecltd/encoding/simple8b"
)

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
func (s *Decoder) SetXOR(xor bool) {
	s.useXOR = xor
}

// SetSpatialRefs automatically maps adjacent sets of three-phase currents for spatial compression
func (s *Decoder) SetSpatialRefs(count int, countV int, countI int, includeNeutral bool) {
	s.spatialRef = createSpatialRefs(count, countV, countI, includeNeutral)
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
