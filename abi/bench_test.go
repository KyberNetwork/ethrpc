package abi_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	ethabi "github.com/KyberNetwork/ethrpc/abi"
)

var multicall = mustABI(`[
{"type":"function","name":"tryAggregate","stateMutability":"nonpayable",
 "inputs":[{"name":"requireSuccess","type":"bool"},{"name":"calls","type":"tuple[]","components":[
	{"name":"target","type":"address"},{"name":"callData","type":"bytes"}]}],
 "outputs":[{"name":"returnData","type":"tuple[]","components":[
	{"name":"success","type":"bool"},{"name":"returnData","type":"bytes"}]}]}]`)

type mcCall struct {
	Target   common.Address
	CallData []byte
}

type mcResult struct {
	Success    bool
	ReturnData []byte
}

const multicallSize = 100

// tryAggregateData is a tryAggregate response carrying multicallSize slot0 results.
func tryAggregateData() []byte {
	res := make([]mcResult, multicallSize)
	for i := range res {
		res[i] = mcResult{true, slot0Data()}
	}
	out, err := multicall.Methods["tryAggregate"].Outputs.Pack(res)
	if err != nil {
		panic(err)
	}
	return out
}

func BenchmarkSlot0(b *testing.B) {
	data := slot0Data()
	b.Run("geth/big.Int", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var out slot0Big
			if err := uniV3.UnpackIntoInterface(&out, "slot0", data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("new/big.Int", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var out slot0Big
			if err := ethabi.UnpackIntoInterface(&uniV3, &out, "slot0", data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("new/uint256+int32", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var out slot0U256
			if err := ethabi.UnpackIntoInterface(&uniV3, &out, "slot0", data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkTicks(b *testing.B) {
	data := ticksData()
	b.Run("geth/big.Int", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var out ticksBig
			if err := uniV3.UnpackIntoInterface(&out, "ticks", data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("new/big.Int", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var out ticksBig
			if err := ethabi.UnpackIntoInterface(&uniV3, &out, "ticks", data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("new/uint256+int64", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var out ticksU256
			if err := ethabi.UnpackIntoInterface(&uniV3, &out, "ticks", data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMulticallSlot0 decodes a tryAggregate response and the slot0 of each call.
func BenchmarkMulticallSlot0(b *testing.B) {
	data := tryAggregateData()
	b.Run("geth/big.Int", func(b *testing.B) {
		b.ReportAllocs()
		out := make([]slot0Big, multicallSize)
		for range b.N {
			var res []mcResult
			if err := multicall.UnpackIntoInterface(&res, "tryAggregate", data); err != nil {
				b.Fatal(err)
			}
			for i := range res {
				if err := uniV3.UnpackIntoInterface(&out[i], "slot0", res[i].ReturnData); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("new/uint256+int32", func(b *testing.B) {
		b.ReportAllocs()
		out := make([]slot0U256, multicallSize)
		for range b.N {
			var res []mcResult
			if err := ethabi.UnpackIntoInterface(&multicall, &res, "tryAggregate", data); err != nil {
				b.Fatal(err)
			}
			for i := range res {
				if err := ethabi.UnpackIntoInterface(&uniV3, &out[i], "slot0", res[i].ReturnData); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// BenchmarkPackMulticallTicks packs ticks(int24) for each call and the tryAggregate
// calldata around them.
func BenchmarkPackMulticallTicks(b *testing.B) {
	target := common.HexToAddress("0x88e6a0c2ddd26feeb64f039a2c41296fcb3f5640")
	b.Run("geth/big.Int", func(b *testing.B) {
		b.ReportAllocs()
		calls := make([]mcCall, multicallSize)
		for range b.N {
			for i := range calls {
				cd, err := uniV3.Pack("ticks", big.NewInt(int64(i*60-3000)))
				if err != nil {
					b.Fatal(err)
				}
				calls[i] = mcCall{target, cd}
			}
			if _, err := multicall.Pack("tryAggregate", false, calls); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("new/int32", func(b *testing.B) {
		b.ReportAllocs()
		calls := make([]mcCall, multicallSize)
		for range b.N {
			for i := range calls {
				cd, err := ethabi.Pack(&uniV3, "ticks", int32(i*60-3000))
				if err != nil {
					b.Fatal(err)
				}
				calls[i] = mcCall{target, cd}
			}
			if _, err := ethabi.Pack(&multicall, "tryAggregate", false, calls); err != nil {
				b.Fatal(err)
			}
		}
	})
}
