package ethrpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
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

func (ts *RPCTestSuite) TestGetBlockNumber() {
	blockNumber, err := ts.client.GetBlockNumber(context.Background())

	ts.Require().NoError(err)
	ts.Require().NotEqual(0, blockNumber)
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
	ts.Require().NoError(err)

	fmt.Printf("%+v\n", reserves)
	fmt.Printf("Block Number: %+v\n", res.BlockNumber.Int64())

	ts.Require().NoError(err)
	ts.Require().Len(res.Result, len(req.Calls))
}

func (ts *RPCTestSuite) TestRetryOnError() {
	retryCount := 0
	retryClient := New("https://rpc.monad.xyz").
		SetRetryCount(3).
		SetRetryDelay(400 * time.Millisecond).
		SetRetryCondition(func(err error) bool {
			if errors.Is(err, ethereum.NotFound) ||
				strings.Contains(strings.ToLower(err.Error()), "not found") ||
				strings.Contains(strings.ToLower(err.Error()), "unknown block") {
				retryCount++
				return true
			}

			return false
		})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	latestBlock, err := retryClient.GetBlockNumber(ctx)
	ts.Require().NoError(err)
	ts.Require().NotZero(latestBlock)

	futureBlock := latestBlock + 2

	ts.T().Logf("latest block: %d, requesting block hash at: %d", latestBlock, futureBlock)

	start := time.Now()
	err = retryClient.WithRetry(ctx, "get block hash", func() error {
		_, err := retryClient.BalanceAt(ctx, common.Address{}, new(big.Int).SetUint64(futureBlock))
		if err != nil {
			return err
		}
		return nil
	})
	elapsed := time.Since(start)

	ts.Require().NoError(err)
	ts.Require().LessOrEqual(retryCount, 3)

	ts.T().Logf("elapsed=%v, retryCount=%d", elapsed, retryCount)
}

func TestRPCTestSuite(t *testing.T) {
	suite.Run(t, new(RPCTestSuite))
}
