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
}{
	// "a10-1": {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 1},
	// "a10-2": {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 2},
	// "a10-2q":         {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 2, qualityChange: true},
	"a10-10": {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 10},
	// "a4-2q":          {samplingRate: 4000, countOfVariables: 8, samples: 4, samplesPerMessage: 2, qualityChange: true},
	// "a8-8q":          {samplingRate: 4000, countOfVariables: 8, samples: 8, samplesPerMessage: 8, qualityChange: true},
	// "b4000-2":        {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 2},
	// "b4000-80":       {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 80},
	// "b4000-60":       {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 60},
	// "b4000-4000":     {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 4000},
	// "c4800-2":        {samplingRate: 4800, countOfVariables: 8, samples: 4800, samplesPerMessage: 2},
	// "c4800-20":       {samplingRate: 4800, countOfVariables: 8, samples: 4800, samplesPerMessage: 20},
	// "d14400-6":       {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 6},
	// "d4000-4000q":    {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 4000, qualityChange: true},
	// "e14400-14400":   {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 14400},
	// "e14400-14400q":  {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 14400, qualityChange: true},
	// "f40000-40000":   {samplingRate: 4000, countOfVariables: 8, samples: 40000, samplesPerMessage: 40000},
	// "g150000-150000": {samplingRate: 150000, countOfVariables: 8, samples: 150000, samplesPerMessage: 150000},
}

func createIEDEmulator(samplingRate int) *iedemulator.IEDEmulator {
	return &iedemulator.IEDEmulator{
		SamplingRate: samplingRate,
		Fnom:         50.0,
		Fdeviation:   0.0,
		Ts:           1 / float64(samplingRate),
		V: iedemulator.ThreePhaseEmulation{
			PosSeqMag: 275000.0 / math.Sqrt(3) * math.Sqrt(2),
			// NoiseMax:  0.00001,
		},
		I: iedemulator.ThreePhaseEmulation{
			PosSeqMag:       500.0,
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
				var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate)

				// initialise data structure for input data
				var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

				// create encoder and decoder
				stream := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
				streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

				b.StartTimer()

				// encode the data
				// when each message is complete, decode
				encodeAndDecode(nil, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage)
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
				var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate)

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
						//  generate average stats
						// encodeStats.iterations++
						// encodeStats.totalBytes += len

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
				var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate)

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
		// fmt.Println("encoded:", i, data[i].Q[0])
	}
	return data
}

type encodeStats struct {
	iterations       int
	totalBytes       int
	totalHeaderBytes int
}

func encodeAndDecode(t *testing.T, data *[]streamprotocol.DatasetWithQuality, enc *streamprotocol.Encoder, dec *streamprotocol.Decoder, countOfVariables int, samplesPerMessage int) (*encodeStats, error) {
	encodeStats := encodeStats{}
	totalSamplesRead := 0

	for i := range *data {
		buf, length, errorEncode := enc.Encode(&((*data)[i]))
		if errorEncode != nil {
			return nil, errorEncode
		}

		if length > 0 {
			//  generate average stats
			encodeStats.iterations++
			encodeStats.totalBytes += length
			encodeStats.totalHeaderBytes += 24

			errDecode := dec.DecodeToBuffer(buf, length)
			if errDecode != nil {
				return nil, errDecode
			}

			// compare decoded output
			if t != nil {
				for i := range dec.Out {
					for j := 0; j < dec.Int32Count; j++ {
						if !assert.Equal(t, (*data)[totalSamplesRead+i].Int32s[j], dec.Out[i].Int32s[j]) {
							t.FailNow()
						}
						if !assert.Equal(t, (*data)[totalSamplesRead+i].Q[j], dec.Out[i].Q[j]) {
							// fmt.Println("Q fail:", (*data)[totalSamplesRead+i].Q[j], dec.Out[i].Q[j], i, j)
							t.FailNow()
						}

					}
				}
			}

			totalSamplesRead += enc.SamplesPerMessage
		}
	}

	if t != nil {
		// meanBytes := float64(encodeStats.totalBytes) / float64(encodeStats.iterations)
		// meanBytesWithoutHeader := float64(encodeStats.totalBytes-encodeStats.totalHeaderBytes) / float64(encodeStats.iterations)
		// theoryBytes := enc.SamplesPerMessage * enc.Int32Count * 16

		// t.Logf("%d messages", encodeStats.iterations)
		// t.Logf("average bytes per message: %.1f (theoretical: %d)", meanBytesWithoutHeader, theoryBytes)
		// t.Logf("average bytes per variable: %.1f (%.1f with header) %.1f%% efficiency",
		// 	meanBytesWithoutHeader/float64(countOfVariables*samplesPerMessage),
		// 	meanBytes/float64(countOfVariables*samplesPerMessage),
		// 	100.0*meanBytesWithoutHeader/float64(theoryBytes))
	}

	return &encodeStats, nil
}

