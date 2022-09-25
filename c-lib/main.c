#include <stdio.h>
#include <stdlib.h>
// #include <time.h>
#include <math.h>
#include <chrono>
#include "c-main.h"

// https://github.com/vladimirvivien/go-cshared-examples
// https://gist.github.com/helinwang/2c7bd2867ea5110f70e6431a7c80cd9b
// https://stackoverflow.com/questions/43646589/does-passing-a-slice-to-golang-from-c-do-a-memory-copy/43646947#43646947

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

    // TODO add loops for params

    // create encoders
    NewEncoder(ID, int32Count, samplingRate, samplesPerMessage);
    NewEncoder(ID2, int32Count, samplingRate, samplesPerMessage);

    auto start = std::chrono::high_resolution_clock::now();

    struct DatasetWithQuality sample;
    sample.T = 0;
    sample.Int32s = (int*) malloc(int32Count);
    sample.Q = (int*) malloc(int32Count);

    // create a single data sample
    for (int s = 0; s < samplesPerMessage; s++) {
        // set values
        for (int i = 0; i < int32Count; i++) {
            sample.Int32s[i] = (int) (1000.0*sin(2*3.14*50.0*((float) s / (float) samplingRate)));
            // printf("%d\n", sample.Int32s[i]);
        }

        // convert to GoSlice
        GoSlice Int32s;
        Int32s.data = (void*) sample.Int32s;
        Int32s.len = int32Count;
        Int32s.cap = int32Count;
        GoSlice Q;
        Q.data = (void*) sample.Q;
        Q.len = int32Count;
        Q.cap = int32Count;

        // attempt encoding
        // struct Encode_return ret = Encode(ID2, &sample);
        // printf("encoded length: %d bytes\n", ret.r0);
        // struct Encode_return ret2 = Encode(ID3, &sample);
        // printf("encoded length: %d bytes\n", ret2.r0);

        // struct timespec start, finish, delta;
        // clock_t begin = clock();
        struct EncodeFlat_return ret3 = EncodeFlat(ID2, 0, Int32s, Q);
        // printf("%d: encoded length: %d bytes\n", s, ret3.r0);
        // struct EncodeFlat_return ret4 = EncodeFlat(ID2, 0, Int32s, Q);
        // printf("encoded length: %d bytes\n", ret4.r0);

        // need to free byte arrays allocated
        // free(ret.r1);
        // free(ret2.r1);
        if (ret3.r0 > 0) {
            free(ret3.r1);
            break;
        }
        // free(ret4.r1);
    }
    // clock_t end = clock();

    auto end = std::chrono::high_resolution_clock::now();
    std::chrono::duration<float> duration = end - start;
    printf("duration: %f s\n", duration.count());

    // printf("Time taken: %lf", (double) (end-begin) / CLOCKS_PER_SEC);

    printf("done\n");
   
    // //Call Add() - passing integer params, integer result
    // GoInt a = 12;
    // GoInt b = 99;
    // printf("awesome.Add(12,99) = %d\n", Add(a, b)); 

    // //Call Cosine() - passing float param, float returned
    // printf("awesome.Cosine(1) = %f\n", (float)(Cosine(1.0)));
    
    // //Call Sort() - passing an array pointer
    // GoInt data[6] = {77, 12, 5, 99, 28, 23};
    // GoSlice nums = {data, 6, 6};
    // Sort(nums);
    // printf("awesome.Sort(77,12,5,99,28,23): ");
    // for (int i = 0; i < 6; i++){
    //     printf("%d,", ((GoInt *)nums.data)[i]);
    // }
    // printf("\n");

    // //Call Log() - passing string value
    // GoString msg = {"Hello from C!", 13};
    // Log(msg);
}

