package streamprotocol_test

import (
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/stretchr/testify/assert"
	"github.com/synaptec/synthesis/iedemulator"
	"github.com/synaptec/synthesis/streamprotocol"
)

var ID = uuid.New()
var samplingRate = 4000

var tests = map[string]struct {
	samplingRate      int
	countOfVariables  int
	samples           int
	samplesPerMessage int
	qualityChange     bool
	earlyEncodingStop bool
	useSpatialRefs    bool
	includeNeutral    bool
}{
	"a10-1":          {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 1},
	"a10-2":          {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 2},
	"a10-2q":         {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 2, qualityChange: true},
	"a10-10":         {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 10},
	"a4-2q":          {samplingRate: 4000, countOfVariables: 8, samples: 4, samplesPerMessage: 2, qualityChange: true},
	"a8-8q":          {samplingRate: 4000, countOfVariables: 8, samples: 8, samplesPerMessage: 8, qualityChange: true},
	"b4000-2":        {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 2},
	"b4000-80":       {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 80},
	"b4000-60":       {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 60},
	"b4000-800":      {samplingRate: 4000, countOfVariables: 8, samples: 800, samplesPerMessage: 800},
	"b4000-4000":     {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 4000},
	"b4000-4000s1":   {samplingRate: 4000, countOfVariables: 16, samples: 4000, samplesPerMessage: 4000, useSpatialRefs: false},
	"b4000-4000s2":   {samplingRate: 4000, countOfVariables: 16, samples: 4000, samplesPerMessage: 4000, useSpatialRefs: true},
	"c4800-2":        {samplingRate: 4800, countOfVariables: 8, samples: 4800, samplesPerMessage: 2},
	"c4800-20":       {samplingRate: 4800, countOfVariables: 8, samples: 4800, samplesPerMessage: 20},
	"d14400-6":       {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 6},
	"d4000-4000q":    {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 4000, qualityChange: true},
	"e14400-14400":   {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 14400},
	"e14400-14400s":  {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 14400, earlyEncodingStop: true},
	"e14400-14400q":  {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 14400, qualityChange: true},
	"f40000-40000":   {samplingRate: 4000, countOfVariables: 8, samples: 40000, samplesPerMessage: 40000},
	"g150000-150000": {samplingRate: 150000, countOfVariables: 8, samples: 150000, samplesPerMessage: 150000},
}

func createIEDEmulator(samplingRate int, phaseOffsetDeg float64) *iedemulator.IEDEmulator {
	return &iedemulator.IEDEmulator{
		SamplingRate: samplingRate,
		Fnom:         50.0,
		Fdeviation:   0.0,
		Ts:           1 / float64(samplingRate),
		V: iedemulator.ThreePhaseEmulation{
			PosSeqMag: 275000.0 / math.Sqrt(3) * math.Sqrt(2),
			// NoiseMax:  0.00001,
			PhaseOffset: phaseOffsetDeg * math.Pi / 180.0,
		},
		I: iedemulator.ThreePhaseEmulation{
			PosSeqMag:       500.0,
			PhaseOffset:     phaseOffsetDeg * math.Pi / 180.0,
			HarmonicNumbers: []float64{5, 7, 11, 13, 17, 19, 23, 25},
			HarmonicMags:    []float64{0.2164, 0.1242, 0.0892, 0.0693, 0.0541, 0.0458, 0.0370, 0.0332},
			HarmonicAngs:    []float64{171.5, 100.4, -52.4, 128.3, 80.0, 2.9, -146.8, 133.9},
			// NoiseMax:        0.00001,
		},
	}
}

func BenchmarkEncodeDecode(b1 *testing.B) {
	keys := make([]string, 0, len(tests))
	for k := range tests {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		b1.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()

				test := tests[name]

				// settings for IED emulator
				var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate, 0)

				// initialise data structure for input data
				var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

				// create encoder and decoder
				stream := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
				streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

				b.StartTimer()

				// encode the data
				// when each message is complete, decode
				encodeAndDecode(nil, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage, test.earlyEncodingStop)
			}
		})
	}
}

