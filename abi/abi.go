// Package abi is an ABI codec over go-ethereum's parsed abi.ABI that encodes and
// decodes uint256.Int and range-safe native Go ints directly, without big.Int
// round trips. Decoding is type-directed into the destination (no []any + Copy).
package abi

import (
	"fmt"
	"reflect"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Pack is geth's ABI.Pack: the method ID followed by the encoded args; an empty
// name packs constructor arguments.
func Pack(a *gethabi.ABI, name string, args ...any) ([]byte, error) {
	if name == "" {
		return packArgs(a.Constructor.Inputs, args, nil)
	}
	method, ok := a.Methods[name]
	if !ok {
		return nil, fmt.Errorf("method '%s' not found", name)
	}
	buf := make([]byte, 4, 4+32*len(method.Inputs))
	copy(buf, method.ID)
	return packArgs(method.Inputs, args, buf)
}

// PackArgs is geth's Arguments.Pack.
func PackArgs(args gethabi.Arguments, vals ...any) ([]byte, error) {
	return packArgs(args, vals, nil)
}

// UnpackIntoInterface is geth's ABI.UnpackIntoInterface for a method, event or
// error named name.
func UnpackIntoInterface(a *gethabi.ABI, v any, name string, data []byte) error {
	args, err := arguments(a, name, data)
	if err != nil {
		return err
	}
	return UnpackArgs(args, v, data)
}

// UnpackArgs is geth's Arguments.Unpack followed by Arguments.Copy into v.
func UnpackArgs(args gethabi.Arguments, v any, data []byte) error {
	count := 0
	for i := range args {
		if !args[i].Indexed {
			count++
		}
	}
	if len(data) == 0 && count != 0 {
		return errEmptyData
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer {
		return fmt.Errorf("abi: Unpack(non-pointer %T)", v)
	}
	if count == 0 {
		return nil
	}
	if rv.IsNil() {
		return fmt.Errorf("abi: Unpack(nil %T)", v)
	}
	dst := rv.Elem()

	if len(args) == 1 { // geth's copyAtomic: a struct receives the value in field 0
		t := &args[0].Type
		if dst.Kind() == reflect.Struct && dst.Type() != bigIntT {
			if dst.NumField() == 0 || !dst.Field(0).CanSet() {
				return errUnmarshal(t, dst.Type())
			}
			dst = dst.Field(0)
		}
		return decode(t, 0, data, dst)
	}

	// geth's copyTuple
	var plan *fieldPlan
	switch dst.Kind() {
	case reflect.Struct:
		var err error
		if plan, err = argsPlan(args, count, dst.Type()); err != nil {
			return err
		}
	case reflect.Slice, reflect.Array:
		if dst.Len() < count {
			return fmt.Errorf("abi: insufficient number of arguments for unpack, want %d, got %d", count, dst.Len())
		}
	default:
		return fmt.Errorf("abi:[2] cannot unmarshal tuple in to %v", dst.Type())
	}
	virtualArgs, index := 0, 0
	for i := range args {
		arg := &args[i]
		if arg.Indexed {
			continue
		}
		var field reflect.Value
		if plan != nil {
			f, err := plan.field(dst, index)
			if err != nil {
				return err
			}
			if !f.IsValid() {
				return fmt.Errorf("abi: field %s can't be found in the given value", arg.Name)
			}
			if !f.CanSet() {
				return errUnmarshal(&arg.Type, f.Type())
			}
			field = f
		} else {
			field = dst.Index(index)
		}
		if err := decode(&arg.Type, (index+virtualArgs)*32, data, field); err != nil {
			return err
		}
		if (arg.Type.T == gethabi.ArrayTy || arg.Type.T == gethabi.TupleTy) && !isDynamic(&arg.Type) {
			virtualArgs += typeSize(&arg.Type)/32 - 1
		}
		index++
	}
	return nil
}

// argsPlan maps the non-indexed argument names onto struct type rt.
func argsPlan(args gethabi.Arguments, count int, rt reflect.Type) (*fieldPlan, error) {
	if count == len(args) {
		return structFields(rt, modeArgs, count, func(i int) string { return args[i].Name })
	}
	names := make([]string, 0, count)
	for i := range args {
		if !args[i].Indexed {
			names = append(names, args[i].Name)
		}
	}
	return structFields(rt, modeArgs, count, func(i int) string { return names[i] })
}

// arguments is geth's ABI.getArguments.
func arguments(a *gethabi.ABI, name string, data []byte) (gethabi.Arguments, error) {
	var args gethabi.Arguments
	if method, ok := a.Methods[name]; ok {
		if len(data)%32 != 0 {
			return nil, fmt.Errorf("abi: improperly formatted output: %q - Bytes: %+v", data, data)
		}
		args = method.Outputs
	}
	if event, ok := a.Events[name]; ok {
		args = event.Inputs
	}
	if e, ok := a.Errors[name]; ok {
		args = e.Inputs
	}
	if args == nil {
		return nil, fmt.Errorf("abi: could not locate named method, event or error: %s", name)
	}
	return args, nil
}

// UnpackLog is geth's bind.BoundContract.UnpackLog: log data into out, then the
// indexed topics into out's fields.
func UnpackLog(a *gethabi.ABI, out any, event string, log *types.Log) error {
	if len(log.Topics) == 0 {
		return ErrNoEventSignature
	}
	ev, ok := a.Events[event]
	if !ok {
		return fmt.Errorf("abi: could not locate event: %s", event)
	}
	if log.Topics[0] != ev.ID {
		return ErrEventSignatureMismatch
	}
	if len(log.Data) > 0 {
		if err := UnpackIntoInterface(a, out, event, log.Data); err != nil {
			return err
		}
	}
	return parseTopics(out, ev.Inputs, true, log.Topics[1:])
}

// ParseTopics is geth's abi.ParseTopics: fields must all be indexed and match
// topics one to one; out must point to a struct.
func ParseTopics(out any, fields gethabi.Arguments, topics []common.Hash) error {
	return parseTopics(out, fields, false, topics)
}

// parseTopics decodes topics into out. With onlyIndexed, non-indexed fields are
// skipped instead of rejected.
func parseTopics(out any, fields gethabi.Arguments, onlyIndexed bool, topics []common.Hash) error {
	count := len(fields)
	if onlyIndexed {
		count = 0
		for i := range fields {
			if fields[i].Indexed {
				count++
			}
		}
	}
	if count != len(topics) {
		return fmt.Errorf("topic/field count mismatch")
	}
	if count == 0 {
		return nil
	}
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("abi: cannot parse topics into %T", out)
	}
	dst := rv.Elem()
	plan, err := structFields(dst.Type(), modeTopic, len(fields), func(i int) string { return fields[i].Name })
	if err != nil {
		return err
	}
	k := 0
	for i := range fields {
		arg := &fields[i]
		if !arg.Indexed {
			if onlyIndexed {
				continue
			}
			return fmt.Errorf("non-indexed field in topic reconstruction")
		}
		topic := topics[k][:]
		k++
		t := &arg.Type
		switch t.T {
		case gethabi.TupleTy:
			return fmt.Errorf("tuple type in topic reconstruction")
		case gethabi.StringTy, gethabi.BytesTy, gethabi.SliceTy, gethabi.ArrayTy:
			t = &hashType
		case gethabi.FunctionTy:
			if topic[0]|topic[1]|topic[2]|topic[3]|topic[4]|topic[5]|topic[6]|topic[7] != 0 {
				return fmt.Errorf("bind: got improperly encoded function type, got %v", topic)
			}
		}
		field, err := plan.field(dst, i)
		if err != nil {
			return err
		}
		if !field.IsValid() {
			return fmt.Errorf("abi: field %s can't be found in the given value", gethabi.ToCamelCase(arg.Name))
		}
		if !field.CanSet() {
			return errUnmarshal(t, field.Type())
		}
		if t.T == gethabi.FunctionTy {
			// geth takes the 24 low bytes of a function topic
			f, err := deref(t, field)
			if err != nil {
				return err
			}
			if f.Kind() == reflect.Interface {
				if !f.CanSet() || !reflect.TypeFor[[24]byte]().AssignableTo(f.Type()) {
					return errUnmarshal(t, f.Type())
				}
				var fn [24]byte
				copy(fn[:], topic[8:])
				f.Set(reflect.ValueOf(fn))
				continue
			}
			if err = setByteArray(t, f, topic[8:]); err != nil {
				return err
			}
			continue
		}
		if err = decode(t, 0, topic, field); err != nil {
			return err
		}
	}
	return nil
}
