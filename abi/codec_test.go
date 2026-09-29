package abi_test

import (
	"bytes"
	"math/big"
	"strings"
	"testing"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	ethabi "github.com/KyberNetwork/ethrpc/abi"
)

func args(t *testing.T, types ...string) gethabi.Arguments {
	t.Helper()
	out := make(gethabi.Arguments, len(types))
	for i, s := range types {
		typ, err := gethabi.NewType(s, "", nil)
		require.NoError(t, err)
		out[i] = gethabi.Argument{Name: "v", Type: typ}
	}
	return out
}

func word(b *big.Int) []byte {
	var u uint256.Int
	u.SetFromBig(b)
	w := u.Bytes32()
	return w[:]
}

func TestInt24ToInt32Negative(t *testing.T) {
	a := args(t, "int24")
	data := word(big.NewInt(-887272)) // Uniswap v3 MIN_TICK
	var tick int32
	require.NoError(t, ethabi.UnpackArgs(a, &tick, data))
	require.Equal(t, int32(-887272), tick)

	var tick64 int64
	require.NoError(t, ethabi.UnpackArgs(a, &tick64, data))
	require.Equal(t, int64(-887272), tick64)
}

func TestUint24ToSignedInt32(t *testing.T) {
	var v int32
	require.NoError(t, ethabi.UnpackArgs(args(t, "uint24"), &v, word(big.NewInt(1<<24-1))))
	require.Equal(t, int32(1<<24-1), v)
}

func TestNativeWidthAndSignRejected(t *testing.T) {
	var u32 uint32
	err := ethabi.UnpackArgs(args(t, "int24"), &u32, word(big.NewInt(1)))
	require.ErrorContains(t, err, "cannot unmarshal int24 in to uint32")

	var u64 uint64
	err = ethabi.UnpackArgs(args(t, "uint160"), &u64, word(big.NewInt(1)))
	require.ErrorContains(t, err, "cannot unmarshal uint160 in to uint64")

	var i16 int16
	err = ethabi.UnpackArgs(args(t, "uint16"), &i16, word(big.NewInt(1)))
	require.ErrorContains(t, err, "cannot unmarshal uint16 in to int16")

	var i8 int8
	err = ethabi.UnpackArgs(args(t, "int16"), &i8, word(big.NewInt(1)))
	require.ErrorContains(t, err, "cannot unmarshal int16 in to int8")
}

func TestInt256ExtremesToUint256(t *testing.T) {
	a := args(t, "int256")
	maxI := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(1))
	minI := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))

	var u uint256.Int
	require.NoError(t, ethabi.UnpackArgs(a, &u, word(maxI)))
	require.Equal(t, uint256.Int{^uint64(0), ^uint64(0), ^uint64(0), 1<<63 - 1}, u)

	require.NoError(t, ethabi.UnpackArgs(a, &u, word(minI)))
	require.Equal(t, uint256.Int{0, 0, 0, 1 << 63}, u)

	var p *uint256.Int
	require.NoError(t, ethabi.UnpackArgs(a, &p, word(big.NewInt(-1))))
	require.Equal(t, uint256.Int{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)}, *p)

	// the same two's complement words pack back to what geth produces for big.Int
	for _, b := range []*big.Int{maxI, minI, big.NewInt(-1)} {
		var x uint256.Int
		x.SetFromBig(b)
		want, err := a.Pack(b)
		require.NoError(t, err)
		got, err := ethabi.PackArgs(a, &x)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestDecodeOverflowRejected(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		word *big.Int
	}{
		{"uint8", big.NewInt(256)},
		{"uint24", big.NewInt(1 << 24)},
		{"uint160", new(big.Int).Lsh(big.NewInt(1), 160)},
		{"int24", big.NewInt(1 << 23)},
		{"int24", big.NewInt(-(1 << 23) - 1)},
		{"int64", new(big.Int).Lsh(big.NewInt(1), 63)},
	} {
		a := args(t, tc.typ)
		data := word(tc.word)
		var u uint256.Int
		require.ErrorContains(t, ethabi.UnpackArgs(a, &u, data), "improperly encoded "+tc.typ, tc.typ)
		var b *big.Int
		require.Error(t, ethabi.UnpackArgs(a, &b, data), tc.typ)
		var i64 int64
		require.Error(t, ethabi.UnpackArgs(a, &i64, data), tc.typ)
	}
}