func BenchmarkEncode(b1 *testing.B) {
	keys := make([]string, 0, len(tests))
	for k := range tests {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		b1.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()

				test := tests[name]

				// settings for IED emulator
				var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate, 0)

				// initialise data structure for input data
				var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

				// create encoder and decoder
				enc := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
				dec := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

				// calling b.StartTimer() often slows things down
				b.StartTimer()
				for d := range data {
					buf, len, _ := enc.Encode(&data[d])

					if len > 0 {
						b.StopTimer()
						dec.DecodeToBuffer(buf, len)
						b.StartTimer()
					}
				}
			}
		})
	}
}

func BenchmarkDecode(b1 *testing.B) {
	keys := make([]string, 0, len(tests))
	for k := range tests {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		b1.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()

				test := tests[name]

				// settings for IED emulator
				var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate, 0)

				// initialise data structure for input data
				var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

				// create encoder and decoder
				enc := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
				dec := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

				for d := range data {
					buf, len, _ := enc.Encode(&data[d])

					if len > 0 {
						b.StartTimer()
						dec.DecodeToBuffer(buf, len)
						b.StopTimer()
					}
				}
			}
		})
	}
}

func createInputData(ied *iedemulator.IEDEmulator, samples int, countOfVariables int, qualityChange bool) []streamprotocol.DatasetWithQuality {
	var data []streamprotocol.DatasetWithQuality = make([]streamprotocol.DatasetWithQuality, samples)
	for i := range data {
		data[i].Int32s = make([]int32, countOfVariables)
		data[i].Q = make([]uint32, countOfVariables)
	}

	// generate data using IED emulator
	// the timestamp is a simple integer counter, starting from 0
	for i := range data {
		// compute emulated waveform data
		ied.Step()

		// calculate timestamp
		data[i].T = uint64(i)

		// set waveform data
		data[i].Int32s[0] = int32(ied.I.A * 1000.0)
		data[i].Int32s[1] = int32(ied.I.B * 1000.0)
		data[i].Int32s[2] = int32(ied.I.C * 1000.0)
		data[i].Int32s[3] = int32((ied.I.A + ied.I.B + ied.I.C) * 1000.0)
		data[i].Int32s[4] = int32(ied.V.A * 100.0)
		data[i].Int32s[5] = int32(ied.V.B * 100.0)
		data[i].Int32s[6] = int32(ied.V.C * 100.0)
		data[i].Int32s[7] = int32((ied.V.A + ied.V.B + ied.V.C) * 100.0)

		// set quality data
		data[i].Q[0] = 0
		data[i].Q[1] = 0
		data[i].Q[2] = 0
		data[i].Q[3] = 0
		data[i].Q[4] = 0
		data[i].Q[5] = 0
		data[i].Q[6] = 0
		data[i].Q[7] = 0

		if qualityChange {
			if i == 2 {
				data[i].Q[0] = 1
			} else if i == 3 {
				data[i].Q[0] = 0x41
			}
		}
	}
	return data
}

