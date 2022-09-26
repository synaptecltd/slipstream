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

	// go func() {
	// 	time.Sleep(1 * time.Second)
	// }()

	// time.Sleep(2 * time.Second)

	// TODO return pool index/ID rather than C copy of object
	/*ret := &C.struct_Encoder{}
	ret.Int32Count = C.int(enc.Int32Count)

	return ret*/
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
			return numBytes, C.CBytes(buf)
		}
		// for i := 0; i < dec.Int32Count; i++ {
		// 	Int32Slice[i] = dec.Out[s].Int32s[i]
		// 	QSlice[i] = dec.Out[s].Q[i]
		// }
	}

	// need to use CBytes() utility function to copy bytes to C, data must be free'd later
	return 0, nil
}

//export Decode
func Decode(ID []byte, data unsafe.Pointer, length int) bool {
	goUUID, _ := uuid.FromBytes(ID)
	dec := findDecByID(goUUID)
	if dec == nil {
		fmt.Println("not found")
		return false
	}

	// assign data into Go struct
	// goData := &slipstream.DatasetWithQuality{
	// 	T:      T,
	// 	Int32s: Int32s,
	// 	Q:      Q,
	// }

	// encode this data sample
	err := dec.DecodeToBuffer(unsafe.Slice((*byte)(data), length), length)
	if err != nil {
		return false
	}

	// return dec.Out
	return true

	// need to use CBytes() utility function to copy bytes to C, data must be free'd later
	// return numBytes, C.CBytes(buf)
}

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

// GetDecoded maps decoded Slipstream data into a DatasetWithQuality struct allocated in C code
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

//export GetDecodedIndexAll
func GetDecodedIndexAll(ID []byte, sampleIndex int) (ok bool, T uint64, Value []int32, Q []uint32) {
	goUUID, _ := uuid.FromBytes(ID)
	dec := findDecByID(goUUID)
	if dec == nil {
		fmt.Println("not found")
		return false, 0, nil, nil
	}

	if sampleIndex >= len(dec.Out) {
		return false, 0, nil, nil
	}

	// TODO need to convert slice to C array (*uint32_t)

	return true, dec.Out[sampleIndex].T, dec.Out[sampleIndex].Int32s, dec.Out[sampleIndex].Q

	// return (*C.struct_DatasetWithQuality)(unsafe.Pointer(&dec.Out[sampleIndex]))

	// return &C.struct_DatasetWithQuality{
	// 	T:      C.uint64_t(sampleIndex),
	// 	Int32s: (*_Ctype_int32_t)(C.CBytes(dec.Out[sampleIndex].Int32s)),
	// 	// T:      _Ctype_ulonglong(dec.Out[index].T),
	// 	// Int32s: (*_Ctype_int)(&dec.Out[index].Int32s[0]),
	// 	// Q:      (*_Ctype_uint)(&dec.Out[index].Q[0]),
	// }
}

func main() {}