func TestEncodeOverflowRejected(t *testing.T) {
	u24 := uint256.NewInt(1 << 24)
	var negOne uint256.Int
	negOne.SetAllOne()
	for _, tc := range []struct {
		typ string
		v   any
	}{
		{"uint24", u24},
		{"uint24", *u24},
		{"uint24", big.NewInt(1 << 24)},
		{"uint24", uint32(1 << 24)},
		{"uint24", int64(-1)},
		{"uint24", big.NewInt(-1)},
		{"uint8", 256},
		{"uint160", new(uint256.Int).Lsh(uint256.NewInt(1), 160)},
		{"uint256", new(big.Int).Lsh(big.NewInt(1), 256)},
		{"int24", int32(1 << 23)},
		{"int24", int32(-(1 << 23) - 1)},
		{"int24", uint256.NewInt(1 << 23)},
		{"int24", big.NewInt(1 << 23)},
		{"int64", uint64(1 << 63)},
		{"int256", new(big.Int).Lsh(big.NewInt(1), 255)},
	} {
		_, err := ethabi.PackArgs(args(t, tc.typ), tc.v)
		require.Error(t, err, "%s %v", tc.typ, tc.v)
	}

	// in-range values of any accepted Go type encode like geth's *big.Int
	for _, tc := range []struct {
		typ string
		v   any
		b   *big.Int
	}{
		{"int24", int32(-(1 << 23)), big.NewInt(-(1 << 23))},
		{"int24", &negOne, big.NewInt(-1)},
		{"uint24", 1<<24 - 1, big.NewInt(1<<24 - 1)},
		{"uint8", uint64(255), big.NewInt(255)},
		{"int256", int8(-5), big.NewInt(-5)},
		{"uint256", uint256.NewInt(7), big.NewInt(7)},
	} {
		a := args(t, tc.typ)
		ref, err := ethabi.PackArgs(a, tc.b)
		require.NoError(t, err)
		require.Equal(t, word(tc.b), ref)
		got, err := ethabi.PackArgs(a, tc.v)
		require.NoError(t, err, "%s %v", tc.typ, tc.v)
		require.Equal(t, ref, got, "%s %v", tc.typ, tc.v)
	}
}

func TestBoolAndOffsetChecks(t *testing.T) {
	var b bool
	require.ErrorContains(t, ethabi.UnpackArgs(args(t, "bool"), &b, word(big.NewInt(2))), "improperly encoded boolean")

	var bs []byte
	data := append(word(big.NewInt(1<<20)), word(big.NewInt(0))...)
	require.ErrorContains(t, ethabi.UnpackArgs(args(t, "bytes"), &bs, data), "would go over slice boundary")

	data = append(word(big.NewInt(32)), word(big.NewInt(64))...)
	require.ErrorContains(t, ethabi.UnpackArgs(args(t, "bytes"), &bs, data), "length insufficient")
}

var uniV3JSON = `[
{"type":"function","name":"slot0","stateMutability":"view","inputs":[],"outputs":[
	{"name":"sqrtPriceX96","type":"uint160"},{"name":"tick","type":"int24"},
	{"name":"observationIndex","type":"uint16"},{"name":"observationCardinality","type":"uint16"},
	{"name":"observationCardinalityNext","type":"uint16"},{"name":"feeProtocol","type":"uint8"},
	{"name":"unlocked","type":"bool"}]},
{"type":"function","name":"ticks","stateMutability":"view","inputs":[{"name":"tick","type":"int24"}],"outputs":[
	{"name":"liquidityGross","type":"uint128"},{"name":"liquidityNet","type":"int128"},
	{"name":"feeGrowthOutside0X128","type":"uint256"},{"name":"feeGrowthOutside1X128","type":"uint256"},
	{"name":"tickCumulativeOutside","type":"int56"},{"name":"secondsPerLiquidityOutsideX128","type":"uint160"},
	{"name":"secondsOutside","type":"uint32"},{"name":"initialized","type":"bool"}]},
{"type":"event","name":"Swap","anonymous":false,"inputs":[
	{"indexed":true,"name":"sender","type":"address"},{"indexed":true,"name":"recipient","type":"address"},
	{"indexed":false,"name":"amount0","type":"int256"},{"indexed":false,"name":"amount1","type":"int256"},
	{"indexed":false,"name":"sqrtPriceX96","type":"uint160"},{"indexed":false,"name":"liquidity","type":"uint128"},
	{"indexed":false,"name":"tick","type":"int24"}]},
{"type":"event","name":"Mint","anonymous":false,"inputs":[
	{"indexed":false,"name":"sender","type":"address"},{"indexed":true,"name":"owner","type":"address"},
	{"indexed":true,"name":"tickLower","type":"int24"},{"indexed":true,"name":"tickUpper","type":"int24"},
	{"indexed":false,"name":"amount","type":"uint128"},{"indexed":false,"name":"amount0","type":"uint256"},
	{"indexed":false,"name":"amount1","type":"uint256"}]}
]`

