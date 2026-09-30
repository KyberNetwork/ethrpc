package ethrpc

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// Offline checks of the request/response middlewares on the new codec.

var slot0ABI, _ = abi.JSON(strings.NewReader(`[{"type":"function","name":"slot0","stateMutability":"view","inputs":[],"outputs":[
	{"name":"sqrtPriceX96","type":"uint160"},{"name":"tick","type":"int24"},{"name":"observationIndex","type":"uint16"},
	{"name":"observationCardinality","type":"uint16"},{"name":"observationCardinalityNext","type":"uint16"},
	{"name":"feeProtocol","type":"uint8"},{"name":"unlocked","type":"bool"}]},
	{"type":"function","name":"ticks","stateMutability":"view","inputs":[{"name":"tick","type":"int24"}],"outputs":[
	{"name":"liquidityNet","type":"int128"}]}]`))

type slot0 struct {
	SqrtPriceX96               uint256.Int
	Tick                       int32
	ObservationIndex           uint16
	ObservationCardinality     uint16
	ObservationCardinalityNext uint16
	FeeProtocol                uint8
	Unlocked                   bool
}

func slot0Response(t *testing.T, tick int64) []byte {
	out, err := slot0ABI.Methods["slot0"].Outputs.Pack(big.NewInt(1<<62), big.NewInt(tick), uint16(1), uint16(2), uint16(3), uint8(0), true)
	require.NoError(t, err)
	return out
}

func TestParseRequestTryAggregate(t *testing.T) {
	c := &Client{multiCallContract: common.HexToAddress("0x5ba1e12693dc8f9c48aad8770482f4739beed696")}
	req := c.R()
	target := "0x88e6a0c2ddd26feeb64f039a2c41296fcb3f5640"
	req.AddCall(&Call{ABI: slot0ABI, Target: target, Method: "ticks", Params: []any{int32(-887220)}}, []any{new(uint256.Int)})
	req.Method = MethodTryAggregate
	require.NoError(t, parseRequestCallParam(c, req))

	// same calldata as geth with *big.Int params
	inner, err := slot0ABI.Pack("ticks", big.NewInt(-887220))
	require.NoError(t, err)
	want, err := multicallABI.Pack(MethodTryAggregate, false, []MultiCallParam{{common.HexToAddress(target), inner}})
	require.NoError(t, err)
	require.Equal(t, want, req.RawCallMsg.Data)
}

func TestParseResponseTryAggregate(t *testing.T) {
	c := &Client{}
	var ok, fallback slot0
	var neg uint256.Int
	req := c.R()
	req.AddCall(&Call{ABI: slot0ABI, Method: "slot0"}, []any{&ok})
	req.AddCall(&Call{ABI: slot0ABI, Method: "slot0"}, []any{&fallback})
	req.AddCall(&Call{ABI: slot0ABI, Method: "ticks"}, []any{&neg})
	req.Method = MethodTryAggregate

	liq, err := slot0ABI.Methods["ticks"].Outputs.Pack(big.NewInt(-5))
	require.NoError(t, err)
	raw, err := multicallABI.Methods[MethodTryAggregate].Outputs.Pack([]TryAggregateResultItem{
		{true, slot0Response(t, -201234)}, {false, nil}, {true, liq},
	})
	require.NoError(t, err)

	res := &Response{Request: req, RawResponse: raw}
	require.NoError(t, parseResponse(c, res))
	require.Equal(t, []bool{true, false, true}, res.Result)
	require.Equal(t, int32(-201234), ok.Tick)
	require.Equal(t, uint64(1<<62), ok.SqrtPriceX96.Uint64())
	require.True(t, ok.Unlocked)
	require.Equal(t, slot0{}, fallback)
	require.Equal(t, int64(-5), int64(neg.Uint64())) // int128 two's complement
}

func TestParseResponseAggregate(t *testing.T) {
	c := &Client{}
	var s slot0
	req := c.R()
	req.AddCall(&Call{ABI: slot0ABI, Method: "slot0"}, []any{&s})
	req.Method = MethodAggregate
	raw, err := multicallABI.Methods[MethodAggregate].Outputs.Pack(big.NewInt(123), [][]byte{slot0Response(t, 887000)})
	require.NoError(t, err)

	res := &Response{Request: req, RawResponse: raw}
	require.NoError(t, parseResponse(c, res))
	require.Equal(t, int64(123), res.BlockNumber.Int64())
	require.Equal(t, int32(887000), s.Tick)
}
