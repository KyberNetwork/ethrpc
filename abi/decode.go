package abi

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"unsafe"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/holiman/uint256"
)

// decode is geth's toGoType fused with set(): it validates the value of type t at
// output[index:] and writes it straight into dst. An invalid dst only validates.
func decode(t *gethabi.Type, index int, output []byte, dst reflect.Value) error {
	if index+32 > len(output) {
		return fmt.Errorf("abi: cannot marshal in to go type: length insufficient %d require %d", len(output), index+32)
	}
	var begin, length int
	if t.T == gethabi.StringTy || t.T == gethabi.BytesTy || t.T == gethabi.SliceTy {
		var err error
		if begin, length, err = lengthPrefixPointsTo(index, output); err != nil {
			return err
		}
	}

	if dst.IsValid() {
		var err error
		if dst, err = deref(t, dst); err != nil {
			return err
		}
		if dst.Kind() == reflect.Interface {
			return decodeNatural(t, index, output, dst)
		}
	}

	switch t.T {
	case gethabi.TupleTy:
		if isDynamic(t) {
			begin, err := tuplePointsTo(index, output)
			if err != nil {
				return err
			}
			return decodeTuple(t, output[begin:], dst)
		}
		return decodeTuple(t, output[index:], dst)
	case gethabi.SliceTy:
		return decodeSeq(t, output[begin:], length, dst)
	case gethabi.ArrayTy:
		if isDynamic(t.Elem) {
			// geth reads the offset from the low 8 bytes only
			offset := binary.BigEndian.Uint64(output[index+24 : index+32])
			if offset > uint64(len(output)) {
				return fmt.Errorf("abi: toGoType offset greater than output length: offset: %d, len(output): %d", offset, len(output))
			}
			return decodeSeq(t, output[offset:], t.Size, dst)
		}
		return decodeSeq(t, output[index:], t.Size, dst)
	case gethabi.StringTy:
		if !dst.IsValid() {
			return nil
		}
		if dst.Kind() != reflect.String || !dst.CanSet() {
			return errUnmarshal(t, dst.Type())
		}
		dst.SetString(string(output[begin : begin+length]))
		return nil
	case gethabi.BytesTy:
		if !dst.IsValid() {
			return nil
		}
		src := output[begin : begin+length]
		if dst.Kind() == reflect.Slice && dst.Type().Elem() == uint8T && dst.CanSet() {
			dst.SetBytes(src) // aliases output, as geth does
			return nil
		}
		return setByteArray(t, dst, src)
	case gethabi.IntTy, gethabi.UintTy:
		var u uint256.Int
		u.SetBytes32(output[index : index+32])
		if err := checkInt(t, &u); err != nil {
			return err
		}
		if !dst.IsValid() {
			return nil
		}
		return setInt(t, &u, dst)
	case gethabi.BoolTy:
		b, err := readBool(output[index : index+32])
		if err != nil || !dst.IsValid() {
			return err
		}
		if dst.Kind() != reflect.Bool || !dst.CanSet() {
			return errUnmarshal(t, dst.Type())
		}
		dst.SetBool(b)
		return nil
	case gethabi.AddressTy:
		return setByteArray(t, dst, output[index+12:index+32])
	case gethabi.HashTy:
		return setByteArray(t, dst, output[index:index+32])
	case gethabi.FixedBytesTy:
		return setByteArray(t, dst, output[index:index+t.Size])
	case gethabi.FunctionTy:
		word := output[index : index+32]
		if binary.BigEndian.Uint64(word[24:32]) != 0 {
			return fmt.Errorf("abi: got improperly encoded function type, got %v", word)
		}
		return setByteArray(t, dst, word[:24])
	default:
		return fmt.Errorf("abi: unknown type %v", t.T)
	}
}

// deref follows pointers (allocating nil settable ones) and interfaces holding
// pointers, stopping at *big.Int. Mirrors the pointer/interface cases of geth's set().
func deref(t *gethabi.Type, dst reflect.Value) (reflect.Value, error) {
	for {
		switch dst.Kind() {
		case reflect.Interface:
			if e := dst.Elem(); e.Kind() == reflect.Pointer {
				dst = e
				continue
			}
			return dst, nil
		case reflect.Pointer:
			if dst.Type() == bigIntPtrT {
				return dst, nil
			}
			if dst.IsNil() {
				if !dst.CanSet() {
					return dst, errUnmarshal(t, dst.Type())
				}
				dst.Set(reflect.New(dst.Type().Elem()))
			}
			dst = dst.Elem()
		default:
			return dst, nil
		}
	}
}

