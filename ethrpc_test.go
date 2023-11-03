package ethrpc

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/suite"
)

type RPCTestSuite struct {
	suite.Suite

	client *Client
}

func (ts *RPCTestSuite) SetupTest() {
	// Setup RPC server
	rpcClient := New("https://eth.llamarpc.com")
	rpcClient.SetMulticallContract(common.HexToAddress("0x5ba1e12693dc8f9c48aad8770482f4739beed696"))

	ts.client = rpcClient
}

func (ts *RPCTestSuite) TestTryAggregate() {
	type TradeInfo struct {
		Reserve0       *big.Int
		Reserve1       *big.Int
		VReserve0      *big.Int
		VReserve1      *big.Int
		FeeInPrecision *big.Int
	}

	pools := []string{
		"0x9a56f30ff04884cb06da80cb3aef09c6132f5e77",
		"0x5ba740fcc020d5b9e39760cbd2fe236586b9dc0a",
		"0x1cf68bbc2b6d3c6cfe1bd3590cf0e10b06a05f17",
	}

	reserves := make([]TradeInfo, len(pools))
	req := ts.client.NewRequest()

	for i, p := range pools {
		req.AddCall(&Call{
			ABI:    dmmPoolABI,
			Target: p,
			Method: "getTradeInfo",
			Params: nil,
		}, []interface{}{&reserves[i]})
	}

	res, err := req.TryAggregate()

	fmt.Printf("%+v\n", reserves)

	ts.Require().NoError(err)
	ts.Require().Len(res.Result, len(req.Calls))
}

func (ts *RPCTestSuite) TestTryBlockAggregate() {
	type TradeInfo struct {
		Reserve0       *big.Int
		Reserve1       *big.Int
		VReserve0      *big.Int
		VReserve1      *big.Int
		FeeInPrecision *big.Int
	}

	pools := []string{
		"0x9a56f30ff04884cb06da80cb3aef09c6132f5e77",
		"0x5ba740fcc020d5b9e39760cbd2fe236586b9dc0a",
		"0x1cf68bbc2b6d3c6cfe1bd3590cf0e10b06a05f17",
	}

	reserves := make([]TradeInfo, len(pools))
	req := ts.client.NewRequest()

	for i, p := range pools {
		req.AddCall(&Call{
			ABI:    dmmPoolABI,
			Target: p,
			Method: "getTradeInfo",
			Params: nil,
		}, []interface{}{&reserves[i]})
	}

	res, err := req.TryBlockAndAggregate()

	fmt.Printf("%+v\n", reserves)
	fmt.Printf("Block Number: %+v\n", res.BlockNumber.Int64())

	ts.Require().NoError(err)
	//ts.Require().Len(res.Result, len(req.Calls))
}

func TestRPCTestSuite(t *testing.T) {
	suite.Run(t, new(RPCTestSuite))
}
