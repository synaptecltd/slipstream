#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include <chrono>
// #include <time.h>
#include "c-main.h"

// note that C++ is used only for accurate timing using std::chrono

// useful references:
// https://github.com/vladimirvivien/go-cshared-examples
// https://gist.github.com/helinwang/2c7bd2867ea5110f70e6431a7c80cd9b
// https://stackoverflow.com/questions/43646589/does-passing-a-slice-to-golang-from-c-do-a-memory-copy/43646947#43646947

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

int main() {
    printf("using Go lib from C/C++\n");

    srand(0);

    // initialise some UUIDs as Go slides of 16 bytes
    GoUint8 ID_bytes[16] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};
    GoSlice ID = {ID_bytes, 16, 16};
    GoUint8 ID2_bytes[16] = {2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 5};
    GoSlice ID2 = {ID2_bytes, 16, 16};
    GoUint8 ID3_bytes[16] = {3, 1, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};
    GoSlice ID3 = {ID3_bytes, 16, 16};

    // encoder/decoder settings
    const int int32Count = 8;
    const int samplingRate = 4000;
    const int samplesPerMessage = 4000;

    // pre-calculate all data samples
    struct DatasetWithQuality *samples;
    samples = (struct DatasetWithQuality*) malloc(samplesPerMessage * sizeof(struct DatasetWithQuality));
    for (int s = 0; s < samplesPerMessage; s++) {
        samples[s].T = s;
        samples[s].Int32s = (int32_t*) malloc(int32Count * sizeof(int32_t));
        samples[s].Q = (uint32_t*) malloc(int32Count * sizeof(uint32_t));

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
    struct DatasetWithQuality *samplesOut;
    samplesOut = (struct DatasetWithQuality*) malloc(samplesPerMessage * sizeof(struct DatasetWithQuality));
    for (int s = 0; s < samplesPerMessage; s++) {
        samplesOut[s].T = s;
        samplesOut[s].Int32s = (int32_t*) malloc(int32Count * sizeof(int32_t));
        samplesOut[s].Q = (uint32_t*) malloc(int32Count * sizeof(uint32_t));
    }

    // create encoders
    NewEncoder(ID, int32Count, samplingRate, samplesPerMessage);
    NewEncoder(ID2, int32Count, samplingRate, samplesPerMessage);

    // create decoders
    NewDecoder(ID2, int32Count, samplingRate, samplesPerMessage);

    // define timers
    std::chrono::high_resolution_clock::time_point start, endEncode, endAll, startDecode, endDecode, endProcessedDecodeOutput;
    start = std::chrono::high_resolution_clock::now();

    int encodedSamples, encodedLength = 0;
    bool decoded = false;

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
            endEncode = std::chrono::high_resolution_clock::now();

            encodedSamples = s;
            encodedLength = ret.r0;

            startDecode = std::chrono::high_resolution_clock::now();
            decoded = Decode(ID2, ret.r1, ret.r0);
            endDecode = std::chrono::high_resolution_clock::now();

            if (decoded == true) {
                struct GetDecodedIndex_return sample_out;

                // struct GetDecodedIndexAll_return sample_out_all;

                bool ok = GetDecoded(ID2, samplesOut, samplesPerMessage);
                printf("decoder: %d\n", ok);

                for (int s = 0; s < samplesPerMessage; s++) {
                    for (int i = 0; i < int32Count; i++) {
                        // sample_out = GetDecodedIndex(ID2, s, i);
                        // if (sample_out.r0 == 0 || sample_out.r1 != samples[s].T || sample_out.r2 != samples[s].Int32s[i] || sample_out.r3 != samples[s].Q[i]) {
                        //     printf("error: decode mismatch: %d, %d\n", s, i);
                        // }
                        // printf("%d\n", sample_out.r2);

                        if (samplesOut[s].Int32s[i] != samples[s].Int32s[i]) {
                            printf("error: decode mismatch: %d, %d (%d, %d)\n", s, i, samplesOut[s].Int32s[i], samples[s].Int32s[i]);
                        }
                    }
                }
            }
            endProcessedDecodeOutput = std::chrono::high_resolution_clock::now();
            
            // need to free byte arrays allocated
            free(ret.r1);
            break;
        }
    }

    endAll = std::chrono::high_resolution_clock::now();
    
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
    printf("total duration:\t\t%.1f ms\n", totalDuration.count() * 1000);
    printf("encode:\t\t\t%.1f ms\n", encodeDuration.count() * 1000);
    printf("decode:\t\t\t%.1f ms\n", decodeDuration.count() * 1000);
    printf("decode with processing:\t%.1f ms\n", decodeWithProcessingDuration.count() * 1000);
}