// decodeNatural fills an interface dst with the value geth's Unpack would produce.
func decodeNatural(t *gethabi.Type, index int, output []byte, dst reflect.Value) error {
	nt := naturalType(t)
	if !dst.CanSet() || !nt.AssignableTo(dst.Type()) {
		return errUnmarshal(t, dst.Type())
	}
	v := reflect.New(nt).Elem()
	if err := decode(t, index, output, v); err != nil {
		return err
	}
	dst.Set(v)
	return nil
}

// decodeTuple is geth's forTupleUnpack; fields are matched by position like setStruct.
func decodeTuple(t *gethabi.Type, output []byte, dst reflect.Value) error {
	if dst.IsValid() && (dst.Kind() != reflect.Struct || dst.Type() == bigIntT || dst.NumField() < len(t.TupleElems)) {
		return errUnmarshal(t, dst.Type())
	}
	virtualArgs := 0
	for i, elem := range t.TupleElems {
		var field reflect.Value
		if dst.IsValid() {
			if field = dst.Field(i); !field.CanSet() {
				return errUnmarshal(elem, field.Type())
			}
		}
		if err := decode(elem, (i+virtualArgs)*32, output, field); err != nil {
			return err
		}
		if (elem.T == gethabi.ArrayTy || elem.T == gethabi.TupleTy) && !isDynamic(elem) {
			virtualArgs += typeSize(elem)/32 - 1
		}
	}
	return nil
}

// decodeSeq is geth's forEachUnpack followed by setSlice/setArray.
func decodeSeq(t *gethabi.Type, output []byte, size int, dst reflect.Value) error {
	if size < 0 {
		return fmt.Errorf("cannot marshal input to array, size is negative (%d)", size)
	}
	if 32*size > len(output) {
		return fmt.Errorf("abi: cannot marshal into go array: offset %d would go over slice boundary (len=%d)", 32*size, len(output))
	}
	elemSize := typeSize(t.Elem)
	n := size // elements written into dst; the rest is only validated
	switch {
	case !dst.IsValid():
		n = 0
	case dst.Kind() == reflect.Slice && t.T == gethabi.SliceTy && dst.CanSet():
		dst.Set(reflect.MakeSlice(dst.Type(), size, size))
	case dst.Kind() == reflect.Array:
		n = min(size, dst.Len())
		for j := n; j < dst.Len(); j++ {
			dst.Index(j).SetZero()
		}
	default:
		return errUnmarshal(t, dst.Type())
	}
	for j := 0; j < size; j++ {
		var elem reflect.Value
		if j < n {
			elem = dst.Index(j)
		}
		if err := decode(t.Elem, j*elemSize, output, elem); err != nil {
			return err
		}
	}
	return nil
}

// checkInt enforces that an ABI intN/uintN word holds an N-bit value.
func checkInt(t *gethabi.Type, u *uint256.Int) error {
	n := intBits(t)
	if n == 256 {
		return nil
	}
	if t.T == gethabi.UintTy {
		if u.BitLen() > n {
			return fmt.Errorf("abi: improperly encoded %v value", t)
		}
	} else if !fitsSigned(u, n) {
		return fmt.Errorf("abi: improperly encoded %v value", t)
	}
	return nil
}

// fitsSigned reports whether the two's complement u is in [-2^(n-1), 2^(n-1)).
func fitsSigned(u *uint256.Int, n int) bool {
	if u.Sign() < 0 {
		var c uint256.Int
		return c.Not(u).BitLen() < n
	}
	return u.BitLen() < n
}

// nativeFits reports whether Go int kind k of the given width holds every value of t.
func nativeFits(t *gethabi.Type, signedDst bool, bits int) bool {
	if t.T == gethabi.IntTy {
		return signedDst && t.Size <= bits
	}
	if signedDst {
		return t.Size < bits
	}
	return t.Size <= bits
}

