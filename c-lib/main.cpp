
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include <chrono>
#include <cstring>
// #include <time.h>
#include "c-main.h"

#define INTEGER_SCALING_I   1000.0
#define INTEGER_SCALING_V   100.0
#define PI                  3.1415926535897932384626433832795
#define TWO_PI_OVER_THREE   2.0943951023931954923084289221863
#define MAG_I               500
#define MAG_V               10000
#define FNOM                50.01
#define NOISE_MAX           0.01

double random(double min, double max) {
    double range = (max - min); 
    double div = RAND_MAX / range;
    return min + (rand() / div);
}

int32_t getSample(double t, bool isVoltage, double phase) {
    double scaling = INTEGER_SCALING_I;
    double mag = MAG_I;
    if (isVoltage) {
        scaling = INTEGER_SCALING_V;
        mag = MAG_V;
    }
    return (int32_t) (scaling * (mag * sin(2*PI*FNOM*t + phase) + random(-NOISE_MAX, NOISE_MAX)));
}

struct DatasetWithQuality *allocateSamples(int int32Count, int samplesPerMessage) {
    struct DatasetWithQuality *samples;
    samples = (struct DatasetWithQuality*) malloc(samplesPerMessage * sizeof(struct DatasetWithQuality));
    for (int s = 0; s < samplesPerMessage; s++) {
        samples[s].T = s;
        samples[s].Int32s = (int32_t*) malloc(int32Count * sizeof(int32_t));
        samples[s].Q = (uint32_t*) malloc(int32Count * sizeof(uint32_t));
    }
    return samples;
}

void freeSamples(struct DatasetWithQuality *samples, int samplesPerMessage) {
    for (int s = 0; s < samplesPerMessage; s++) {
        free(samples[s].Int32s);
        free(samples[s].Q);
    }
    free(samples);
}

typedef struct SlipstreamTest {
    // encoder/decoder settings
    int int32Count;
    int samplingRate;
    int samplesPerMessage;

    // Slipstream UUID
    GoUint8 ID_bytes[16];
    GoSlice ID;

    // vars for storing encoding/decoding status
    int encodedSamples = 0;
    int encodedLength = 0;
    bool decoded = false;

    // storage for data samples, for input to encoder and output of decoder
    struct DatasetWithQuality *samples;
    struct DatasetWithQuality *samplesOut;

    // define timers
    std::chrono::high_resolution_clock::time_point start, endEncode, endAll, startDecode, endDecode, endProcessedDecodeOutput;
} SlipstreamTest;

void initialiseTestParams(SlipstreamTest *test, GoUint8 ID_bytes[16]) {
    test->int32Count = 8;
    test->samplingRate = 4000;
    test->samplesPerMessage = 4000;

    memcpy(test->ID_bytes, ID_bytes, 16*sizeof(GoUint8));
    GoSlice ID = {test->ID_bytes, 16, 16};

    test->samples = allocateSamples(test->int32Count, test->samplesPerMessage);
    test->samplesOut = allocateSamples(test->int32Count, test->samplesPerMessage);
}

