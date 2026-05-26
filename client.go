package ethrpc

import (
	"context"
	"math/big"
	"time"

	"github.com/KyberNetwork/logger"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
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
	from              common.Address
	gas               uint64
	gasPrice          *big.Int
	preReqHook        RequestMiddleware
	retryCount        int
	retryDelay        time.Duration
	retryConditionFn  func(error) bool
}

func (c *Client) GetETHClient() *ethclient.Client {
	return c.ethClient
}

func (c *Client) SetMulticallContract(multiCallContract common.Address) *Client {
	c.multiCallContract = multiCallContract

	return c
}

func (c *Client) SetRetryCount(count int) *Client {
	if count < 0 {
		count = 0
	}
	c.retryCount = count

	return c
}

func (c *Client) SetRetryDelay(delay time.Duration) *Client {
	c.retryDelay = delay

	return c
}

func (c *Client) SetRetryCondition(fn func(error) bool) *Client {
	c.retryConditionFn = fn

	return c
}

func (c *Client) SetFrom(from common.Address) *Client {
	c.from = from

	return c
}

func (c *Client) SetGas(gas uint64) *Client {
	c.gas = gas

	return c
}

func (c *Client) SetGasPrice(gasPrice *big.Int) *Client {
	c.gasPrice = gasPrice

	return c
}

func (c *Client) SetPreReqHook(hook RequestMiddleware) *Client {
	c.preReqHook = hook

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
	return &Request{
		client:   c,
		From:     c.from,
		Gas:      c.gas,
		GasPrice: c.gasPrice,
	}
}

func (c *Client) NewRequest() *Request {
	return c.R()
}

func (c *Client) getStorageAt(ctx context.Context, account common.Address, key common.Hash, abi abi.Arguments) ([]any, error) {
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

	if c.preReqHook != nil {
		if err = c.preReqHook(c, req); err != nil {
			return nil, err
		}
	}

	// we don't support block hash and overrides at the same time
	if req.BlockHash != zeroHash && len(req.Overrides) > 0 {
		logger.Errorf("block hash and overrides are not supported at the same time")
		return nil, ErrWrongCallParam
	}

	var resp []byte
	err = c.WithRetry(req.Context(), "call multicall", func() error {
		resp, err = c.callContract(req)

		return err
	})
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

func (c *Client) WithRetry(ctx context.Context, operation string, fn func() error) error {
	attempts := 1
	if c.retryCount > 0 {
		attempts += c.retryCount
	}

	for attempt := range attempts {
		err := fn()
		if err == nil {
			return nil
		}

		if attempt < attempts-1 && c.retryConditionFn != nil && c.retryConditionFn(err) {
			logger.Warnf("failed to %s (attempt %d/%d), retrying, err: %v", operation, attempt+1, attempts, err)

			if err := sleepWithContext(ctx, c.retryDelay<<attempt); err != nil {
				return err
			}

			continue
		}

		return err
	}

	return nil
}

func (c *Client) callContract(req *Request) ([]byte, error) {
	if req.BlockHash != zeroHash {
		return c.ethClient.CallContractAtHash(req.Context(), req.RawCallMsg, req.BlockHash)
	}

	if req.Overrides != nil {
		return c.gethClient.CallContract(req.Context(), req.RawCallMsg, req.BlockNumber, &req.Overrides)
	}

	return c.ethClient.CallContract(req.Context(), req.RawCallMsg, req.BlockNumber)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