func TestEncodeDecode(t *testing.T) {
	// prepare table for presenting results
	tab := table.NewWriter()
	tab.SetOutputMirror(os.Stdout)
	tab.SetStyle(table.StyleLight)
	tab.AppendHeader(table.Row{"samples", "sampling\nrate", "samples\nper message", "messages", "quality\nchange", "size (bytes)", "size (%)"})

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
			var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate)

			// initialise data structure for input data
			var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

			// create encoder and decoder
			stream := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
			// streamDecoder := streamprotocol.NewChannelDecoder(ID, test.countOfVariables, test.samplingRate)
			streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

			// encode the data
			// when each message is complete, decode
			encodeStats, _ := encodeAndDecode(t, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage)

			theoryBytes := tests[name].countOfVariables * tests[name].samplesPerMessage * 16
			meanBytes := float64(encodeStats.totalBytes) / float64(encodeStats.iterations) // includes header overhead
			percent := 100 * float64(meanBytes) / float64(theoryBytes)

			// meanBytesWithoutHeader := float64(encodeStats.totalBytes-encodeStats.totalHeaderBytes) / float64(encodeStats.iterations)

			tab.AppendRow([]interface{}{
				tests[name].samples,
				tests[name].samplingRate,
				tests[name].samplesPerMessage,
				encodeStats.iterations,
				tests[name].qualityChange,
				fmt.Sprintf("%.1f", meanBytes),
				fmt.Sprintf("%.1f", percent),
			})
			// tab.AppendSeparator()
		})
	}

	// show table of results
	tab.Render()
	tab.RenderCSV()
}

// func TestWrongID(t *testing.T) {
// 	t.Run("wrong ID", func(t *testing.T) {
// 		if _, ok := tests["a10-1"]; ok {
// 			// settings for IED emulator
// 			var ied *iedemulator.IEDEmulator = createIEDEmulator(tests["10-1"].samplingRate)
// 			var wrongID uuid.UUID = uuid.New()

// 			// initialise data structure for input data
// 			var data []streamprotocol.DatasetWithQuality = createInputData(ied, tests["10-1"].samples, tests["10-1"].countOfVariables, tests["10-1"].qualityChange)

// 			// create encoder and decoder
// 			stream := streamprotocol.NewEncoder(ID, tests["10-1"].countOfVariables, tests["10-1"].samplingRate, tests["10-1"].samplesPerMessage)
// 			streamDecoder := streamprotocol.NewDecoder(wrongID, tests["10-1"].countOfVariables, tests["10-1"].samplingRate, tests["10-1"].samplesPerMessage)

// 			// encode the data
// 			// when each message is complete, decode
// 			_, err := encodeAndDecode(t, &data, stream, streamDecoder, tests["10-1"].countOfVariables, tests["10-1"].samplesPerMessage)
// 			assert.Equal(t, err.Error(), "IDs did not match")
// 		} else {
// 			t.Log("Test data missing")
// 			t.Fail()
// 		}
// 	})
// }
