package main

/*
struct Encoder {
    int Int32Count;
    int Y;
};

struct DatasetWithQuality {
    int T;
    int *Int32s;
    int *Q;
};
*/
import "C"

import (
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
func Encode(ID []byte, data *C.struct_DatasetWithQuality) (int, unsafe.Pointer) {
	goUUID, _ := uuid.FromBytes(ID)
	enc := findEncByID(goUUID)
	if enc == nil {
		return 0, nil
	}

	// TODO need to copy data into Go struct?
	goData := &slipstream.DatasetWithQuality{
		T: uint64(data.T),
		// Int32s: C.CSlice(data.Int32s),
	}
	buf, numBytes, _ := enc.Encode(goData)

	// need to use utility function to copy bytes to C
	return numBytes, C.CBytes(buf)
}

//export EncodeFlat
func EncodeFlat(ID []byte, T uint64, Int32s []int32, Q []uint32) (int, unsafe.Pointer) {
	goUUID, _ := uuid.FromBytes(ID)
	enc := findEncByID(goUUID)
	if enc == nil {
		return 0, nil
	}

	// TODO need to copy data into Go struct?
	goData := &slipstream.DatasetWithQuality{
		T:      T,
		Int32s: Int32s,
		Q:      Q,
	}
	buf, numBytes, _ := enc.Encode(goData)

	// need to use utility function to copy bytes to C
	return numBytes, C.CBytes(buf)
}

func main() {}
