package streamprotocol_test

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/synaptec/synthesis/iedemulator"
	"github.com/synaptec/synthesis/streamprotocol"
)

func TestBasic(t *testing.T) {
	// settings for stream protocol
	ID := uuid.New()
	// countOfVariables := 3
	samplingRate := 4000
	// samples := 10
	// samplesPerMessage := 2

	tests := map[string]struct {
		// samplingRate      int
		countOfVariables  int
		samples           int
		samplesPerMessage int
	}{
		"1": {countOfVariables: 3, samples: 10, samplesPerMessage: 1},
		"2": {countOfVariables: 3, samples: 10, samplesPerMessage: 2},
		"3": {countOfVariables: 3, samples: 4000, samplesPerMessage: 2},
		"4": {countOfVariables: 3, samples: 4000, samplesPerMessage: 4000},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// settings for IED emulator
			var ied iedemulator.IEDEmulator = iedemulator.IEDEmulator{
				SamplingRate: samplingRate,
				Fnom:         50.0,
				Fdeviation:   0.0,
				Ts:           1 / float64(samplingRate),
				V: iedemulator.ThreePhaseEmulation{
					PosSeqMag: 275000.0 / math.Sqrt(3) * math.Sqrt(2),
					NegSeqAng: 0.0,
					NoiseMax:  0.002,
				},
				I: iedemulator.ThreePhaseEmulation{
					PosSeqMag:       500.0,
					NegSeqAng:       0.0, //-0.646,
					HarmonicNumbers: []float64{5, 7, 11, 13, 17, 19, 23, 25},
					HarmonicMags:    []float64{0.2164, 0.1242, 0.0892, 0.0693, 0.0541, 0.0458, 0.0370, 0.0332},
					HarmonicAngs:    []float64{0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0}, //{171.5, 100.4, -52.4, 128.3, 80.0, 2.9, -146.8, 133.9},
					NoiseMax:        0.002,
				},
			}

			// initialise data structure for input data
			var data []streamprotocol.DatasetWithQuality = make([]streamprotocol.DatasetWithQuality, test.samples)
			for i := range data {
				data[i].Int32s = make([]int32, test.countOfVariables)
				data[i].Q = make([]uint32, test.countOfVariables)
			}

			// generate data using IED emulator
			// the timestamp is a simple integer counter, starting from 0
			for i := range data {
				ied.Step()
				// set waveform data
				data[i].T = uint64(i)
				data[i].Int32s[0] = int32(ied.V.A * 1000.0)
				data[i].Int32s[1] = int32(ied.V.B * 1000.0)
				data[i].Int32s[2] = int32(ied.V.C * 1000.0)
				data[i].Q[0] = 0
				data[i].Q[1] = 0
				data[i].Q[2] = 0
			}

			// create encoder and decoder
			stream := streamprotocol.NewEncoder(ID, test.countOfVariables, samplingRate, test.samplesPerMessage)
			streamDecoder := streamprotocol.NewDecoder(ID, test.countOfVariables, samplingRate)

			var done sync.WaitGroup

			// create thread to decode
			done.Add(1)
			go func(ch chan streamprotocol.DatasetWithQuality, wg *sync.WaitGroup) {
				// defer close(streamDecoder.Ch)
				count := 0
				for {
					select {
					case <-time.After(1 * time.Millisecond):
						// fmt.Println("timeout")
						wg.Done()
						return
					case d := <-ch:
						// got data
						count++

						// compare decoded output
						i := d.T
						for j := 0; j < 1; j++ {
							assert.Equal(t, data[i].Int32s[j], d.Int32s[j])
							// if data[i].Int32s[j] != d.Int32s[j] {
							// 	t.Errorf("T = %d: data[i].Int32s[j] (%d) != d.Int32s[j] (%d)", i, data[i].Int32s[j], d.Int32s[j])
							// } else {
							// 	t.Logf("T = %d: data[i].Int32s[j] (%d) == d.Int32s[j] (%d) (ok)", i, data[i].Int32s[j], d.Int32s[j])
							// }
							// TODO check Q
						}
					}
				}
			}(streamDecoder.Ch, &done)

			// encode the data
			// when each message is complete, decode
			for i := range data {
				dataset := streamprotocol.Dataset{
					Int32s: make([]int32, len(data[i].Int32s)),
				}
				copy(dataset.Int32s, data[i].Int32s)
				// fmt.Println("ts in:", data[i].T)
				buf, len := stream.Encode(dataset, data[i].Q, data[i].T)

				if len > 0 {
					// fmt.Println("decoding")
					streamDecoder.Decode(buf, len)
				}
			}

			// wait for decoder thread to complete
			done.Wait()
		})
	}
}
