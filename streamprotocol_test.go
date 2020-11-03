package streamprotocol_test

import (
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/synaptec/synthesis/iedemulator"
	"github.com/synaptec/synthesis/streamprotocol"
)

var ID = uuid.New()
var samplingRate = 4000

var tests = map[string]struct {
	// samplingRate      int
	countOfVariables  int
	samples           int
	samplesPerMessage int
	qualityChange     bool // TODO test fail due to not implemented yet
}{
	"10-1":      {countOfVariables: 6, samples: 10, samplesPerMessage: 1},
	"10-2":      {countOfVariables: 6, samples: 10, samplesPerMessage: 2},
	"4000-2":    {countOfVariables: 6, samples: 4000, samplesPerMessage: 2},
	"4000-6":    {countOfVariables: 6, samples: 4000, samplesPerMessage: 6},
	"4000-50":   {countOfVariables: 6, samples: 4000, samplesPerMessage: 80},
	"4000-60":   {countOfVariables: 6, samples: 4000, samplesPerMessage: 67},
	"4000-4000": {countOfVariables: 6, samples: 4000, samplesPerMessage: 4000, qualityChange: false},
	// "4000-4000q": {countOfVariables: 6, samples: 4000, samplesPerMessage: 4000, qualityChange: true},
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
				var ied *iedemulator.IEDEmulator = createIEDEmulator(samplingRate)

				// initialise data structure for input data
				var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

				if b != nil {
					b.StartTimer()
				}

				// create encoder and decoder
				stream := streamprotocol.NewEncoder(ID, test.countOfVariables, samplingRate, test.samplesPerMessage)
				streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, samplingRate)

				var done sync.WaitGroup

				// create thread to decode
				done.Add(1)
				go listenAndCheckDecoder(nil, streamDecoder.Ch, &data, &done)

				// encode the data
				// when each message is complete, decode
				encodeAndDecode(nil, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage)

				// wait for decoder thread to complete
				done.Wait()
			}
		})
	}
}

func createIEDEmulator(samplingRate int) *iedemulator.IEDEmulator {
	return &iedemulator.IEDEmulator{
		SamplingRate: samplingRate,
		Fnom:         50.0,
		Fdeviation:   0.0,
		Ts:           1 / float64(samplingRate),
		V: iedemulator.ThreePhaseEmulation{
			PosSeqMag: 275000.0 / math.Sqrt(3) * math.Sqrt(2),
			NoiseMax:  0.002,
		},
		I: iedemulator.ThreePhaseEmulation{
			PosSeqMag:       500.0,
			HarmonicNumbers: []float64{5, 7, 11, 13, 17, 19, 23, 25},
			HarmonicMags:    []float64{0.2164, 0.1242, 0.0892, 0.0693, 0.0541, 0.0458, 0.0370, 0.0332},
			HarmonicAngs:    []float64{0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0}, //{171.5, 100.4, -52.4, 128.3, 80.0, 2.9, -146.8, 133.9},
			NoiseMax:        0.002,
		},
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

		// // calculate timestamp
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
		data[i].Int32s[3] = int32(ied.V.A * 100.0)
		data[i].Int32s[4] = int32(ied.V.B * 100.0)
		data[i].Int32s[5] = int32(ied.V.C * 100.0)

		// set quality data
		data[i].Q[0] = 0
		data[i].Q[1] = 0
		data[i].Q[2] = 0
		data[i].Q[3] = 0
		data[i].Q[4] = 0
		data[i].Q[5] = 0
		if i == 2 {
			if qualityChange {
				data[i].Q[0] = 1
			}
		}
	}
	return data
}

type encodeStats struct {
	iterations int
	totalBytes int
}

