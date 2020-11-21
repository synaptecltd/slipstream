package streamprotocol_test

import (
	"math"
	"sort"
	"testing"

	"github.com/google/uuid"
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
	"10-1":          {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 1},
	"10-2":          {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 2},
	"10-10":         {samplingRate: 4000, countOfVariables: 8, samples: 10, samplesPerMessage: 10},
	"4000-2":        {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 2},
	"4800-2":        {samplingRate: 4800, countOfVariables: 8, samples: 4800, samplesPerMessage: 2},
	"4800-6":        {samplingRate: 4800, countOfVariables: 8, samples: 4800, samplesPerMessage: 20},
	"4000-80":       {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 80},
	"4000-60":       {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 67},
	"4000-4000":     {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 4000},
	"40000-40000":   {samplingRate: 4000, countOfVariables: 8, samples: 40000, samplesPerMessage: 40000},
	"4-2q":          {samplingRate: 4000, countOfVariables: 8, samples: 4, samplesPerMessage: 2, qualityChange: true},
	"4000-4000q":    {samplingRate: 4000, countOfVariables: 8, samples: 4000, samplesPerMessage: 4000, qualityChange: true},
	"14400-6":       {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 6},
	"14400-14400":   {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 14400},
	"14400-14400q":  {samplingRate: 14400, countOfVariables: 8, samples: 14400, samplesPerMessage: 14400, qualityChange: true},
	"150000-150000": {samplingRate: 150000, countOfVariables: 8, samples: 150000, samplesPerMessage: 150000},
}

func createIEDEmulator(samplingRate int) *iedemulator.IEDEmulator {
	return &iedemulator.IEDEmulator{
		SamplingRate: samplingRate,
		Fnom:         50.0,
		Fdeviation:   0.0,
		Ts:           1 / float64(samplingRate),
		V: iedemulator.ThreePhaseEmulation{
			PosSeqMag: 275000.0 / math.Sqrt(3) * math.Sqrt(2),
			// NoiseMax:  0.001,
		},
		I: iedemulator.ThreePhaseEmulation{
			PosSeqMag:       500.0,
			HarmonicNumbers: []float64{5, 7, 11, 13, 17, 19, 23, 25},
			HarmonicMags:    []float64{0.2164, 0.1242, 0.0892, 0.0693, 0.0541, 0.0458, 0.0370, 0.0332},
			HarmonicAngs:    []float64{0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0}, //{171.5, 100.4, -52.4, 128.3, 80.0, 2.9, -146.8, 133.9},
			// NoiseMax: 0.001,
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
				if b != nil {
					b.StopTimer()
				}

				test := tests[name]

				// settings for IED emulator
				var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate)

				// initialise data structure for input data
				var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

				// create encoder and decoder
				stream := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
				streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

				// var done sync.WaitGroup

				// create thread to decode
				// done.Add(1)
				// go listenAndCheckDecoder(nil, streamDecoder.Ch, &data, &done)

				if b != nil {
					b.StartTimer()
				}

				// encode the data
				// when each message is complete, decode
				encodeAndDecode(nil, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage)

				// wait for decoder thread to complete
				// done.Wait()
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
				// dec := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate)

				encodeStats := encodeStats{}

				for i := range data {
					b.StartTimer()
					_, len, _ := enc.Encode(&data[i])
					b.StopTimer()

					if len > 0 {
						// b.StopTimer()

						//  generate average stats
						encodeStats.iterations++
						encodeStats.totalBytes += len

						// fmt.Println("decoding")
						// dec.DecodeToBuffer(buf, len)
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

				encodeStats := encodeStats{}

				for i := range data {
					buf, len, _ := enc.Encode(&data[i])

					if len > 0 {
						//  generate average stats
						encodeStats.iterations++
						encodeStats.totalBytes += len

						b.StartTimer()

						// fmt.Println("decoding")
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

	// soc := uint32(time.Now().Unix())

	// generate data using IED emulator
	// the timestamp is a simple integer counter, starting from 0
	for i := range data {
		// compute emulated waveform data
		ied.Step()

		// calculate timestamp
		// bs := make([]byte, 8)
		// // TODO big endian?
		// binary.LittleEndian.PutUint32(bs, soc)
		// binary.LittleEndian.PutUint32(bs[4:8], uint32(i))
		// ts := binary.LittleEndian.Uint64(bs)
		// data[i].T = ts
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
		// fmt.Println("encoded:", data[i].Q[0])
	}
	return data
}

type encodeStats struct {
	iterations       int
	totalBytes       int
	totalHeaderBytes int
}

func encodeAndDecode(t *testing.T, data *[]streamprotocol.DatasetWithQuality, enc *streamprotocol.Encoder, dec *streamprotocol.Decoder, countOfVariables int, samplesPerMessage int) error {
	encodeStats := encodeStats{}
	totalSamplesRead := 0

	for i := range *data {
		// fmt.Println("ts in:", data[i].T)
		buf, length, errorEncode := enc.Encode(&((*data)[i]))
		if errorEncode != nil {
			return errorEncode
		}

		if length > 0 {
			//  generate average stats
			encodeStats.iterations++
			encodeStats.totalBytes += length
			encodeStats.totalHeaderBytes += 24

			// fmt.Println("decoding")
			errDecode := dec.DecodeToBuffer(buf, length)
			if errDecode != nil {
				return errDecode
			}

			// compare decoded output
			if t != nil {
				for i := range dec.Out {
					for j := 0; j < dec.Int32Count; j++ {
						// fmt.Println((*data)[totalSamplesRead+i].Int32s[j], dec.Out[i].Int32s[j])
						if !assert.Equal(t, (*data)[totalSamplesRead+i].Int32s[j], dec.Out[i].Int32s[j]) {
							t.FailNow()
						}
						// TODO re-enable
						// if !assert.Equal(t, (*data)[totalSamplesRead+i].Q[j], dec.Out[i].Q[j]) {
						// 	t.FailNow()
						// }

					}
				}
			}

			totalSamplesRead += len(dec.Out)
		}
	}

	if t != nil {
		// TODO enc.SamplingRate is correct?
		meanBytes := float64(encodeStats.totalBytes) / float64(encodeStats.iterations)
		meanBytesWithoutHeader := float64(encodeStats.totalBytes-encodeStats.totalHeaderBytes) / float64(encodeStats.iterations)
		theoryBytes := /*enc.SamplingRate * */ enc.SamplesPerMessage * enc.Int32Count * 16

		t.Logf("%d messages", encodeStats.iterations)
		t.Logf("average bytes per message: %.1f (theoretical: %d)", meanBytesWithoutHeader, theoryBytes)
		t.Logf("average bytes per variable: %.1f (%.1f with header) %.1f%% efficiency",
			meanBytesWithoutHeader/float64(countOfVariables*samplesPerMessage),
			meanBytes/float64(countOfVariables*samplesPerMessage),
			100.0*meanBytesWithoutHeader/float64(theoryBytes))
	}

	return nil
}

// func listenAndCheckDecoder(t *testing.T, ch chan streamprotocol.DatasetWithQuality, data *[]streamprotocol.DatasetWithQuality, wg *sync.WaitGroup) {
// 	// defer close(streamDecoder.Ch)
// 	for {
// 		select {
// 		case <-time.After(1 * time.Microsecond):
// 			wg.Done()
// 			return
// 		case d := <-ch:
// 			i := d.T

// 			// compare decoded output
// 			for j := 0; j < 1; j++ {
// 				if t != nil {
// 					assert.Equal(t, (*data)[i].Int32s[j], d.Int32s[j])
// 					assert.Equal(t, (*data)[i].Q[j], d.Q[j])
// 				}
// 				// if data[i].Int32s[j] != d.Int32s[j] {
// 				// 	t.Errorf("T = %d: data[i].Int32s[j] (%d) != d.Int32s[j] (%d)", i, data[i].Int32s[j], d.Int32s[j])
// 				// } else {
// 				// 	t.Logf("T = %d: data[i].Int32s[j] (%d) == d.Int32s[j] (%d) (ok)", i, data[i].Int32s[j], d.Int32s[j])
// 				// }
// 			}
// 		}
// 	}
// }

func TestEncodeDecode(t *testing.T) {
	// settings for stream protocol
	// countOfVariables := 3
	// samples := 10
	// samplesPerMessage := 2

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// t.Parallel()

			// settings for IED emulator
			var ied *iedemulator.IEDEmulator = createIEDEmulator(test.samplingRate)

			// initialise data structure for input data
			var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

			// create encoder and decoder
			stream := streamprotocol.NewEncoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)
			// streamDecoder := streamprotocol.NewChannelDecoder(ID, test.countOfVariables, test.samplingRate)
			streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, test.samplingRate, test.samplesPerMessage)

			// var done sync.WaitGroup

			// create thread to decode
			// done.Add(1)
			// go listenAndCheckDecoder(t, streamDecoder.Ch, &data, &done)

			// encode the data
			// when each message is complete, decode
			encodeAndDecode(t, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage)

			// wait for decoder thread to complete
			// done.Wait()
		})
	}
}

