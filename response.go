package ethrpc

import "math/big"

type Response struct {
	Request     *Request
	BlockNumber *big.Int
	RawResponse []byte
	// Result is an array that contains response result for all calls in the request
	Result []bool
}
