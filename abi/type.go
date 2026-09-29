package abi

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

var (
	bigIntT    = reflect.TypeFor[big.Int]()
	bigIntPtrT = reflect.TypeFor[*big.Int]()
	uint256T   = reflect.TypeFor[uint256.Int]()
	uint8T     = reflect.TypeFor[uint8]()
	hashT      = reflect.TypeFor[common.Hash]()

	// hashType decodes a topic word of an indexed dynamic value (its keccak256 hash).
	hashType = gethabi.Type{T: gethabi.HashTy, Size: 32}
)

// Error texts shared with geth's accounts/abi.
var (
	errBadBool     = errors.New("abi: improperly encoded boolean value")
	errInvalidSign = errors.New("abi: negatively-signed value cannot be packed into uint parameter")
	errEmptyData   = errors.New("abi: attempting to unmarshal an empty string while arguments are expected")
)

// Errors returned by UnpackLog, same texts as geth's bind/v2.
var (
	ErrNoEventSignature       = errors.New("no event signature")
	ErrEventSignatureMismatch = errors.New("event signature mismatch")
)

// isDynamic mirrors geth's isDynamicType.
func isDynamic(t *gethabi.Type) bool {
	switch t.T {
	case gethabi.StringTy, gethabi.BytesTy, gethabi.SliceTy:
		return true
	case gethabi.ArrayTy:
		return isDynamic(t.Elem)
	case gethabi.TupleTy:
		for _, e := range t.TupleElems {
			if isDynamic(e) {
				return true
			}
		}
	}
	return false
}

// typeSize mirrors geth's getTypeSize: the head size of t in bytes.
func typeSize(t *gethabi.Type) int {
	switch t.T {
	case gethabi.ArrayTy:
		if !isDynamic(t.Elem) {
			if t.Elem.T == gethabi.ArrayTy || t.Elem.T == gethabi.TupleTy {
				return t.Size * typeSize(t.Elem)
			}
			return t.Size * 32
		}
	case gethabi.TupleTy:
		if !isDynamic(t) {
			total := 0
			for _, e := range t.TupleElems {
				total += typeSize(e)
			}
			return total
		}
	}
	return 32
}

// naturalType is the Go type geth's Unpack produces for t.
func naturalType(t *gethabi.Type) reflect.Type {
	if t.T == gethabi.HashTy {
		return hashT
	}
	return t.GetType()
}

// intBits caps the ABI integer width used for range checks at 256.
func intBits(t *gethabi.Type) int {
	return min(t.Size, 256)
}

func errUnmarshal(t *gethabi.Type, dst reflect.Type) error {
	return fmt.Errorf("abi: cannot unmarshal %v in to %v", naturalType(t), dst)
}

func errPack(t *gethabi.Type, v reflect.Value) error {
	if !v.IsValid() {
		return fmt.Errorf("abi: cannot use nil as type %v as argument", t)
	}
	return fmt.Errorf("abi: cannot use %v as type %v as argument", v.Type(), t)
}
