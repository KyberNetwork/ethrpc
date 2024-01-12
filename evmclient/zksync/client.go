package zksync

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/zksync-sdk/zksync2-go/clients"
)

type Client struct {
	c clients.Client
}

func NewZKSyncClient(url string) (*Client, error) {
	c, err := clients.Dial(url)
	if err != nil {
		return nil, err
	}

	return &Client{c: c}, err
}

func (c *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return c.SuggestGasPrice(ctx)
}

func (c *Client) EstimateGas(ctx context.Context, call ethereum.CallMsg) (uint64, error) {
	return c.EstimateGas(ctx, call)
}

func (c *Client) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	return c.StorageAt(ctx, account, key, blockNumber)
}

func (c *Client) CallContractAtHash(ctx context.Context, call ethereum.CallMsg, blockHash common.Hash) ([]byte, error) {
	return c.CallContractAtHash(ctx, call, blockHash)
}

func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return c.CallContract(ctx, call, blockNumber)
}

func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	return c.BlockNumber(ctx)
}
