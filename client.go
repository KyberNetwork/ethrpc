package ethrpc

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/KyberNetwork/logger"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
)

const (
	MethodCall = "call"

	MethodAggregate = "aggregate"

	MethodTryAggregate = "tryAggregate"

	MethodGetCurrentBlockTimestamp = "getCurrentBlockTimestamp"

	MethodTryBlockAndAggregate = "tryBlockAndAggregate"
)

var zeroHash common.Hash

type (
	// RequestMiddleware type is for request middleware, called before a request is sent
	RequestMiddleware func(*Client, *Request) error

	// ResponseMiddleware type is for response middleware, called after a response has been received
	ResponseMiddleware func(*Client, *Response) error
)

type Client struct {
	ethClient         *ethclient.Client
	gethClient        *gethclient.Client
	multiCallContract common.Address
	beforeRequest     []RequestMiddleware
	afterResponse     []ResponseMiddleware
	overrides         map[common.Address]gethclient.OverrideAccount
}

// Clone returns a new client with the same configuration as the original client. Except the overrides.
func (c *Client) Clone() *Client {
	CloneClient := Client{
		ethClient:         c.ethClient,
		gethClient:        c.gethClient,
		multiCallContract: c.multiCallContract,
		beforeRequest:     c.beforeRequest,
		afterResponse:     c.afterResponse,
		overrides:         nil,
	}

	return &CloneClient
}

func (c *Client) SetMulticallContract(multiCallContract common.Address) *Client {
	c.multiCallContract = multiCallContract

	return c
}

func (c *Client) SetOverrides(overrides map[common.Address]gethclient.OverrideAccount) *Client {
	c.overrides = overrides

	return c
}

func (c *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return c.ethClient.SuggestGasPrice(ctx)
}

func (c *Client) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	return c.ethClient.EstimateGas(ctx, msg)
}

func (c *Client) GetBlockNumber(ctx context.Context) (uint64, error) {
	return c.ethClient.BlockNumber(ctx)
}

func (c *Client) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	return c.ethClient.BalanceAt(ctx, account, blockNumber)
}

func (c *Client) R() *Request {
	r := &Request{
		client: c,
	}

	if c.overrides != nil {
		r.SetOverrides(c.overrides)
	}

	return r
}

func (c *Client) NewRequest() *Request {
	return c.R()
}

func (c *Client) getStorageAt(ctx context.Context, account common.Address, key common.Hash, abi abi.Arguments) ([]interface{}, error) {
	resp, err := c.ethClient.StorageAt(ctx, account, key, nil)
	if err != nil {
		logger.Errorf("failed to call StorageAt to %v at %v, err: %v", account, key, err)
		return nil, err
	}
	logger.Debugf("raw response %v", common.Bytes2Hex(resp))

	res, err := abi.Unpack(resp)
	if err != nil {
		logger.Errorf("failed to unpack StorageAt to %v at %v, err: %v", account, key, err)
		return nil, err
	}

	return res, nil
}

func (c *Client) execute(req *Request) (*Response, error) {
	var err error

	// Apply Request middlewares
	for _, f := range c.beforeRequest {
		if err = f(c, req); err != nil {
			return nil, err
		}
	}

	var resp []byte

	// we don't support block hash and overrides at the same time
	if req.BlockHash != zeroHash && len(req.Overrides) > 0 {
		logger.Errorf("block hash and overrides are not supported at the same time")
		return nil, ErrWrongCallParam
	}

	if req.BlockHash != zeroHash {
		resp, err = c.ethClient.CallContractAtHash(req.Context(), req.RawCallMsg, req.BlockHash)
	} else if req.Overrides != nil {
		resp, err = c.gethClient.CallContract(req.Context(), req.RawCallMsg, req.BlockNumber, &req.Overrides)
	} else {
		resp, err = c.ethClient.CallContract(req.Context(), req.RawCallMsg, req.BlockNumber)
	}
	if err != nil {
		logger.Errorf("failed to call multicall, err: %v", err)
		return nil, err
	}

	response := &Response{
		Request:     req,
		RawResponse: resp,
	}

	// Apply Response middleware
	for _, f := range c.afterResponse {
		if err = f(c, response); err != nil {
			break
		}
	}

	return response, err
}

func createClient(ec *ethclient.Client) *Client {
	c := &Client{
		ethClient:  ec,
		gethClient: gethclient.New(ec.Client()),
	}

	// default before request middlewares
	c.beforeRequest = []RequestMiddleware{
		parseRequestCallParam,
	}

	// default after response middlewares
	c.afterResponse = []ResponseMiddleware{
		parseResponse,
	}

	return c
}
