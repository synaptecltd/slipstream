/* Code generated by cmd/cgo; DO NOT EDIT. */

/* package command-line-arguments */


#line 1 "cgo-builtin-export-prolog"

#include <stddef.h>

#ifndef GO_CGO_EXPORT_PROLOGUE_H
#define GO_CGO_EXPORT_PROLOGUE_H

#ifndef GO_CGO_GOSTRING_TYPEDEF
typedef struct { const char *p; ptrdiff_t n; } _GoString_;
#endif

#endif

/* Start of preamble from import "C" comments.  */


#line 3 "c-main.go"

#include <stdint.h>

struct DatasetWithQuality {
    uint64_t	T;
    int32_t 	*Int32s;
    uint32_t	*Q;
};

#line 1 "cgo-generated-wrapper"


/* End of preamble from import "C" comments.  */


/* Start of boilerplate cgo prologue.  */
#line 1 "cgo-gcc-export-header-prolog"

#ifndef GO_CGO_PROLOGUE_H
#define GO_CGO_PROLOGUE_H

typedef signed char GoInt8;
typedef unsigned char GoUint8;
typedef short GoInt16;
typedef unsigned short GoUint16;
typedef int GoInt32;
typedef unsigned int GoUint32;
typedef long long GoInt64;
typedef unsigned long long GoUint64;
typedef GoInt64 GoInt;
typedef GoUint64 GoUint;
typedef size_t GoUintptr;
typedef float GoFloat32;
typedef double GoFloat64;
#ifdef _MSC_VER
#include <complex.h>
typedef _Fcomplex GoComplex64;
typedef _Dcomplex GoComplex128;
#else
typedef float _Complex GoComplex64;
typedef double _Complex GoComplex128;
#endif

/*
  static assertion to make sure the file is being used on architecture
  at least with matching size of GoInt.
*/
typedef char _check_for_64_bit_pointer_matching_GoInt[sizeof(void*)==64/8 ? 1:-1];

#ifndef GO_CGO_GOSTRING_TYPEDEF
typedef _GoString_ GoString;
#endif
typedef void *GoMap;
typedef void *GoChan;
typedef struct { void *t; void *v; } GoInterface;
typedef struct { void *data; GoInt len; GoInt cap; } GoSlice;

#endif

/* End of boilerplate cgo prologue.  */

#ifdef __cplusplus
extern "C" {
#endif

extern __declspec(dllexport) void NewEncoder(GoSlice ID, GoInt int32Count, GoInt samplingRate, GoInt samplesPerMessage);
extern __declspec(dllexport) void NewDecoder(GoSlice ID, GoInt int32Count, GoInt samplingRate, GoInt samplesPerMessage);
extern __declspec(dllexport) void RemoveEncoder(GoSlice ID);

/* Return type for Encode */
struct Encode_return {
	GoInt r0; /* length */
	void* r1; /* data */
};
extern __declspec(dllexport) struct Encode_return Encode(GoSlice ID, GoUint64 T, GoSlice Int32s, GoSlice Q);
extern __declspec(dllexport) GoUint8 Decode(GoSlice ID, void* data, GoInt length);

/* Return type for GetDecodedIndex */
struct GetDecodedIndex_return {
	GoUint8 r0; /* ok */
	GoUint64 r1; /* T */
	GoInt32 r2; /* Value */
	GoUint32 r3; /* Q */
};
extern __declspec(dllexport) struct GetDecodedIndex_return GetDecodedIndex(GoSlice ID, GoInt sampleIndex, GoInt valueIndex);

// GetDecoded maps decoded Slipstream data into a DatasetWithQuality struct allocated in C code
//
extern __declspec(dllexport) GoUint8 GetDecoded(GoSlice ID, void* data, GoInt length);

/* Return type for GetDecodedIndexAll */
struct GetDecodedIndexAll_return {
	GoUint8 r0; /* ok */
	GoUint64 r1; /* T */
	GoSlice r2; /* Value */
	GoSlice r3; /* Q */
};
extern __declspec(dllexport) struct GetDecodedIndexAll_return GetDecodedIndexAll(GoSlice ID, GoInt sampleIndex);

#ifdef __cplusplus
}
#endif