func setInt(t *gethabi.Type, u *uint256.Int, dst reflect.Value) error {
	if !dst.CanSet() {
		if dst.Type() == bigIntPtrT && !dst.IsNil() {
			toBig(t, u, (*big.Int)(dst.UnsafePointer()))
			return nil
		}
		return errUnmarshal(t, dst.Type())
	}
	switch k := dst.Kind(); {
	case dst.Type() == uint256T:
		*(*uint256.Int)(dst.Addr().UnsafePointer()) = *u
	case dst.Type() == bigIntPtrT:
		b := new(big.Int)
		toBig(t, u, b)
		dst.Set(reflect.ValueOf(b))
	case dst.Type() == bigIntT:
		toBig(t, u, (*big.Int)(dst.Addr().UnsafePointer()))
	case k >= reflect.Int && k <= reflect.Int64:
		if !nativeFits(t, true, dst.Type().Bits()) {
			return errNative(t, dst.Type())
		}
		dst.SetInt(int64(u[0]))
	case k >= reflect.Uint && k <= reflect.Uint64:
		if !nativeFits(t, false, dst.Type().Bits()) {
			return errNative(t, dst.Type())
		}
		dst.SetUint(u[0])
	default:
		return errUnmarshal(t, dst.Type())
	}
	return nil
}

func errNative(t *gethabi.Type, dst reflect.Type) error {
	return fmt.Errorf("abi: cannot unmarshal %v in to %v: Go type cannot hold every %v value", t, dst, t)
}

// toBig sets b to u, read as two's complement for signed ABI types (geth semantics).
func toBig(t *gethabi.Type, u *uint256.Int, b *big.Int) {
	if t.T == gethabi.IntTy && u.Sign() < 0 {
		var neg uint256.Int
		neg.Neg(u)
		neg.IntoBig(&b)
		b.Neg(b)
		return
	}
	u.IntoBig(&b)
}

// setByteArray copies src into a byte-array dst, truncating or zero-filling like
// geth's setArray.
func setByteArray(t *gethabi.Type, dst reflect.Value, src []byte) error {
	if !dst.IsValid() {
		return nil
	}
	if dst.Kind() != reflect.Array || dst.Type().Elem() != uint8T || !dst.CanAddr() || !dst.CanSet() {
		return errUnmarshal(t, dst.Type())
	}
	d := unsafe.Slice((*byte)(dst.Addr().UnsafePointer()), dst.Len())
	clear(d[copy(d, src):])
	return nil
}

// readBool is geth's readBool.
func readBool(word []byte) (bool, error) {
	for _, b := range word[:31] {
		if b != 0 {
			return false, errBadBool
		}
	}
	switch word[31] {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, errBadBool
	}
}

// wordUint64 returns the low 8 bytes of a word and whether the high 24 are zero.
func wordUint64(w []byte) (uint64, bool) {
	return binary.BigEndian.Uint64(w[24:32]),
		binary.BigEndian.Uint64(w[0:8])|binary.BigEndian.Uint64(w[8:16])|binary.BigEndian.Uint64(w[16:24]) == 0
}

// lengthPrefixPointsTo is geth's function of the same name without big.Int on the
// happy path.
func lengthPrefixPointsTo(index int, output []byte) (start int, length int, err error) {
	w := output[index : index+32]
	n := uint64(len(output))
	off, ok := wordUint64(w)
	if !ok || off > n || off+32 > n {
		end := new(big.Int).SetBytes(w)
		end.Add(end, big.NewInt(32))
		return 0, 0, fmt.Errorf("abi: cannot marshal in to go slice: offset %v would go over slice boundary (len=%v)", end, len(output))
	}
	offsetEnd := off + 32
	lw := output[offsetEnd-32 : offsetEnd]
	l, ok := wordUint64(lw)
	if !ok || l > math.MaxInt64-offsetEnd {
		total := new(big.Int).SetBytes(lw)
		total.Add(total, new(big.Int).SetUint64(offsetEnd))
		return 0, 0, fmt.Errorf("abi: length larger than int64: %v", total)
	}
	if total := offsetEnd + l; total > n {
		return 0, 0, fmt.Errorf("abi: cannot marshal in to go type: length insufficient %v require %v", len(output), total)
	}
	return int(offsetEnd), int(l), nil
}

// tuplePointsTo is geth's function of the same name.
func tuplePointsTo(index int, output []byte) (int, error) {
	w := output[index : index+32]
	off, ok := wordUint64(w)
	if !ok || off > uint64(len(output)) {
		return 0, fmt.Errorf("abi: cannot marshal in to go slice: offset %v would go over slice boundary (len=%v)", new(big.Int).SetBytes(w), len(output))
	}
	return int(off), nil
}
