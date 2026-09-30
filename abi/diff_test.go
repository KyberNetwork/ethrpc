package abi_test

import (
	"bytes"
	"fmt"
	"math/big"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	ethabi "github.com/KyberNetwork/ethrpc/abi"
)

// corpusJSON declares methods whose inputs equal their outputs, so geth can encode
// random values that are then decoded by both codecs.
var corpusJSON = `[
` + method("uints", `uint8 a,uint16 b,uint24 c,uint32 d,uint40 e,uint56 e2,uint64 f,uint72 g,uint128 h,uint160 i,uint248 i2,uint256 j`) + `,
` + method("ints", `int8 a,int16 b,int24 c,int32 d,int40 e,int56 e2,int64 f,int72 g,int128 h,int160 i,int248 i2,int256 j`) + `,
` + method("misc", `bool a,address b,bytes1 c,bytes4 d,bytes20 e,bytes32 f,bytes g,string h`) + `,
` + method("arrays", `uint24[3] a,int256[] b,address[2][] c,bytes[] d,string[2] e,uint8[][2] f,int24[2][3] g,uint160[] h`) + `,
` + method("single_int24", `int24 x`) + `,
` + method("single_uint256", `uint256 x`) + `,
` + method("single_slice", `int16[] x`) + `,
` + method("single_static_arr", `int24[4] x`) + `,
` + `{"type":"function","name":"tuples","stateMutability":"view",` + tupleIO(`
	{"name":"s","type":"tuple","components":[
		{"name":"a","type":"uint160"},{"name":"b","type":"int24"},
		{"name":"e","type":"tuple","components":[{"name":"c","type":"bool"},{"name":"d","type":"bytes"}]}]},
	{"name":"list","type":"tuple[]","components":[{"name":"x","type":"int24"},{"name":"y","type":"uint256[]"}]},
	{"name":"fixed","type":"tuple[2]","components":[{"name":"p","type":"uint8"},{"name":"q","type":"address"}]},
	{"name":"st","type":"tuple","components":[{"name":"a","type":"uint128"},{"name":"b","type":"int24"}]},
	{"name":"arr","type":"uint256[2]"},
	{"name":"inl","type":"tuple","components":[
		{"name":"a","type":"uint24[2]"},
		{"name":"b","type":"tuple","components":[{"name":"x","type":"int8"},{"name":"y","type":"uint160"}]},
		{"name":"c","type":"bool"}]},
	{"name":"dyn","type":"tuple","components":[
		{"name":"a","type":"int24[2][2]"},
		{"name":"b","type":"tuple","components":[{"name":"x","type":"int8"},{"name":"y","type":"uint160"}]},
		{"name":"c","type":"string"},
		{"name":"d","type":"int16"}]},
	{"name":"z","type":"int8"}`) + `},
` + `{"type":"function","name":"nested","stateMutability":"view",` + tupleIO(`
	{"name":"deep","type":"tuple","components":[
		{"name":"inner","type":"tuple[]","components":[{"name":"a","type":"int24[]"},{"name":"b","type":"string"}]},
		{"name":"c","type":"uint160"}]},
	{"name":"w","type":"int256"}`) + `}
]`

func method(name, args string) string {
	var parts []string
	for _, a := range strings.Split(args, ",") {
		f := strings.Fields(a)
		parts = append(parts, fmt.Sprintf(`{"name":%q,"type":%q}`, f[1], f[0]))
	}
	return `{"type":"function","name":"` + name + `","stateMutability":"view",` + tupleIO(strings.Join(parts, ",")) + `}`
}

func tupleIO(args string) string {
	return `"inputs":[` + args + `],"outputs":[` + args + `]`
}

var (
	corpus      = mustABI(corpusJSON)
	corpusNames = []string{"uints", "ints", "misc", "arrays", "single_int24", "single_uint256",
		"single_slice", "single_static_arr", "tuples", "nested"}
)

func mustABI(s string) gethabi.ABI {
	a, err := gethabi.JSON(strings.NewReader(s))
	if err != nil {
		panic(err)
	}
	return a
}