func createInputDataDualIED(ied1 *iedemulator.IEDEmulator, ied2 *iedemulator.IEDEmulator, samples int, countOfVariables int, qualityChange bool) []streamprotocol.DatasetWithQuality {
	var data []streamprotocol.DatasetWithQuality = make([]streamprotocol.DatasetWithQuality, samples)
	for i := range data {
		data[i].Int32s = make([]int32, countOfVariables)
		data[i].Q = make([]uint32, countOfVariables)
	}

	// generate data using IED emulator
	// the timestamp is a simple integer counter, starting from 0
	for i := range data {
		// compute emulated waveform data
		ied1.Step()
		ied2.Step()

		// calculate timestamp
		data[i].T = uint64(i)

		// set waveform data
		data[i].Int32s[0] = int32(ied1.V.A * 100.0)
		data[i].Int32s[1] = int32(ied1.V.B * 100.0)
		data[i].Int32s[2] = int32(ied1.V.C * 100.0)
		data[i].Int32s[3] = int32((ied1.V.A + ied1.V.B + ied1.V.C) * 100.0)
		data[i].Int32s[4] = int32(ied2.V.A * 100.0)
		data[i].Int32s[5] = int32(ied2.V.B * 100.0)
		data[i].Int32s[6] = int32(ied2.V.C * 100.0)
		data[i].Int32s[7] = int32((ied2.V.A + ied2.V.B + ied2.V.C) * 100.0)

		data[i].Int32s[8] = int32(ied1.I.A * 1000.0)
		data[i].Int32s[9] = int32(ied1.I.B * 1000.0)
		data[i].Int32s[10] = int32(ied1.I.C * 1000.0)
		data[i].Int32s[11] = int32((ied1.I.A + ied1.I.B + ied1.I.C) * 1000.0)
		data[i].Int32s[12] = int32(ied2.I.A * 1000.0)
		data[i].Int32s[13] = int32(ied2.I.B * 1000.0)
		data[i].Int32s[14] = int32(ied2.I.C * 1000.0)
		data[i].Int32s[15] = int32((ied2.I.A + ied2.I.B + ied2.I.C) * 1000.0)

		// set quality data
		data[i].Q[0] = 0
		data[i].Q[1] = 0
		data[i].Q[2] = 0
		data[i].Q[3] = 0
		data[i].Q[4] = 0
		data[i].Q[5] = 0
		data[i].Q[6] = 0
		data[i].Q[7] = 0
		data[i].Q[8] = 0
		data[i].Q[9] = 0
		data[i].Q[10] = 0
		data[i].Q[11] = 0
		data[i].Q[12] = 0
		data[i].Q[13] = 0
		data[i].Q[14] = 0
		data[i].Q[15] = 0

		if qualityChange {
			if i == 2 {
				data[i].Q[0] = 1
			} else if i == 3 {
				data[i].Q[0] = 0x41
			}
		}
	}
	return data
}

type encodeStats struct {
	samples          int
	messages         int
	totalBytes       int
	totalHeaderBytes int
}

const earlyEncodingStopSamples = 100

func encodeAndDecode(t *testing.T, data *[]streamprotocol.DatasetWithQuality, enc *streamprotocol.Encoder, dec *streamprotocol.Decoder, countOfVariables int, samplesPerMessage int, earlyEncodingStop bool) (*encodeStats, error) {
	encodeStats := encodeStats{}
	totalSamplesRead := 0

	for i := range *data {
		encodeStats.samples++
		buf, length, errorEncode := enc.Encode(&((*data)[i]))
		if errorEncode != nil {
			return nil, errorEncode
		}

		// simulate encoding stopping early
		if earlyEncodingStop && length == 0 && i == (earlyEncodingStopSamples-1) {
			buf, length, _ = enc.EndEncode()
		}

		if length > 0 {
			// generate average stats
			encodeStats.messages++
			encodeStats.totalBytes += length
			encodeStats.totalHeaderBytes += 24

			errDecode := dec.DecodeToBuffer(buf, length)
			if errDecode != nil {
				return nil, errDecode
			}

			// compare decoded output
			if t != nil {
				for i := range dec.Out {
					// only check up to samples encoded
					if earlyEncodingStop && i >= earlyEncodingStopSamples {
						break
					}

					for j := 0; j < dec.Int32Count; j++ {
						if !assert.Equal(t, (*data)[totalSamplesRead+i].Int32s[j], dec.Out[i].Int32s[j]) {
							// fmt.Println("error at", i, j)
							t.FailNow()
						}
						// fmt.Println("fine at", i, j, (*data)[totalSamplesRead+i].Int32s[j], dec.Out[i].Int32s[j])
						if !assert.Equal(t, (*data)[totalSamplesRead+i].Q[j], dec.Out[i].Q[j]) {
							// fmt.Println("Q fail:", (*data)[totalSamplesRead+i].Q[j], dec.Out[i].Q[j], i, j)
							t.FailNow()
						}
					}
				}
			}

			totalSamplesRead += enc.SamplesPerMessage

			if earlyEncodingStop {
				return &encodeStats, nil
			}
		}
	}

	return &encodeStats, nil
}

