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

#define INTEGER_SCALING 1000.0
#define PI              3.1415926535897932384626433832795
#define FNOM            50.0

int main() {
    printf("Using lib from C\n");

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

        for (int i = 0; i < int32Count; i++) {
            double t = (double) s / (double) samplingRate;
            samples[s].Int32s[i] = (int32_t) (INTEGER_SCALING * 10000.0 * sin(2*PI*FNOM*t));
            samples[s].Q[i] = 0;
            // printf("%d\n", sample.Int32s[i]);
        }
    }

    // create encoders
    NewEncoder(ID, int32Count, samplingRate, samplesPerMessage);
    NewEncoder(ID2, int32Count, samplingRate, samplesPerMessage);

    // create decoders
    NewDecoder(ID2, int32Count, samplingRate, samplesPerMessage);

    auto start = std::chrono::high_resolution_clock::now();

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
            printf("%d: encoded length: %d bytes\n", s, ret.r0);

            GoUint8 decoded = Decode(ID2, ret.r1, ret.r0);
            if (decoded == 1) {
                printf("decoded\n");

                struct GetDecodedIndex_return sample_out;
                for (int s = 0; s < samplesPerMessage; s++) {
                    for (int i = 0; i < int32Count; i++) {
                        sample_out = GetDecodedIndex(ID2, s, i);
                        if (sample_out.r0 == 0 || sample_out.r1 != samples[s].T || sample_out.r2 != samples[s].Int32s[i] || sample_out.r3 != samples[s].Q[i]) {
                            printf("decode mismatch\n");
                        }
                    }
                }

            }
            
            // need to free byte arrays allocated
            free(ret.r1);
            break;
        }
    }

    auto end = std::chrono::high_resolution_clock::now();
    std::chrono::duration<float> duration = end - start;
    printf("duration: %f s\n", duration.count());
}