// Destination variants for integers.
const (
	varNatural = iota // geth's own Go types (*big.Int for non-native sizes)
	varU256           // uint256.Int everywhere
	varU256Ptr        // *uint256.Int everywhere
	varNative         // smallest native Go int able to hold the ABI type, else uint256.Int
	varSigned         // like varNative but signed Go ints for uint ABI types
	numVariants
)

func nativeFor(t *gethabi.Type, signed bool) reflect.Type {
	need := t.Size
	if t.T == gethabi.UintTy && signed {
		need++
	}
	signed = signed || t.T == gethabi.IntTy
	ints := []reflect.Type{reflect.TypeFor[int8](), reflect.TypeFor[int16](), reflect.TypeFor[int32](), reflect.TypeFor[int64]()}
	uints := []reflect.Type{reflect.TypeFor[uint8](), reflect.TypeFor[uint16](), reflect.TypeFor[uint32](), reflect.TypeFor[uint64]()}
	for i, bits := range []int{8, 16, 32, 64} {
		if need <= bits {
			if signed {
				return ints[i]
			}
			return uints[i]
		}
	}
	return reflect.TypeFor[uint256.Int]()
}

// altType is the Go type used to decode t for a destination variant.
func altType(t *gethabi.Type, variant int) reflect.Type {
	switch t.T {
	case gethabi.IntTy, gethabi.UintTy:
		switch variant {
		case varU256:
			return reflect.TypeFor[uint256.Int]()
		case varU256Ptr:
			return reflect.TypeFor[*uint256.Int]()
		case varNative, varSigned:
			return nativeFor(t, variant == varSigned)
		}
		return t.GetType()
	case gethabi.SliceTy:
		return reflect.SliceOf(altType(t.Elem, variant))
	case gethabi.ArrayTy:
		return reflect.ArrayOf(t.Size, altType(t.Elem, variant))
	case gethabi.TupleTy:
		fields := make([]reflect.StructField, len(t.TupleElems))
		for i, e := range t.TupleElems {
			fields[i] = reflect.StructField{Name: t.TupleType.Field(i).Name, Type: altType(e, variant)}
		}
		return reflect.StructOf(fields)
	}
	return t.GetType()
}

// outStruct builds a struct type with one camel-cased field per output.
func outStruct(args gethabi.Arguments, variant int) reflect.Type {
	fields := make([]reflect.StructField, len(args))
	for i, a := range args {
		fields[i] = reflect.StructField{Name: gethabi.ToCamelCase(a.Name), Type: altType(&a.Type, variant)}
	}
	return reflect.StructOf(fields)
}

// genValue returns a random value of geth's natural Go type for t.
func genValue(r *rand.Rand, t *gethabi.Type) reflect.Value {
	switch t.T {
	case gethabi.IntTy, gethabi.UintTy:
		b := randInt(r, t)
		v := reflect.New(t.GetType()).Elem()
		switch v.Kind() {
		case reflect.Pointer:
			v.Set(reflect.ValueOf(b))
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			v.SetInt(b.Int64())
		default:
			v.SetUint(b.Uint64())
		}
		return v
	case gethabi.BoolTy:
		return reflect.ValueOf(r.Intn(2) == 1)
	case gethabi.AddressTy:
		var a common.Address
		r.Read(a[:])
		return reflect.ValueOf(a)
	case gethabi.FixedBytesTy:
		v := reflect.New(t.GetType()).Elem()
		for i := 0; i < t.Size; i++ {
			v.Index(i).SetUint(uint64(r.Intn(256)))
		}
		return v
	case gethabi.BytesTy:
		b := make([]byte, r.Intn(70))
		r.Read(b)
		return reflect.ValueOf(b)
	case gethabi.StringTy:
		b := make([]byte, r.Intn(70))
		for i := range b {
			b[i] = byte('a' + r.Intn(26))
		}
		return reflect.ValueOf(string(b))
	case gethabi.SliceTy:
		n := r.Intn(4)
		v := reflect.MakeSlice(t.GetType(), n, n)
		for i := 0; i < n; i++ {
			v.Index(i).Set(genValue(r, t.Elem))
		}
		return v
	case gethabi.ArrayTy:
		v := reflect.New(t.GetType()).Elem()
		for i := 0; i < t.Size; i++ {
			v.Index(i).Set(genValue(r, t.Elem))
		}
		return v
	case gethabi.TupleTy:
		v := reflect.New(t.TupleType).Elem()
		for i, e := range t.TupleElems {
			v.Field(i).Set(genValue(r, e))
		}
		return v
	}
	panic("unsupported type " + t.String())
}

