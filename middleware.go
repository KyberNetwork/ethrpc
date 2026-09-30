package ethrpc

import (
	"github.com/KyberNetwork/logger"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/KyberNetwork/ethrpc/abi"
)

func parseRequestCallParam(c *Client, req *Request) error {
	switch req.Method {
	case MethodCall:
		if len(req.Calls) != 1 {
			return ErrWrongCallParam
		}

		call := req.Calls[0]
		callData, err := abi.Pack(&call.ABI, call.Method, call.Params...)
		if err != nil {
			logger.Errorf("failed to pack api, err: %v", err)
			return err
		}

		target := common.HexToAddress(call.Target)
		msg := ethereum.CallMsg{From: req.From, To: &target, Gas: req.Gas, GasPrice: req.GasPrice, Data: callData}

		req.RawCallMsg = msg

		return nil
	case MethodAggregate:
		var multiCallParams []MultiCallParam

		for _, c := range req.Calls {
			callData, err := abi.Pack(&c.ABI, c.Method, c.Params...)
			if err != nil {
				logger.Errorf("failed to build call data for target=%s method=%s, err: %v", c.Target, c.Method, err)
				return err
			}

			multiCallParams = append(
				multiCallParams, MultiCallParam{
					Target:   common.HexToAddress(c.Target),
					CallData: callData,
				},
			)
		}

		callData, err := abi.Pack(&multicallABI, MethodAggregate, multiCallParams)
		if err != nil {
			logger.Errorf("failed to build multi call data, err: %v", err)
			return err
		}

		msg := ethereum.CallMsg{From: req.From, To: &c.multiCallContract, Gas: req.Gas, GasPrice: req.GasPrice, Data: callData}
		req.RawCallMsg = msg

		return nil
	case MethodTryAggregate:
		var multiCallParams []MultiCallParam

		for _, call := range req.Calls {
			callData, err := abi.Pack(&call.ABI, call.Method, call.Params...)
			if err != nil {
				logger.Errorf("failed to build call data for target=%s method=%s, err: %v", call.Target, call.Method, err)
				return err
			}

			multiCallParams = append(
				multiCallParams, MultiCallParam{
					Target:   common.HexToAddress(call.Target),
					CallData: callData,
				},
			)
		}

		callData, err := abi.Pack(&multicallABI, MethodTryAggregate, req.RequireSuccess, multiCallParams)
		if err != nil {
			logger.Errorf("failed to build multi call data, err: %v", err)
			return err
		}

		msg := ethereum.CallMsg{From: req.From, To: &c.multiCallContract, Gas: req.Gas, GasPrice: req.GasPrice, Data: callData}
		req.RawCallMsg = msg

		return nil
	case MethodGetCurrentBlockTimestamp:
		callData, err := abi.Pack(&multicallABI, MethodGetCurrentBlockTimestamp)
		if err != nil {
			logger.Errorf("failed to build call data, err: %v", err)
			return err
		}

		msg := ethereum.CallMsg{From: req.From, To: &c.multiCallContract, Gas: req.Gas, GasPrice: req.GasPrice, Data: callData}
		req.RawCallMsg = msg

		return nil
	case MethodTryBlockAndAggregate:
		var multiCallParams []MultiCallParam

		for _, call := range req.Calls {
			callData, err := abi.Pack(&call.ABI, call.Method, call.Params...)
			if err != nil {
				logger.Errorf("failed to build call data for target=%s method=%s, err: %v", call.Target, call.Method, err)
				return err
			}

			multiCallParams = append(
				multiCallParams, MultiCallParam{
					Target:   common.HexToAddress(call.Target),
					CallData: callData,
				},
			)
		}

		callData, err := abi.Pack(&multicallABI, MethodTryBlockAndAggregate, req.RequireSuccess, multiCallParams)
		if err != nil {
			logger.Errorf("failed to build multi call data, err: %v", err)
			return err
		}

		msg := ethereum.CallMsg{From: req.From, To: &c.multiCallContract, Gas: req.Gas, GasPrice: req.GasPrice, Data: callData}
		req.RawCallMsg = msg

		return nil
	default:
		return ErrMethodNotSupported
	}
}

func parseResponse(_ *Client, res *Response) (err error) {
	switch res.Request.Method {
	case MethodCall:
		if len(res.Request.Calls) != 1 {
			return ErrWrongCallParam
		}

		call := res.Request.Calls[0]

		if err = abi.UnpackIntoInterface(&call.ABI, call.Output[0], call.Method, res.RawResponse); err != nil {
			logger.Errorf("failed to unpack call %s, err: %v", call.Method, err)
			return err
		}

		return nil
	case MethodAggregate:
		var result AggregateResult

		err = abi.UnpackIntoInterface(&multicallABI, &result, res.Request.Method, res.RawResponse)
		if err != nil || len(result.ReturnData) != len(res.Request.Calls) {
			logger.Errorf("failed to unpack aggregate response, err: %v", err)
			return err
		}

		for i, c := range res.Request.Calls {
			// result will always be true if it can reach this far
			res.Result = append(res.Result, true)

			if err = abi.UnpackIntoInterface(&c.ABI, c.Output[0], c.Method, result.ReturnData[i]); err != nil {
				logger.Errorf("failed to unpack target=%s method=%s, err: %v", c.Target, c.Method, err)

				return NewUnPackMulticallError(err)
			}
		}
		res.BlockNumber = result.BlockNumber

		return nil
	case MethodTryAggregate:
		var result TryAggregateResult

		err = abi.UnpackIntoInterface(&multicallABI, &result, res.Request.Method, res.RawResponse)
		if err != nil || len(result) != len(res.Request.Calls) {
			logger.Errorf("failed to unpack tryAggregate response, err: %v", err)
			return err
		}

		for i, c := range res.Request.Calls {
			res.Result = append(res.Result, result[i].Success)

			if result[i].Success {
				for j := range c.UnpackABI {
					if err = abi.UnpackIntoInterface(&c.UnpackABI[j], c.Output[j], c.Method, result[i].ReturnData); err == nil {
						break
					}

					if j == len(c.UnpackABI)-1 {
						logger.Errorf("failed to unpack target=%s method=%s, err: %v", c.Target, c.Method, err)

						if res.Request.RequireSuccess {
							return NewUnPackMulticallError(err)
						}
					}
				}
			}
		}

		return nil
	case MethodGetCurrentBlockTimestamp:
		// do nothing

		return nil
	case MethodTryBlockAndAggregate:
		var result TryBlockAndAggregateResult

		err = abi.UnpackIntoInterface(&multicallABI, &result, res.Request.Method, res.RawResponse)
		if err != nil || len(result.ReturnData) != len(res.Request.Calls) {
			logger.Errorf("failed to unpack tryAggregate response, err: %v", err)
			return err
		}

		for i, c := range res.Request.Calls {
			res.Result = append(res.Result, result.ReturnData[i].Success)

			if result.ReturnData[i].Success {
				for j := range c.UnpackABI {
					if err = abi.UnpackIntoInterface(&c.UnpackABI[j], c.Output[j], c.Method, result.ReturnData[i].ReturnData); err == nil {
						break
					}

					if j == len(c.UnpackABI)-1 {
						logger.Errorf("failed to unpack target=%s method=%s, err: %v", c.Target, c.Method, err)

						if res.Request.RequireSuccess {
							return NewUnPackMulticallError(err)
						}
					}
				}
			}
		}
		res.BlockNumber = result.BlockNumber

		return nil
	default:
		return ErrMethodNotSupported
	}
}