func encodeAndDecode(t *testing.T, data *[]streamprotocol.DatasetWithQuality, enc *streamprotocol.Encoder, dec *streamprotocol.Decoder, countOfVariables int, samplesPerMessage int) error {
	encodeStats := encodeStats{}

	for i := range *data {
		dataset := streamprotocol.Dataset{
			Int32s: make([]int32, len((*data)[i].Int32s)),
		}
		copy(dataset.Int32s, (*data)[i].Int32s)
		// fmt.Println("ts in:", data[i].T)
		buf, len := enc.Encode(dataset, (*data)[i].Q, (*data)[i].T)

		if len > 0 {
			// TODO generate average stats
			encodeStats.iterations++
			encodeStats.totalBytes += len

			// fmt.Println("decoding")
			err := dec.DecodeToChannel(buf, len)
			if err != nil {
				return err
			}
		}
	}

	if t != nil {
		t.Logf("%d messages, average bytes per message: %.1f, average bytes per variable: %.1f",
			encodeStats.iterations,
			float64(encodeStats.totalBytes)/float64(encodeStats.iterations),
			float64(encodeStats.totalBytes)/float64(encodeStats.iterations*countOfVariables*samplesPerMessage))
	}

	return nil
}

func listenAndCheckDecoder(t *testing.T, ch chan streamprotocol.DatasetWithQuality, data *[]streamprotocol.DatasetWithQuality, wg *sync.WaitGroup) {
	// defer close(streamDecoder.Ch)
	for {
		select {
		case <-time.After(1 * time.Microsecond):
			wg.Done()
			return
		case d := <-ch:
			i := d.T
			// compare decoded output

			// TODO needs more complicated method for timestamp to slice offset for comparison with data param
			//      or just set to sample number for test
			// i := d.Iteration
			// fmt.Println(i, d.T)
			for j := 0; j < 1; j++ {
				if t != nil {
					// if i == 999999999 {
					assert.Equal(t, (*data)[i].Int32s[j], d.Int32s[j])
					assert.Equal(t, (*data)[i].Q[j], d.Q[j])
					// }
				}
				// if data[i].Int32s[j] != d.Int32s[j] {
				// 	t.Errorf("T = %d: data[i].Int32s[j] (%d) != d.Int32s[j] (%d)", i, data[i].Int32s[j], d.Int32s[j])
				// } else {
				// 	t.Logf("T = %d: data[i].Int32s[j] (%d) == d.Int32s[j] (%d) (ok)", i, data[i].Int32s[j], d.Int32s[j])
				// }
			}
		}
	}
}

func TestEncodeDecode(t *testing.T) {
	// settings for stream protocol
	// countOfVariables := 3
	// samples := 10
	// samplesPerMessage := 2

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// t.Parallel()

			// settings for IED emulator
			var ied *iedemulator.IEDEmulator = createIEDEmulator(samplingRate)

			// initialise data structure for input data
			var data []streamprotocol.DatasetWithQuality = createInputData(ied, test.samples, test.countOfVariables, test.qualityChange)

			// create encoder and decoder
			stream := streamprotocol.NewEncoder(ID, test.countOfVariables, samplingRate, test.samplesPerMessage)
			streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, samplingRate)

			var done sync.WaitGroup

			// create thread to decode
			done.Add(1)
			go listenAndCheckDecoder(t, streamDecoder.Ch, &data, &done)

			// encode the data
			// when each message is complete, decode
			encodeAndDecode(t, &data, stream, streamDecoder, test.countOfVariables, test.samplesPerMessage)

			// wait for decoder thread to complete
			done.Wait()
		})
	}
}

func TestWrongID(t *testing.T) {
	t.Run("wrong ID", func(t *testing.T) {
		// settings for IED emulator
		var ied *iedemulator.IEDEmulator = createIEDEmulator(samplingRate)
		var wrongID uuid.UUID = uuid.New()

		// initialise data structure for input data
		var data []streamprotocol.DatasetWithQuality = createInputData(ied, tests["10-1"].samples, tests["10-1"].countOfVariables, tests["10-1"].qualityChange)

		// create encoder and decoder
		stream := streamprotocol.NewEncoder(ID, tests["10-1"].countOfVariables, samplingRate, tests["10-1"].samplesPerMessage)
		streamDecoder := streamprotocol.NewDecoder(wrongID, tests["10-1"].countOfVariables, samplingRate)

		var done sync.WaitGroup

		// create thread to decode
		done.Add(1)
		go listenAndCheckDecoder(t, streamDecoder.Ch, &data, &done)

		// encode the data
		// when each message is complete, decode
		err := encodeAndDecode(t, &data, stream, streamDecoder, tests["10-1"].countOfVariables, tests["10-1"].samplesPerMessage)
		assert.Equal(t, err.Error(), "IDs did not match")

		// wait for decoder thread to complete
		done.Wait()

		// TODO add error handling
	})
}