var two256 = new(big.Int).Lsh(big.NewInt(1), 256)

// randInt picks a value of intN/uintN, biased towards the range boundaries.
func randInt(r *rand.Rand, t *gethabi.Type) *big.Int {
	n := uint(t.Size)
	lo, hi := new(big.Int), new(big.Int).Lsh(big.NewInt(1), n) // [lo, hi)
	if t.T == gethabi.IntTy {
		hi.Rsh(hi, 1)
		lo.Neg(hi)
	}
	switch r.Intn(6) {
	case 0:
		return lo
	case 1:
		return hi.Sub(hi, big.NewInt(1))
	case 2:
		return big.NewInt(0)
	case 3:
		if t.T == gethabi.IntTy {
			return big.NewInt(-1)
		}
		return big.NewInt(1)
	}
	span := new(big.Int).Sub(hi, lo)
	x := new(big.Int).Rand(r, span)
	return x.Add(x, lo)
}

// leafBig returns the integer held by a decoded value, reading uint256 as two's
// complement when the ABI type is signed.
func leafBig(t *gethabi.Type, v reflect.Value) *big.Int {
	if v.Kind() == reflect.Pointer && v.Type() != reflect.TypeFor[*big.Int]() {
		v = v.Elem()
	}
	switch x := v.Interface().(type) {
	case *big.Int:
		return x
	case uint256.Int:
		b := x.ToBig()
		if t.T == gethabi.IntTy && b.Bit(255) == 1 {
			b.Sub(b, two256)
		}
		return b
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return big.NewInt(v.Int())
	default:
		return new(big.Int).SetUint64(v.Uint())
	}
}

