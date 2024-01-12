package evmclient

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Client struct {
	client *ethclient.Client
}

func NewCommonClient(url string) (*Client, error) {
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
	return c.client.EstimateGas(ctx, call)
}

func (c *Client) StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	return c.client.StorageAt(ctx, account, key, blockNumber)
}

func (c *Client) CallContractAtHash(ctx context.Context, call ethereum.CallMsg, blockHash common.Hash) ([]byte, error) {
	return c.client.CallContractAtHash(ctx, call, blockHash)
}

func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return c.client.CallContract(ctx, call, blockNumber)
}

func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	return c.client.BlockNumber(ctx)
}
