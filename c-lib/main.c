#include <stdio.h>
#include <stdlib.h>
#include "c-main.h"

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
    const int sampling_rate = 4000;
    const int samplesPerMessage = 2;

    // TODO add loops

    // create encoders
    NewEncoder(ID, int32Count, sampling_rate, samplesPerMessage);
    NewEncoder(ID2, int32Count, sampling_rate, samplesPerMessage);

    // create a single data sample
    struct DatasetWithQuality sample;
    sample.T = 0;
    sample.Int32s = (int*) malloc(int32Count);
    sample.Q = (int*) malloc(int32Count);
    // set values
    sample.Int32s[0] = 500;

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
    struct EncodeFlat_return ret3 = EncodeFlat(ID2, 0, Int32s, Q);
    printf("encoded length: %d bytes\n", ret3.r0);
    struct EncodeFlat_return ret4 = EncodeFlat(ID2, 0, Int32s, Q);
    printf("encoded length: %d bytes\n", ret4.r0);

    // need to free byte arrays allocated
    // free(ret.r1);
    // free(ret2.r1);
    free(ret3.r1);
    free(ret4.r1);
   
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
