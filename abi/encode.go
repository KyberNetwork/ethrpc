package abi

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"reflect"
	"unsafe"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// packArgs appends the ABI encoding of vals to buf in a single buffer: heads are
// reserved zeroed up front, static values are written in place and dynamic tails
// are appended.
func packArgs(args gethabi.Arguments, vals []any, buf []byte) ([]byte, error) {
	if len(vals) != len(args) {
		return nil, fmt.Errorf("argument count mismatch: got %d for %d", len(vals), len(args))
	}
	head := 0
	for i := range args {
		head += typeSize(&args[i].Type)
	}
	start := len(buf)
	buf = append(buf, make([]byte, head)...)
	pos := start
	for i := range args {
		t := &args[i].Type
		v, err := indirect(t, reflect.ValueOf(vals[i]))
		if err != nil {
			return nil, err
		}
		if buf, pos, err = packHead(t, v, buf, start, pos); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// packHead encodes one head slot at buf[pos:] of a block starting at start and
// returns the next slot position.
func packHead(t *gethabi.Type, v reflect.Value, buf []byte, start, pos int) ([]byte, int, error) {
	if isDynamic(t) {
		putUint64(buf[pos:pos+32], uint64(len(buf)-start))
		buf, err := packDynamic(t, v, buf)
		return buf, pos + 32, err
	}
	size := typeSize(t)
	return buf, pos + size, packStatic(t, v, buf[pos:pos+size])
}

// indirect is geth's indirect (pointers except *big.Int) that also unwraps
// interfaces and rejects nil pointers instead of panicking.
func indirect(t *gethabi.Type, v reflect.Value) (reflect.Value, error) {
	for {
		switch v.Kind() {
		case reflect.Interface:
			v = v.Elem()
		case reflect.Pointer:
			if v.IsNil() {
				return v, fmt.Errorf("abi: cannot use nil %v as type %v as argument", v.Type(), t)
			}
			if v.Type() == bigIntPtrT {
				return v, nil
			}
			v = v.Elem()
		case reflect.Invalid:
			return v, fmt.Errorf("abi: cannot use nil as type %v as argument", t)
		default:
			return v, nil
		}
	}
}

func packDynamic(t *gethabi.Type, v reflect.Value, buf []byte) ([]byte, error) {
	switch t.T {
	case gethabi.StringTy:
		if v.Kind() != reflect.String {
			return nil, errPack(t, v)
		}
		return appendBytes(buf, v.String()), nil
	case gethabi.BytesTy:
		if v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 {
			return nil, errPack(t, v)
		}
		return appendBytes(buf, v.Bytes()), nil
	case gethabi.SliceTy, gethabi.ArrayTy:
		if k := v.Kind(); k != reflect.Slice && k != reflect.Array || t.T == gethabi.ArrayTy && v.Len() != t.Size {
			return nil, errPack(t, v)
		}
		if t.T == gethabi.SliceTy {
			buf = appendUint64(buf, uint64(v.Len()))
		}
		return packSeq(t.Elem, v, buf)
	case gethabi.TupleTy:
		return packTuple(t, v, buf)
	default:
		return nil, fmt.Errorf("abi: unknown dynamic type %v", t)
	}
}

// packSeq appends the elements of v as a head/tail block.
func packSeq(elem *gethabi.Type, v reflect.Value, buf []byte) ([]byte, error) {
	n := v.Len()
	size := typeSize(elem)
	start := len(buf)
	buf = append(buf, make([]byte, n*size)...)
	pos := start
	for j := 0; j < n; j++ {
		e, err := indirect(elem, v.Index(j))
		if err != nil {
			return nil, err
		}
		if buf, pos, err = packHead(elem, e, buf, start, pos); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// packTuple appends a tuple as a head/tail block; fields are matched by name.
func packTuple(t *gethabi.Type, v reflect.Value, buf []byte) ([]byte, error) {
	plan, err := tuplePlan(t, v)
	if err != nil {
		return nil, err
	}
	head := 0
	for _, e := range t.TupleElems {
		head += typeSize(e)
	}
	start := len(buf)
	buf = append(buf, make([]byte, head)...)
	pos := start
	for i, e := range t.TupleElems {
		f, err := tupleField(t, plan, v, i)
		if err != nil {
			return nil, err
		}
		if buf, pos, err = packHead(e, f, buf, start, pos); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

func tuplePlan(t *gethabi.Type, v reflect.Value) (*fieldPlan, error) {
	if v.Kind() != reflect.Struct {
		return nil, errPack(t, v)
	}
	return structFields(v.Type(), modeArgs, len(t.TupleRawNames), func(i int) string { return t.TupleRawNames[i] })
}

func tupleField(t *gethabi.Type, plan *fieldPlan, v reflect.Value, i int) (reflect.Value, error) {
	f, err := plan.field(v, i)
	if err != nil {
		return f, err
	}
	if !f.IsValid() {
		return f, fmt.Errorf("field %s for tuple not found in the given struct", t.TupleRawNames[i])
	}
	return indirect(t.TupleElems[i], f)
}

// packStatic writes a static value into dst, which is zeroed and typeSize(t) long.
func packStatic(t *gethabi.Type, v reflect.Value, dst []byte) error {
	switch t.T {
	case gethabi.IntTy, gethabi.UintTy:
		return packInt(t, v, dst)
	case gethabi.BoolTy:
		if v.Kind() != reflect.Bool {
			return errPack(t, v)
		}
		if v.Bool() {
			dst[31] = 1
		}
		return nil
	case gethabi.AddressTy:
		// geth left-pads any byte array
		if !isByteArray(v) || v.Len() > 32 {
			return errPack(t, v)
		}
		copyByteArray(dst[32-v.Len():], v)
		return nil
	case gethabi.FixedBytesTy, gethabi.FunctionTy:
		if !isByteArray(v) || v.Len() > 32 || t.T == gethabi.FixedBytesTy && v.Len() != t.Size {
			return errPack(t, v)
		}
		copyByteArray(dst, v)
		return nil
	case gethabi.ArrayTy:
		if k := v.Kind(); k != reflect.Slice && k != reflect.Array || v.Len() != t.Size {
			return errPack(t, v)
		}
		size := typeSize(t.Elem)
		for j := 0; j < t.Size; j++ {
			e, err := indirect(t.Elem, v.Index(j))
			if err != nil {
				return err
			}
			if err = packStatic(t.Elem, e, dst[j*size:(j+1)*size]); err != nil {
				return err
			}
		}
		return nil
	case gethabi.TupleTy:
		plan, err := tuplePlan(t, v)
		if err != nil {
			return err
		}
		pos := 0
		for i, e := range t.TupleElems {
			f, err := tupleField(t, plan, v, i)
			if err != nil {
				return err
			}
			size := typeSize(e)
			if err = packStatic(e, f, dst[pos:pos+size]); err != nil {
				return err
			}
			pos += size
		}
		return nil
	default:
		return fmt.Errorf("could not pack element, unknown type: %v", t.T)
	}
}

// packInt range-checks v against intN/uintN and writes its 32-byte two's complement.
func packInt(t *gethabi.Type, v reflect.Value, dst []byte) error {
	n := intBits(t)
	switch k := v.Kind(); {
	case v.Type() == uint256T:
		var u uint256.Int
		if v.CanAddr() {
			u = *(*uint256.Int)(v.Addr().UnsafePointer())
		} else {
			for i := range u {
				u[i] = v.Index(i).Uint()
			}
		}
		if n < 256 && (t.T == gethabi.UintTy && u.BitLen() > n || t.T == gethabi.IntTy && !fitsSigned(&u, n)) {
			return errOverflow(t, u.Dec())
		}
		u.PutUint256(dst)
	case v.Type() == bigIntPtrT:
		b := (*big.Int)(v.UnsafePointer())
		if t.T == gethabi.UintTy && b.Sign() < 0 {
			return errInvalidSign
		}
		if !bigFits(t, b, n) {
			return errOverflow(t, b)
		}
		var u uint256.Int
		u.SetFromBig(b)
		u.PutUint256(dst)
	case k >= reflect.Int && k <= reflect.Int64:
		x := v.Int()
		if t.T == gethabi.UintTy && (x < 0 || n < 64 && x>>uint(n) != 0) ||
			t.T == gethabi.IntTy && n < 64 && x>>uint(n-1) != 0 && x>>uint(n-1) != -1 {
			return errOverflow(t, x)
		}
		if x < 0 {
			for i := range dst[:24] {
				dst[i] = 0xff
			}
		}
		binary.BigEndian.PutUint64(dst[24:], uint64(x))
	case k >= reflect.Uint && k <= reflect.Uint64:
		x := v.Uint()
		if t.T == gethabi.UintTy && n < 64 && x>>uint(n) != 0 || t.T == gethabi.IntTy && n <= 64 && x>>uint(n-1) != 0 {
			return errOverflow(t, x)
		}
		binary.BigEndian.PutUint64(dst[24:], x)
	default:
		return errPack(t, v)
	}
	return nil
}

// bigFits reports whether b is in range of intN/uintN (b >= 0 for uint).
func bigFits(t *gethabi.Type, b *big.Int, n int) bool {
	l := b.BitLen()
	switch {
	case t.T == gethabi.UintTy:
		return l <= n
	case b.Sign() >= 0:
		return l < n
	default: // |b| <= 2^(n-1)
		return l < n || l == n && b.TrailingZeroBits() == uint(n-1)
	}
}

func errOverflow(t *gethabi.Type, v any) error {
	return fmt.Errorf("abi: value %v overflows %v", v, t)
}

func isByteArray(v reflect.Value) bool {
	return v.Kind() == reflect.Array && v.Type().Elem().Kind() == reflect.Uint8
}

// copyByteArray copies byte array v into dst without allocating.
func copyByteArray(dst []byte, v reflect.Value) {
	if v.CanAddr() {
		copy(dst, unsafe.Slice((*byte)(v.Addr().UnsafePointer()), v.Len()))
		return
	}
	if v.CanInterface() {
		// Interface of a non-addressable array reuses its storage.
		switch a := v.Interface().(type) {
		case common.Address:
			copy(dst, a[:])
			return
		case common.Hash:
			copy(dst, a[:])
			return
		case [4]byte:
			copy(dst, a[:])
			return
		}
	}
	for i := range v.Len() {
		dst[i] = byte(v.Index(i).Uint())
	}
}

func putUint64(word []byte, x uint64) {
	binary.BigEndian.PutUint64(word[24:32], x)
}

func appendUint64(buf []byte, x uint64) []byte {
	buf = append(buf, make([]byte, 32)...)
	putUint64(buf[len(buf)-32:], x)
	return buf
}

// appendBytes appends the length-prefixed, right-padded encoding of b.
func appendBytes[T string | []byte](buf []byte, b T) []byte {
	buf = appendUint64(buf, uint64(len(b)))
	buf = append(buf, b...)
	return append(buf, make([]byte, (32-len(b)%32)%32)...)
}
