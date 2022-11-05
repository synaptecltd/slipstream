package main

/*
#include <stdint.h>

struct DatasetWithQuality {
    uint64_t	T;
    int32_t 	*Int32s;
    uint32_t	*Q;
};
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/google/uuid"
	"github.com/synaptecltd/slipstream"
	"github.com/zyedidia/generic/list"
)

// type UUID []byte

var encList *list.List[*slipstream.Encoder]
var decList *list.List[*slipstream.Decoder]

func init() {
	encList = list.New[*slipstream.Encoder]()
	decList = list.New[*slipstream.Decoder]()
}

//export NewEncoder
func NewEncoder(ID []byte, int32Count int, samplingRate int, samplesPerMessage int) /**C.struct_Encoder*/ {
	// goUUID, _ := uuid.Parse(string(ID))
	goUUID, _ := uuid.FromBytes(ID)

	enc := slipstream.NewEncoder(goUUID, int32Count, samplingRate, samplesPerMessage)

	encList.PushBack(enc)
}

//export NewDecoder
func NewDecoder(ID []byte, int32Count int, samplingRate int, samplesPerMessage int) {
	goUUID, _ := uuid.FromBytes(ID)

	dec := slipstream.NewDecoder(goUUID, int32Count, samplingRate, samplesPerMessage)

	decList.PushBack(dec)
}

func findEncByID(ID uuid.UUID) *slipstream.Encoder {
	var ret *slipstream.Encoder
	encList.Front.Each(func(e *slipstream.Encoder) {
		// fmt.Println(e.ID)
		if e.ID == ID {
			// fmt.Println("  found:", e.ID)
			ret = e
		}
	})
	return ret
}
func findDecByID(ID uuid.UUID) *slipstream.Decoder {
	var ret *slipstream.Decoder
	decList.Front.Each(func(e *slipstream.Decoder) {
		// fmt.Println(e.ID)
		if e.ID == ID {
			// fmt.Println("  found:", e.ID)
			ret = e
		}
	})
	return ret
}

//export RemoveEncoder
func RemoveEncoder(ID []byte) {
	goUUID, _ := uuid.FromBytes(ID)
	enc := findEncByID(goUUID)
	if enc == nil {
		return
	}

	// TODO

	// encList.Remove(enc)
}

//export RemoveDecoder
func RemoveDecoder(ID []byte) {
	goUUID, _ := uuid.FromBytes(ID)
	dec := findDecByID(goUUID)
	if dec == nil {
		return
	}

	// TODO

	// encList.Remove(enc)
}

// Encode performs encoding of a single sample of data. If this completes a message, the encoded message data is returned.
//
//export Encode
func Encode(ID []byte, T uint64, Int32s []int32, Q []uint32) (length int, data unsafe.Pointer) {
	goUUID, _ := uuid.FromBytes(ID)
	enc := findEncByID(goUUID)
	if enc == nil {
		fmt.Println("not found")
		return 0, nil
	}

	// assign data into Go struct
	goData := &slipstream.DatasetWithQuality{
		T:      T,
		Int32s: Int32s,
		Q:      Q,
	}

	// encode this data sample
	buf, numBytes, err := enc.Encode(goData)
	if err != nil {
		return 0, nil
	}

	// need to use CBytes() utility function to copy bytes to C, data must be free'd later
	return numBytes, C.CBytes(buf)
}

// EncodeAll performs batch encoding of an entire message. The encoded message data is returned.
//
//export EncodeAll
func EncodeAll(ID []byte, data unsafe.Pointer, length int) (lengthOut int, dataOut unsafe.Pointer) {
	goUUID, _ := uuid.FromBytes(ID)
	enc := findEncByID(goUUID)
	if enc == nil {
		fmt.Println("not found")
		return 0, nil
	}

	// convert array of DatasetWithQuality "owned" by C code into Go slice
	datasetSlice := (*[1 << 30]C.struct_DatasetWithQuality)(unsafe.Pointer(data))[:length:length]

	for s := range datasetSlice {
		// similar to above, convert C arrays into Go slices
		Int32Slice := (*[1 << 30]int32)(unsafe.Pointer(datasetSlice[s].Int32s))[:enc.Int32Count:enc.Int32Count]
		QSlice := (*[1 << 30]uint32)(unsafe.Pointer(datasetSlice[s].Q))[:enc.Int32Count:enc.Int32Count]

		// assign data into Go struct
		goData := &slipstream.DatasetWithQuality{
			T:      uint64(datasetSlice[s].T),
			Int32s: Int32Slice,
			Q:      QSlice,
		}

		// encode this data sample
		buf, numBytes, err := enc.Encode(goData)
		if err != nil {
			return 0, nil
		}

		if numBytes > 0 {
			// need to use CBytes() utility function to copy bytes to C, data must be free'd later
			return numBytes, C.CBytes(buf)
		}
	}

	return 0, nil
}

// Decode performs Slipstream decoding from raw byte data. Results are stored in the Go struct, and `GetDecoded()` or `GetDecodedIndex()` should be used to access results from C.
//
//export Decode
func Decode(ID []byte, data unsafe.Pointer, length int) bool {
	goUUID, _ := uuid.FromBytes(ID)
	dec := findDecByID(goUUID)
	if dec == nil {
		fmt.Println("not found")
		return false
	}

	// encode this data sample
	err := dec.DecodeToBuffer(unsafe.Slice((*byte)(data), length), length)
	if err != nil {
		return false
	}

	return true
}

// GetDecodedIndex returns a single data point (with timestamp and quality). This is very inefficient because it needs to be called repeatedly for each encoded variable and time-step.
//
//export GetDecodedIndex
func GetDecodedIndex(ID []byte, sampleIndex int, valueIndex int) (ok bool, T uint64, Value int32, Q uint32) {
	goUUID, _ := uuid.FromBytes(ID)
	dec := findDecByID(goUUID)
	if dec == nil {
		fmt.Println("not found")
		return false, 0, 0, 0
	}

	if sampleIndex >= len(dec.Out) || valueIndex >= len(dec.Out[sampleIndex].Int32s) {
		return false, 0, 0, 0
	}

	return true, dec.Out[sampleIndex].T, dec.Out[sampleIndex].Int32s[valueIndex], dec.Out[sampleIndex].Q[valueIndex]
}

// GetDecoded maps decoded Slipstream data into a slice of `DatasetWithQuality struct`, which is allocated in C code. This provides an efficient way of copying all decoded data from a message from Go to C.
//
//export GetDecoded
func GetDecoded(ID []byte, data unsafe.Pointer, length int) (ok bool) {
	goUUID, _ := uuid.FromBytes(ID)
	dec := findDecByID(goUUID)
	if dec == nil {
		fmt.Println("not found")
		return false
	}

	// convert array of DatasetWithQuality "owned" by C code into Go slice
	datasetSlice := (*[1 << 30]C.struct_DatasetWithQuality)(unsafe.Pointer(data))[:length:length]

	for s := range datasetSlice {
		datasetSlice[s].T = C.uint64_t(dec.Out[s].T)

		// similar to above, convert C arrays into Go slices
		Int32Slice := (*[1 << 30]int32)(unsafe.Pointer(datasetSlice[s].Int32s))[:dec.Int32Count:dec.Int32Count]
		QSlice := (*[1 << 30]uint32)(unsafe.Pointer(datasetSlice[s].Q))[:dec.Int32Count:dec.Int32Count]

		for i := 0; i < dec.Int32Count; i++ {
			Int32Slice[i] = dec.Out[s].Int32s[i]
			QSlice[i] = dec.Out[s].Q[i]
		}
	}

	return true
}

func main() {}