var uniV3 = mustABI(uniV3JSON)

type slot0Big struct {
	SqrtPriceX96               *big.Int
	Tick                       *big.Int
	ObservationIndex           uint16
	ObservationCardinality     uint16
	ObservationCardinalityNext uint16
	FeeProtocol                uint8
	Unlocked                   bool
}

type slot0U256 struct {
	SqrtPriceX96               uint256.Int
	Tick                       int32
	ObservationIndex           uint16
	ObservationCardinality     uint16
	ObservationCardinalityNext uint16
	FeeProtocol                uint8
	Unlocked                   bool
}

type ticksBig struct {
	LiquidityGross                 *big.Int
	LiquidityNet                   *big.Int
	FeeGrowthOutside0X128          *big.Int
	FeeGrowthOutside1X128          *big.Int
	TickCumulativeOutside          *big.Int
	SecondsPerLiquidityOutsideX128 *big.Int
	SecondsOutside                 uint32
	Initialized                    bool
}

type ticksU256 struct {
	LiquidityGross                 uint256.Int
	LiquidityNet                   uint256.Int // int128, two's complement
	FeeGrowthOutside0X128          uint256.Int
	FeeGrowthOutside1X128          uint256.Int
	TickCumulativeOutside          int64
	SecondsPerLiquidityOutsideX128 uint256.Int
	SecondsOutside                 uint32
	Initialized                    bool
}

func slot0Data() []byte {
	sqrtP, _ := new(big.Int).SetString("1461446703485210103287273052203988822378723970341", 10)
	out, err := uniV3.Methods["slot0"].Outputs.Pack(sqrtP, big.NewInt(-201234), uint16(12), uint16(300), uint16(300), uint8(0), true)
	if err != nil {
		panic(err)
	}
	return out
}

func ticksData() []byte {
	x, _ := new(big.Int).SetString("340282366920938463463374607431768211455", 10)
	out, err := uniV3.Methods["ticks"].Outputs.Pack(x, new(big.Int).Neg(big.NewInt(123456789)), x, big.NewInt(42),
		big.NewInt(-9876543210), big.NewInt(77), uint32(1700000000), true)
	if err != nil {
		panic(err)
	}
	return out
}

func TestSlot0AndTicks(t *testing.T) {
	var g slot0Big
	require.NoError(t, uniV3.UnpackIntoInterface(&g, "slot0", slot0Data()))
	var n slot0U256
	require.NoError(t, ethabi.UnpackIntoInterface(&uniV3, &n, "slot0", slot0Data()))
	require.Equal(t, g.SqrtPriceX96, n.SqrtPriceX96.ToBig())
	require.Equal(t, g.Tick.Int64(), int64(n.Tick))
	require.Equal(t, g.ObservationCardinality, n.ObservationCardinality)
	require.True(t, n.Unlocked)

	var tg ticksBig
	require.NoError(t, uniV3.UnpackIntoInterface(&tg, "ticks", ticksData()))
	var tn ticksU256
	require.NoError(t, ethabi.UnpackIntoInterface(&uniV3, &tn, "ticks", ticksData()))
	require.Equal(t, tg.LiquidityNet, new(big.Int).Sub(tn.LiquidityNet.ToBig(), two256))
	require.Equal(t, tg.TickCumulativeOutside.Int64(), tn.TickCumulativeOutside)

	// geth's own struct decodes identically through the new codec
	var tg2 ticksBig
	require.NoError(t, ethabi.UnpackIntoInterface(&uniV3, &tg2, "ticks", ticksData()))
	require.Equal(t, tg, tg2)
}

