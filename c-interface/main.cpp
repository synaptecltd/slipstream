#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <math.h>
#include <chrono>
#include <cstring>
#include "c-interface.h"

#define INTEGER_SCALING_I   1000.0
#define INTEGER_SCALING_V   100.0
#define PI                  3.1415926535897932384626433832795
#define TWO_PI_OVER_THREE   2.0943951023931954923084289221863
#define MAG_I               500
#define MAG_V               326598.63   // 400000.0 / sqrt(3) * sqrt(2)
#define FNOM                50.01
#define NOISE_MAX           0.01

/**
 * SlipstreamTest is a container for storing Slipstream encoder/decoder data and monitoring info.
 */
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

/**
 * random is a simple random number generator for adding noise to emulated measurements.
 */
double random(double min, double max) {
    double range = (max - min); 
    double div = RAND_MAX / range;
    return min + (rand() / div);
}

/**
 * getSample generates current or voltage waveform data for testing.
 */
int32_t getSample(double t, bool isVoltage, double phase) {
    double scaling = INTEGER_SCALING_I;
    double mag = MAG_I;
    if (isVoltage) {
        scaling = INTEGER_SCALING_V;
        mag = MAG_V;
    }
    return (int32_t) (scaling * (mag * sin(2*PI*FNOM*t + phase) + random(-NOISE_MAX, NOISE_MAX)));
}

/**
 * allocateSamples allocates memory for data sample storage.
 */
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

/**
 * freeSlipstreamTest frees memory for data sample storage.
 */
void freeSlipstreamTest(SlipstreamTest *test) {
    for (int s = 0; s < test->samplesPerMessage; s++) {
        free(test->samples[s].Int32s);
        free(test->samples[s].Q);
        free(test->samplesOut[s].Int32s);
        free(test->samplesOut[s].Q);
    }
    free(test->samples);
    free(test->samplesOut);

    RemoveEncoder(test->ID);
    RemoveDecoder(test->ID);
}

/**
 * initialiseTestParams sets up a SlipstreamTest container and allocates memory.
 */
void initialiseTestParams(SlipstreamTest *test, GoUint8 ID_bytes[16]) {
    test->int32Count = 8;
    test->samplingRate = 4000;
    test->samplesPerMessage = 4000;

    memcpy(test->ID_bytes, ID_bytes, 16*sizeof(GoUint8));
    test->ID = {test->ID_bytes, 16, 16};

    // pre-calculate all data samples
    test->samples = allocateSamples(test->int32Count, test->samplesPerMessage);
    test->samplesOut = allocateSamples(test->int32Count, test->samplesPerMessage);

    // emulate three-phase current and voltage waveform samples
    for (int s = 0; s < test->samplesPerMessage; s++) {
        double t = (double) s / (double) test->samplingRate;
        test->samples[s].Int32s[0] = getSample(t, false, 0.0);
        test->samples[s].Int32s[1] = getSample(t, false, -TWO_PI_OVER_THREE);
        test->samples[s].Int32s[2] = getSample(t, false, TWO_PI_OVER_THREE);
        test->samples[s].Int32s[3] = test->samples[s].Int32s[0] + test->samples[s].Int32s[1] + test->samples[s].Int32s[2];
        test->samples[s].Int32s[4] = getSample(t, true, 0.0);
        test->samples[s].Int32s[5] = getSample(t, true, -TWO_PI_OVER_THREE);
        test->samples[s].Int32s[6] = getSample(t, true, TWO_PI_OVER_THREE);
        test->samples[s].Int32s[7] = test->samples[s].Int32s[4] + test->samples[s].Int32s[5] + test->samples[s].Int32s[6];

        // set quality values
        for (int i = 0; i < test->int32Count; i++) {
            test->samples[s].Q[i] = 0;
            // printf("%d\n", sample.Int32s[i]);
        }
    }

    // create encoder
    NewEncoder(test->ID, test->int32Count, test->samplingRate, test->samplesPerMessage);

    // create decoder
    NewDecoder(test->ID, test->int32Count, test->samplingRate, test->samplesPerMessage);
}

/**
 * validateData checks the the every sample of the decoded output matches the original data.
 */
void validateData(SlipstreamTest *test) {
    for (int s = 0; s < test->samplesPerMessage; s++) {
        for (int i = 0; i < test->int32Count; i++) {
            if (test->samplesOut[s].T != test->samples[s].T || test->samplesOut[s].Int32s[i] != test->samples[s].Int32s[i] || test->samplesOut[s].Q[i] != test->samples[s].Q[i]) {
                printf("error: decode mismatch: %d, %d (%d, %d)\n", s, i, test->samplesOut[s].Int32s[i], test->samples[s].Int32s[i]);
            }
        }
    }
}

/**
 * printResults outputs test results.
 */