func TestEncodeDecode(t *testing.T) {
	// prepare table for presenting results
	tab := table.NewWriter()
	tab.SetOutputMirror(os.Stdout)
	tab.SetStyle(table.StyleLight)
	tab.AppendHeader(table.Row{"samples", "sampling\nrate", "samples\nper message", "messages", "quality\nchange", "early\nencode stop", "spatial\nrefs", "size\n(bytes)", "size\n(%)"})

	keys := make([]string, 0, len(tests))
	for k := range tests {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		t.Run(name, func(t *testing.T) {
			// t.Parallel()
			test := tests[name]

			// settings for IED emulator
			var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate, 0)

			// initialise data structure for input data
			var data []streamprotocol.DatasetWithQuality
			if test.countOfVariables == 16 {
				var ied2 *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate, 0)
				data = createInputDataDualIED(ied, ied2, test.samples, test.countOfVariables, test.qualityChange)
			} else {
				data = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)
			}

			// create encoder and decoder
			stream := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
			// streamDecoder := streamprotocol.NewChannelDecoder(ID, test.countOfVariables, test.samplingRate)
			streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

			if test.useSpatialRefs {
				stream.SetSpatialRefs(test.countOfVariables, test.countOfVariables/8, test.countOfVariables/8, true)        // TODO test includeNeutral
				streamDecoder.SetSpatialRefs(test.countOfVariables, test.countOfVariables/8, test.countOfVariables/8, true) // TODO test includeNeutral
			}

			// encode the data
			// when each message is complete, decode
			encodeStats, _ := encodeAndDecode(t, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage, test.earlyEncodingStop)

			theoryBytesPerMessage := test.countOfVariables * test.samplesPerMessage * 16

			if test.earlyEncodingStop {
				theoryBytesPerMessage = test.countOfVariables * encodeStats.samples * 16
			}
			meanBytesPerMessage := float64(encodeStats.totalBytes) / float64(encodeStats.messages) // includes header overhead
			percent := 100 * float64(meanBytesPerMessage) / float64(theoryBytesPerMessage)
			// meanBytesWithoutHeader := float64(encodeStats.totalBytes-encodeStats.totalHeaderBytes) / float64(encodeStats.iterations)

			tab.AppendRow([]interface{}{
				encodeStats.samples,
				tests[name].samplingRate,
				tests[name].samplesPerMessage,
				encodeStats.messages,
				tests[name].qualityChange,
				tests[name].earlyEncodingStop,
				tests[name].useSpatialRefs,
				fmt.Sprintf("%.1f", meanBytesPerMessage),
				fmt.Sprintf("%.1f", percent),
			})
			// tab.AppendSeparator()
		})
	}

	// show table of results
	tab.Render()
	// tab.RenderCSV()
}

func TestWrongID(t *testing.T) {
	t.Run("wrong ID", func(t *testing.T) {
		if _, ok := tests["a10-1"]; ok {
			test := tests["a10-1"]

			// settings for IED emulator
			var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate, 0)
			var wrongID uuid.UUID = uuid.New()

			// initialise data structure for input data
			var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

			// create encoder and decoder
			stream := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
			streamDecoder := streamprotocol.NewDecoder(wrongID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

			// encode the data
			// when each message is complete, decode
			_, err := encodeAndDecode(t, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage, test.earlyEncodingStop)
			assert.Equal(t, err.Error(), "IDs did not match")
		} else {
			t.Log("Test data missing")
			t.Fail()
		}
	})
}
