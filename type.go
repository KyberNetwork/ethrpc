package ethrpc

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type MultiCallParam struct {
	Target   common.Address
	CallData []byte
}

type AggregateResult struct {
	BlockNumber *big.Int
	ReturnData  [][]byte
}

type TryAggregateResultItem struct {
	Success    bool
	ReturnData []byte
}

type TryAggregateResult []TryAggregateResultItem

type TryBlockAndAggregateResult struct {
	BlockNumber *big.Int
	BlockHash   [32]byte
	ReturnData  []TryAggregateResultItem
}