void printResults(SlipstreamTest *test) {
    // overall results
    printf("samples encoded: %d, length: %d bytes\n", test->encodedSamples, test->encodedLength);
    int bytesPerSample = 8 + 4 + 4; // timestamp, value, quality
    double efficiency = 100.0 * test->encodedLength / (test->int32Count * bytesPerSample * test->samplesPerMessage);
    printf("compression efficiency: %.2f%% of original size\n", efficiency);
    if (test->decoded == true) {
        printf("decoding successful\n");
    } else {
        printf("decoding not successful\n");
    }
    printf("\n");

    // calculate timings
    std::chrono::duration<float> totalDuration = test->endAll - test->start;
    std::chrono::duration<float> encodeDuration = test->endEncode - test->start;
    std::chrono::duration<float> decodeDuration = test->endDecode - test->startDecode;
    std::chrono::duration<float> decodeWithProcessingDuration = test->endProcessedDecodeOutput - test->startDecode;
    printf("total duration:\t\t%.2f ms\n", totalDuration.count() * 1000);
    printf("encode:\t\t\t%.2f ms\n", encodeDuration.count() * 1000);
    printf("decode:\t\t\t%.2f ms\n", decodeDuration.count() * 1000);
    printf("decode with processing:\t%.2f ms\n", decodeWithProcessingDuration.count() * 1000);
}

int main() {
    printf("using Go lib from C/C++\n");

    // seed random number for measurement noise
    srand(0);

    printf("\n*** 1. perform encoding of all samples ***\n\n");
    SlipstreamTest batchEncode = {0};
    GoUint8 ID_bytes[16] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0};
    initialiseTestParams(&batchEncode, ID_bytes);

    // perform encoding of all samples
    batchEncode.start = std::chrono::high_resolution_clock::now();
    struct EncodeAll_return retAll = EncodeAll(batchEncode.ID, batchEncode.samples, batchEncode.samplesPerMessage);
    batchEncode.encodedSamples = batchEncode.samplesPerMessage;
    batchEncode.encodedLength = retAll.r0;
    batchEncode.endEncode = std::chrono::high_resolution_clock::now();

    // check if encoded data is available, then attempt decoding of data bytes
    if (retAll.r0 > 0) {
        batchEncode.startDecode = std::chrono::high_resolution_clock::now();
        batchEncode.decoded = Decode(batchEncode.ID, retAll.r1, retAll.r0);

        // need to free byte arrays allocated
        free(retAll.r1);

        batchEncode.endDecode = std::chrono::high_resolution_clock::now();
    }

    if (batchEncode.decoded == true) {
        bool ok = GetDecoded(batchEncode.ID, batchEncode.samplesOut, batchEncode.samplesPerMessage);

        validateData(&batchEncode);
    }
    batchEncode.endProcessedDecodeOutput = std::chrono::high_resolution_clock::now();
    batchEncode.endAll = std::chrono::high_resolution_clock::now();

    printResults(&batchEncode);
    
    // free allocated memory
    freeSlipstreamTest(&batchEncode);

    printf("\n*** 2. perform iterative encoding of samples ***\n\n");
    SlipstreamTest iterativeEncode = {0};
    GoUint8 ID2_bytes[16] = {2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 5};
    initialiseTestParams(&iterativeEncode, ID2_bytes);

    iterativeEncode.start = std::chrono::high_resolution_clock::now();

    // performing encoding sample by sample. decoding is attempted once a full message is created.
    for (int s = 0; s < iterativeEncode.samplesPerMessage; s++) {
        // convert a single data sample to GoSlice
        GoSlice Int32s;
        Int32s.data = (void*) iterativeEncode.samples[s].Int32s;
        Int32s.len = iterativeEncode.int32Count;
        Int32s.cap = iterativeEncode.int32Count;
        GoSlice Q;
        Q.data = (void*) iterativeEncode.samples[s].Q;
        Q.len = iterativeEncode.int32Count;
        Q.cap = iterativeEncode.int32Count;

        // attempt encoding
        struct Encode_return ret = Encode(iterativeEncode.ID, 0, Int32s, Q);

        // check for completed message
        if (ret.r0 > 0) {
            iterativeEncode.endEncode = std::chrono::high_resolution_clock::now();

            iterativeEncode.encodedSamples = s + 1;
            iterativeEncode.encodedLength = ret.r0;

            iterativeEncode.startDecode = std::chrono::high_resolution_clock::now();
            iterativeEncode.decoded = Decode(iterativeEncode.ID, ret.r1, ret.r0);
            iterativeEncode.endDecode = std::chrono::high_resolution_clock::now();

            if (iterativeEncode.decoded == true) {
                bool ok = GetDecoded(iterativeEncode.ID, iterativeEncode.samplesOut, iterativeEncode.samplesPerMessage);

                validateData(&iterativeEncode);
            }
            
            // need to free byte arrays allocated
            free(ret.r1);
            break;
        }
    }
    iterativeEncode.endProcessedDecodeOutput = std::chrono::high_resolution_clock::now();
    iterativeEncode.endAll = std::chrono::high_resolution_clock::now();

    printResults(&iterativeEncode);
    
    // free allocated memory
    freeSlipstreamTest(&iterativeEncode);
}