// compare checks that decoded value got equals geth's natural value want.
func compare(t *gethabi.Type, want, got reflect.Value, path string) error {
	for want.Kind() == reflect.Interface {
		want = want.Elem()
	}
	switch t.T {
	case gethabi.IntTy, gethabi.UintTy:
		if a, b := leafBig(t, want), leafBig(t, got); a.Cmp(b) != 0 {
			return fmt.Errorf("%s: want %v got %v (%v)", path, a, b, got.Type())
		}
		return nil
	case gethabi.SliceTy, gethabi.ArrayTy:
		if want.Len() != got.Len() {
			return fmt.Errorf("%s: len want %d got %d", path, want.Len(), got.Len())
		}
		for i := 0; i < want.Len(); i++ {
			if err := compare(t.Elem, want.Index(i), got.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case gethabi.TupleTy:
		for i, e := range t.TupleElems {
			if err := compare(e, want.Field(i), got.Field(i), path+"."+t.TupleRawNames[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if !reflect.DeepEqual(want.Interface(), got.Interface()) {
		return fmt.Errorf("%s: want %v got %v", path, want, got)
	}
	return nil
}

// toAlt converts geth's natural value into the variant's Go type (for Pack).
func toAlt(t *gethabi.Type, v reflect.Value, typ reflect.Type) reflect.Value {
	out := reflect.New(typ).Elem()
	switch t.T {
	case gethabi.IntTy, gethabi.UintTy:
		b := leafBig(t, v)
		dst := out
		if typ.Kind() == reflect.Pointer {
			dst = reflect.New(typ.Elem())
			out.Set(dst)
			dst = dst.Elem()
		}
		switch dst.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			dst.SetInt(b.Int64())
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			dst.SetUint(b.Uint64())
		case reflect.Pointer:
			dst.Set(reflect.ValueOf(new(big.Int).Set(b)))
		default:
			var u uint256.Int
			u.SetFromBig(b)
			dst.Set(reflect.ValueOf(u))
		}
	case gethabi.SliceTy:
		out.Set(reflect.MakeSlice(typ, v.Len(), v.Len()))
		fallthrough
	case gethabi.ArrayTy:
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(toAlt(t.Elem, v.Index(i), typ.Elem()))
		}
	case gethabi.TupleTy:
		for i, e := range t.TupleElems {
			out.Field(i).Set(toAlt(e, v.Field(i), typ.Field(i).Type))
		}
	default:
		out.Set(v)
	}
	return out
}

// outOfRange reports whether geth accepted an intN/uintN (N not in 8,16,32,64)
// that does not fit N bits; the new codec rejects those.
func outOfRange(t *gethabi.Type, v reflect.Value) bool {
	for v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	switch t.T {
	case gethabi.IntTy, gethabi.UintTy:
		b, ok := v.Interface().(*big.Int)
		if !ok || t.Size >= 256 {
			return false
		}
		if t.T == gethabi.UintTy {
			return b.BitLen() > t.Size
		}
		lim := new(big.Int).Lsh(big.NewInt(1), uint(t.Size-1))
		return b.Cmp(lim) >= 0 || b.Cmp(new(big.Int).Neg(lim)) < 0
	case gethabi.SliceTy, gethabi.ArrayTy:
		for i := 0; i < v.Len(); i++ {
			if outOfRange(t.Elem, v.Index(i)) {
				return true
			}
		}
	case gethabi.TupleTy:
		for i, e := range t.TupleElems {
			if outOfRange(e, v.Field(i)) {
				return true
			}
		}
	}
	return false
}

func gethUnpack(args gethabi.Arguments, data []byte) (vals []any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("geth panic: %v", r)
		}
	}()
	return args.Unpack(data)
}

// checkDecode decodes data with geth and with every destination variant of the new
// codec and asserts equal values, or equal rejection.
func checkDecode(t testing.TB, name string, data []byte) {
	m := corpus.Methods[name]
	want, gethErr := gethUnpack(m.Outputs, data)
	gethLax := false
	if gethErr == nil {
		for i := range m.Outputs {
			gethLax = gethLax || outOfRange(&m.Outputs[i].Type, reflect.ValueOf(want[i]))
		}
	}
	for variant := 0; variant < numVariants; variant++ {
		out := reflect.New(outStruct(m.Outputs, variant))
		err := ethabi.UnpackArgs(m.Outputs, out.Interface(), data)
		switch {
		case gethErr != nil:
			if err == nil {
				t.Fatalf("%s variant %d: geth rejected (%v) but new codec accepted %x", name, variant, gethErr, data)
			}
		case gethLax:
			if err == nil {
				t.Fatalf("%s variant %d: out-of-range integer accepted %x", name, variant, data)
			}
		case err != nil:
			t.Fatalf("%s variant %d: geth accepted but new codec failed: %v\n%x", name, variant, err, data)
		default:
			for i := range m.Outputs {
				arg := &m.Outputs[i]
				if cerr := compare(&arg.Type, reflect.ValueOf(want[i]), out.Elem().Field(i), name+"."+arg.Name); cerr != nil {
					t.Fatalf("variant %d: %v", variant, cerr)
				}
			}
		}
	}
}

func genArgs(r *rand.Rand, args gethabi.Arguments) []any {
	vals := make([]any, len(args))
	for i := range args {
		vals[i] = genValue(r, &args[i].Type).Interface()
	}
	return vals
}

func TestDifferentialDecode(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for _, name := range corpusNames {
		t.Run(name, func(t *testing.T) {
			m := corpus.Methods[name]
			for iter := 0; iter < 300; iter++ {
				data, err := m.Inputs.Pack(genArgs(r, m.Inputs)...)
				if err != nil {
					t.Fatal(err)
				}
				checkDecode(t, name, data)

				// geth's own struct decoding must match ours into the same natural struct
				natural := outStruct(m.Outputs, varNatural)
				a, b := reflect.New(natural), reflect.New(natural)
				gerr := corpus.UnpackIntoInterface(a.Interface(), name, data)
				nerr := ethabi.UnpackIntoInterface(&corpus, b.Interface(), name, data)
				if gerr != nil || nerr != nil {
					t.Fatalf("UnpackIntoInterface: geth %v, new %v", gerr, nerr)
				}
				if !reflect.DeepEqual(a.Interface(), b.Interface()) {
					t.Fatalf("natural struct mismatch:\ngeth %+v\nnew  %+v", a.Elem(), b.Elem())
				}

				for k := 0; k < 8; k++ {
					checkDecode(t, name, mutate(r, data))
				}
			}
		})
	}
}

// mutate corrupts an encoding: byte flips, overwritten words, truncation, junk.
func mutate(r *rand.Rand, data []byte) []byte {
	d := bytes.Clone(data)
	if len(d) == 0 {
		return []byte{1}
	}
	switch r.Intn(6) {
	case 0:
		d[r.Intn(len(d))] ^= byte(1 + r.Intn(255))
	case 1: // one byte of a word's head, e.g. breaks range or offsets
		w := r.Intn(len(d) / 32)
		d[w*32+r.Intn(4)] = 0xff
	case 2: // word set to a small offset/length
		w := r.Intn(len(d) / 32)
		copy(d[w*32:], make([]byte, 32))
		d[w*32+31] = byte(r.Intn(256))
		d[w*32+30] = byte(r.Intn(3))
	case 3:
		d = d[:r.Intn(len(d))]
	case 4:
		d = append(d, make([]byte, 32*(1+r.Intn(2)))...)
	case 5: // 2^N boundary in the low bytes of a word
		w := r.Intn(len(d) / 32)
		copy(d[w*32:], make([]byte, 32))
		d[w*32+31-r.Intn(32)] = 1 << r.Intn(8)
	}
	return d
}

func TestDifferentialPack(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for _, name := range corpusNames {
		t.Run(name, func(t *testing.T) {
			for iter := 0; iter < 300; iter++ {
				checkPack(t, r, name)
			}
		})
	}
}

func checkPack(t testing.TB, r *rand.Rand, name string) {
	m := corpus.Methods[name]
	vals := genArgs(r, m.Inputs)
	want, err := corpus.Pack(name, vals...)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ethabi.Pack(&corpus, name, vals...)
	if err != nil {
		t.Fatalf("%s natural: %v", name, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s natural: pack mismatch\ngeth %x\nnew  %x", name, want, got)
	}
	for variant := varU256; variant < numVariants; variant++ {
		alt := make([]any, len(vals))
		for i := range vals {
			arg := &m.Inputs[i]
			alt[i] = toAlt(&arg.Type, reflect.ValueOf(vals[i]), altType(&arg.Type, variant)).Interface()
		}
		got, err := ethabi.Pack(&corpus, name, alt...)
		if err != nil {
			t.Fatalf("%s variant %d: %v", name, variant, err)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("%s variant %d: pack mismatch\ngeth %x\nnew  %x", name, variant, want, got)
		}
	}
}

func FuzzUnpack(f *testing.F) {
	r := rand.New(rand.NewSource(3))
	for i, name := range corpusNames {
		m := corpus.Methods[name]
		for k := 0; k < 4; k++ {
			data, err := m.Inputs.Pack(genArgs(r, m.Inputs)...)
			if err != nil {
				f.Fatal(err)
			}
			f.Add(uint8(i), data)
		}
	}
	f.Fuzz(func(t *testing.T, which uint8, data []byte) {
		checkDecode(t, corpusNames[int(which)%len(corpusNames)], data)
	})
}

func FuzzPack(f *testing.F) {
	for i := int64(0); i < 8; i++ {
		f.Add(i)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		r := rand.New(rand.NewSource(seed))
		checkPack(t, r, corpusNames[r.Intn(len(corpusNames))])
	})
}

func newRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }
