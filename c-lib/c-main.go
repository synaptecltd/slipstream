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
	buf, numBytes, _ := enc.Encode(goData)

	// need to use CBytes() utility function to copy bytes to C, data must be free'd later
	return numBytes, C.CBytes(buf)
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
