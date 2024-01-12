package avalanche

import (
	"context"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

type Client struct {
	client ethclient.Client
}

func NewClient(url string) (*Client, error) {
	client, err := ethclient.Dial(url)
	if err != nil {
		return nil, err
	}

	return &Client{client: client}, nil
}

func (c *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return c.client.SuggestGasPrice(ctx)
}

func (c *Client) EstimateGas(ctx context.Context, call ethereum.CallMsg) (uint64, error) {
	return c.client.EstimateGas(ctx, transformCallMsg(call))
}

func (c *Client) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	return c.client.StorageAt(ctx, account, key, blockNumber)
}

func (c *Client) CallContractAtHash(ctx context.Context, call ethereum.CallMsg, blockHash common.Hash) ([]byte, error) {
	return c.client.CallContractAtHash(ctx, transformCallMsg(call), blockHash)
}

func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return c.client.CallContract(ctx, transformCallMsg(call), blockNumber)
}

func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {

	return c.client.BlockNumber(ctx)
}

func transformCallMsg(call ethereum.CallMsg) interfaces.CallMsg {
	var accessList types.AccessList
	for _, accessTuple := range call.AccessList {
		accessList = append(accessList, types.AccessTuple{
			Address:     accessTuple.Address,
			StorageKeys: accessTuple.StorageKeys,
		})
	}

	return interfaces.CallMsg{
		From:       call.From,
		To:         call.To,
		Gas:        call.Gas,
		GasPrice:   call.GasPrice,
		GasFeeCap:  call.GasFeeCap,
		GasTipCap:  call.GasTipCap,
		Value:      call.Value,
		Data:       call.Data,
		AccessList: accessList,
	}
}