int main() {
    printf("using Go lib from C/C++\n");

    // seed random number for measurement noise
    srand(0);

    SlipstreamTest batchEncodeDecode = {0};
    GoUint8 ID_bytes[16] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};
    initialiseTestParams(&batchEncodeDecode, ID_bytes);

    SlipstreamTest iterativeEncode = {0};
    GoUint8 ID2_bytes[16] = {2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 5};
    initialiseTestParams(&batchEncodeDecode, ID2_bytes);

    // initialise some UUIDs as Go slides of 16 bytes
    // GoUint8 ID_bytes[16] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};
    GoSlice ID = {ID_bytes, 16, 16};
    GoSlice ID2 = {ID2_bytes, 16, 16};

    // encoder/decoder settings
    const int int32Count = 8;
    const int samplingRate = 4000;
    const int samplesPerMessage = 4000;

    // pre-calculate all data samples
    struct DatasetWithQuality *samples = allocateSamples(int32Count, samplesPerMessage);;
    for (int s = 0; s < samplesPerMessage; s++) {
        // emulate three-phase current and voltage waveform samples
        double t = (double) s / (double) samplingRate;
        samples[s].Int32s[0] = getSample(t, false, 0.0);
        samples[s].Int32s[1] = getSample(t, false, -TWO_PI_OVER_THREE);
        samples[s].Int32s[2] = getSample(t, false, TWO_PI_OVER_THREE);
        samples[s].Int32s[3] = samples[s].Int32s[0] + samples[s].Int32s[1] + samples[s].Int32s[2];
        samples[s].Int32s[4] = getSample(t, true, 0.0);
        samples[s].Int32s[5] = getSample(t, true, -TWO_PI_OVER_THREE);
        samples[s].Int32s[6] = getSample(t, true, TWO_PI_OVER_THREE);
        samples[s].Int32s[7] = samples[s].Int32s[4] + samples[s].Int32s[5] + samples[s].Int32s[6];

        // set quality values
        for (int i = 0; i < int32Count; i++) {
            samples[s].Q[i] = 0;
            // printf("%d\n", sample.Int32s[i]);
        }
    }

    // pre-allocate storage for decoder output
    struct DatasetWithQuality *samplesOut = allocateSamples(int32Count, samplesPerMessage);

    // create encoders
    NewEncoder(ID, int32Count, samplingRate, samplesPerMessage);
    NewEncoder(ID2, int32Count, samplingRate, samplesPerMessage);

    // create decoders
    NewDecoder(ID, int32Count, samplingRate, samplesPerMessage);
    NewDecoder(ID2, int32Count, samplingRate, samplesPerMessage);

    // vars for storing encoding/decoding status
    int encodedSamples, encodedLength = 0;
    bool decoded = false;

    // define timers
    std::chrono::high_resolution_clock::time_point start, endEncode, endAll, startDecode, endDecode, endProcessedDecodeOutput;
    start = std::chrono::high_resolution_clock::now();

    // perform encoding of all samples
    struct EncodeAll_return retAll = EncodeAll(ID2, samples, samplesPerMessage);
    endEncode = std::chrono::high_resolution_clock::now();

    // check if encoded data is available, then attempt decoding of data bytes
    if (retAll.r0 > 0) {
        startDecode = std::chrono::high_resolution_clock::now();
        decoded = Decode(ID2, retAll.r1, retAll.r0);
        endDecode = std::chrono::high_resolution_clock::now();
    }

    if (decoded == true) {
        bool ok = GetDecoded(ID2, samplesOut, samplesPerMessage);

        for (int s = 0; s < samplesPerMessage; s++) {
            for (int i = 0; i < int32Count; i++) {
                // sample_out = GetDecodedIndex(ID2, s, i);
                // if (sample_out.r0 == 0 || sample_out.r1 != samples[s].T || sample_out.r2 != samples[s].Int32s[i] || sample_out.r3 != samples[s].Q[i]) {
                //     printf("error: decode mismatch: %d, %d\n", s, i);
                // }

                if (samplesOut[s].T != samples[s].T || samplesOut[s].Int32s[i] != samples[s].Int32s[i] || samplesOut[s].Q[i] != samples[s].Q[i]) {
                    printf("error: decode mismatch: %d, %d (%d, %d)\n", s, i, samplesOut[s].Int32s[i], samples[s].Int32s[i]);
                }
            }
        }
    }
    endProcessedDecodeOutput = std::chrono::high_resolution_clock::now();

    endAll = std::chrono::high_resolution_clock::now();

    // performing encoding sample by sample. decoding is attempted once a full message is created.
    for (int s = 0; s < samplesPerMessage; s++) {
        // convert a single data sample to GoSlice
        GoSlice Int32s;
        Int32s.data = (void*) samples[s].Int32s;
        Int32s.len = int32Count;
        Int32s.cap = int32Count;
        GoSlice Q;
        Q.data = (void*) samples[s].Q;
        Q.len = int32Count;
        Q.cap = int32Count;

        // attempt encoding
        struct Encode_return ret = Encode(ID2, 0, Int32s, Q);

        // check for completed message
        if (ret.r0 > 0) {
            // endEncode = std::chrono::high_resolution_clock::now();

            encodedSamples = s;
            encodedLength = ret.r0;

            // startDecode = std::chrono::high_resolution_clock::now();
            decoded = Decode(ID2, ret.r1, ret.r0);
            // endDecode = std::chrono::high_resolution_clock::now();

            if (decoded == true) {
                bool ok = GetDecoded(ID2, samplesOut, samplesPerMessage);

                for (int s = 0; s < samplesPerMessage; s++) {
                    for (int i = 0; i < int32Count; i++) {
                        // sample_out = GetDecodedIndex(ID2, s, i);
                        // if (sample_out.r0 == 0 || sample_out.r1 != samples[s].T || sample_out.r2 != samples[s].Int32s[i] || sample_out.r3 != samples[s].Q[i]) {
                        //     printf("error: decode mismatch: %d, %d\n", s, i);
                        // }

                        if (samplesOut[s].T != samples[s].T || samplesOut[s].Int32s[i] != samples[s].Int32s[i] || samplesOut[s].Q[i] != samples[s].Q[i]) {
                            printf("error: decode mismatch: %d, %d (%d, %d)\n", s, i, samplesOut[s].Int32s[i], samples[s].Int32s[i]);
                        }
                    }
                }
            }
            // endProcessedDecodeOutput = std::chrono::high_resolution_clock::now();
            
            // need to free byte arrays allocated
            free(ret.r1);
            break;
        }
    }
    
    // overall results
    printf("samples encoded: %d, length: %d bytes\n", encodedSamples + 1, encodedLength);
    double efficiency = 100.0 * encodedLength / (int32Count * 8 * samplesPerMessage);
    printf("compression efficiency: %.2f%% of original size\n", efficiency);
    if (decoded == true) {
        printf("decoding successful\n");
    }
    printf("\n");

    // calculate timings
    std::chrono::duration<float> totalDuration = endAll - start;
    std::chrono::duration<float> encodeDuration = endEncode - start;
    std::chrono::duration<float> decodeDuration = endDecode - startDecode;
    std::chrono::duration<float> decodeWithProcessingDuration = endProcessedDecodeOutput - startDecode;
    printf("total duration:\t\t%.2f ms\n", totalDuration.count() * 1000);
    printf("encode:\t\t\t%.2f ms\n", encodeDuration.count() * 1000);
    printf("decode:\t\t\t%.2f ms\n", decodeDuration.count() * 1000);
    printf("decode with processing:\t%.2f ms\n", decodeWithProcessingDuration.count() * 1000);

    // free allocated memory
    freeSamples(samples,  samplesPerMessage);
    freeSamples(samplesOut,  samplesPerMessage);
}