func TestUnpackLog(t *testing.T) {
	ev := uniV3.Events["Mint"]
	sender := common.HexToAddress("0x1111111111111111111111111111111111111111")
	owner := common.HexToAddress("0x2222222222222222222222222222222222222222")
	data, err := ev.Inputs.NonIndexed().Pack(sender, big.NewInt(5), big.NewInt(6), big.NewInt(7))
	require.NoError(t, err)
	lower, upper := word(big.NewInt(-887220)), word(big.NewInt(887220))
	log := types.Log{
		Topics: []common.Hash{ev.ID, common.BytesToHash(owner[:]), common.BytesToHash(lower), common.BytesToHash(upper)},
		Data:   data,
	}

	var out struct {
		Sender    common.Address
		Owner     common.Address
		TickLower int32
		TickUpper int32
		Amount    uint256.Int
		Amount0   *uint256.Int
		Amount1   *big.Int
	}
	require.NoError(t, ethabi.UnpackLog(&uniV3, &out, "Mint", &log))
	require.Equal(t, sender, out.Sender)
	require.Equal(t, owner, out.Owner)
	require.Equal(t, int32(-887220), out.TickLower)
	require.Equal(t, int32(887220), out.TickUpper)
	require.Equal(t, uint64(5), out.Amount.Uint64())
	require.Equal(t, uint64(6), out.Amount0.Uint64())
	require.Equal(t, int64(7), out.Amount1.Int64())

	// geth's reference result for the geth-typed struct
	type gethMint struct {
		Sender    common.Address
		Owner     common.Address
		TickLower *big.Int
		TickUpper *big.Int
		Amount    *big.Int
		Amount0   *big.Int
		Amount1   *big.Int
	}
	var g, n gethMint
	require.NoError(t, uniV3.UnpackIntoInterface(&g, "Mint", data))
	var indexed gethabi.Arguments
	for _, a := range ev.Inputs {
		if a.Indexed {
			indexed = append(indexed, a)
		}
	}
	require.NoError(t, gethabi.ParseTopics(&g, indexed, log.Topics[1:]))
	require.NoError(t, ethabi.UnpackLog(&uniV3, &n, "Mint", &log))
	require.Equal(t, g, n)

	log.Topics[0] = common.Hash{1}
	require.ErrorIs(t, ethabi.UnpackLog(&uniV3, &out, "Mint", &log), ethabi.ErrEventSignatureMismatch)
	log.Topics = nil
	require.ErrorIs(t, ethabi.UnpackLog(&uniV3, &out, "Mint", &log), ethabi.ErrNoEventSignature)
}

func TestTopicDynamicTypeIsHash(t *testing.T) {
	a := mustABI(`[{"type":"event","name":"E","inputs":[{"indexed":true,"name":"s","type":"string"},{"indexed":true,"name":"f","type":"function"}]}]`)
	h := common.HexToHash("0xabcdef")
	var fn common.Hash
	copy(fn[8:], bytes.Repeat([]byte{7}, 24))
	var out struct {
		S common.Hash
		F [24]byte
	}
	require.NoError(t, ethabi.ParseTopics(&out, a.Events["E"].Inputs, []common.Hash{h, fn}))
	require.Equal(t, h, out.S)
	require.Equal(t, [24]byte(bytes.Repeat([]byte{7}, 24)), out.F)

	fn[0] = 1
	require.ErrorContains(t, ethabi.ParseTopics(&out, a.Events["E"].Inputs, []common.Hash{h, fn}), "improperly encoded function")
}

func TestInterfaceDestinationsGetGethTypes(t *testing.T) {
	m := corpus.Methods["tuples"]
	data, err := m.Inputs.Pack(genArgs(newRand(5), m.Inputs)...)
	require.NoError(t, err)
	want, err := m.Outputs.Unpack(data)
	require.NoError(t, err)
	got := make([]any, len(m.Outputs))
	require.NoError(t, ethabi.UnpackArgs(m.Outputs, &got, data))
	require.Equal(t, want, got)
}

func TestShortArrayDestinationValidatesAll(t *testing.T) {
	a := args(t, "uint8[]")
	data, err := a.Pack([]uint8{1, 2, 3})
	require.NoError(t, err)
	var two [2]uint8
	require.NoError(t, ethabi.UnpackArgs(a, &two, data))
	require.Equal(t, [2]uint8{1, 2}, two)

	data[len(data)-2] = 1 // third element > 255
	require.Error(t, ethabi.UnpackArgs(a, &two, data))
}

func TestPackMulticallTuples(t *testing.T) {
	a := mustABI(`[{"type":"function","name":"aggregate","inputs":[{"name":"calls","type":"tuple[]","components":[{"name":"target","type":"address"},{"name":"callData","type":"bytes"}]}],"outputs":[]}]`)
	type call struct {
		Target   common.Address
		CallData []byte
	}
	calls := []call{{common.HexToAddress("0x01"), []byte{1, 2, 3}}, {common.HexToAddress("0x02"), bytes.Repeat([]byte{9}, 40)}}
	want, err := a.Pack("aggregate", calls)
	require.NoError(t, err)
	got, err := ethabi.Pack(&a, "aggregate", calls)
	require.NoError(t, err)
	require.Equal(t, want, got)

	_, err = ethabi.Pack(&a, "nope")
	require.True(t, strings.Contains(err.Error(), "not found"))
}