func TestWrongID(t *testing.T) {
	t.Run("wrong ID", func(t *testing.T) {
		if _, ok := tests["10-1"]; ok {
			// settings for IED emulator
			var ied *iedemulator.IEDEmulator = createIEDEmulator(tests["10-1"].samplingRate)
			var wrongID uuid.UUID = uuid.New()

			// initialise data structure for input data
			var data []streamprotocol.DatasetWithQuality = createInputData(ied, tests["10-1"].samples, tests["10-1"].countOfVariables, tests["10-1"].qualityChange)

			// create encoder and decoder
			stream := streamprotocol.NewEncoder(ID, tests["10-1"].countOfVariables, tests["10-1"].samplingRate, tests["10-1"].samplesPerMessage)
			// streamDecoder := streamprotocol.NewChannelDecoder(ID, test.countOfVariables, test.samplingRate)
			streamDecoder := streamprotocol.NewDecoder(wrongID, tests["10-1"].countOfVariables, tests["10-1"].samplingRate, tests["10-1"].samplesPerMessage)

			// var done sync.WaitGroup

			// create thread to decode
			// done.Add(1)
			// go listenAndCheckDecoder(t, streamDecoder.Ch, &data, &done)

			// encode the data
			// when each message is complete, decode
			err := encodeAndDecode(t, &data, stream, streamDecoder, tests["10-1"].countOfVariables, tests["10-1"].samplesPerMessage)
			assert.Equal(t, err.Error(), "IDs did not match")

			// wait for decoder thread to complete
			// done.Wait()
		} else {
			t.Log("Test data missing")
			t.Fail() // TODO restore
		}

		// TODO add error handling
	})
}
