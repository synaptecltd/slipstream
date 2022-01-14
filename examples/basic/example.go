package main

import (
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/guptarohit/asciigraph"
	"github.com/synaptecltd/emulator"
	"github.com/synaptecltd/slipstream"
)

func main() {
	// define settings
	uuid := uuid.New()
	variablePerSample := 8   // number of "variables", such as voltages or currents. 8 is equivalent to IEC 61850-9-2 LE
	samplingRate := 4800     // Hz
	samplesPerMessage := 480 // each message contains 100 ms of data

	// initialise an encoder
	enc := slipstream.NewEncoder(uuid, variablePerSample, samplingRate, samplesPerMessage)

	// use the Synaptec "emulator" library to generate three-phase voltage and current test signals
	emulator := &emulator.Emulator{
		SamplingRate: samplingRate,
		Fnom:         50.0,
		Ts:           1 / float64(samplingRate),
		V: &emulator.ThreePhaseEmulation{
			PosSeqMag: 400000.0 / math.Sqrt(3) * math.Sqrt(2),
		},
		I: &emulator.ThreePhaseEmulation{
			PosSeqMag: 500.0,
		},
	}

	// use emulator to generate test data
	samplesToEncode := 480 // equates to 1 full message
	data := createInputData(emulator, samplesToEncode, variablePerSample)

	// loop through data samples and encode into Slipstream format
	for d := range data {
		buf, length, err := enc.Encode(&data[d])

		// check if message encoding has finished (or an error occurred)
		if err == nil && length > 0 {
			// buf should now contain an encoded message, and can be send over the network or stored
			fmt.Println("message size:", len(buf), "bytes")

			// initialise a decoder
			dec := slipstream.NewDecoder(uuid, variablePerSample, samplingRate, samplesPerMessage)

			// decode the message
			errDecode := dec.DecodeToBuffer(buf, length)

			// iterate through the decoded samples
			if errDecode == nil {
				var decodedData []float64 = make([]float64, samplesToEncode)
				for i := range dec.Out {
					// extract the phase A current values and convert to Amps
					decodedData[i] = float64(dec.Out[i].Int32s[0]) / 1000.0
					// for j := 0; j < dec.Int32Count; j++ {
					// 	fmt.Println("timestamp:", dec.Out[i].T, "value:", dec.Out[i].Int32s[j], "quality:", dec.Out[i].Q[j])
					// }
				}
				fmt.Println("Decoded phase A current data:")
				graph := asciigraph.Plot(decodedData, asciigraph.Height(10), asciigraph.Width(80))
				fmt.Println(graph)
			}
		}
	}
}

func createInputData(ied *emulator.Emulator, samples int, countOfVariables int) []slipstream.DatasetWithQuality {
	var data []slipstream.DatasetWithQuality = make([]slipstream.DatasetWithQuality, samples)
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
	}

	return data
}
