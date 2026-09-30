# ethrpc

A go-resty style RPC client for EVM-compatible chains: chainable requests, single
`eth_call`s and Multicall batches, with an ABI codec that decodes straight into
`uint256.Int` and native Go ints.

```sh
go get github.com/KyberNetwork/ethrpc
```

## Quick start

```go
client := ethrpc.New("https://eth.llamarpc.com").
	SetMulticallContract(common.HexToAddress("0x5ba1e12693dc8f9c48aad8770482f4739beed696"))

var slot0 struct {
	SqrtPriceX96               uint256.Int
	Tick                       int32
	ObservationIndex           uint16
	ObservationCardinality     uint16
	ObservationCardinalityNext uint16
	FeeProtocol                uint8
	Unlocked                   bool
}

_, err := client.R().
	SetContext(ctx).
	AddCall(&ethrpc.Call{ABI: poolABI, Target: pool, Method: "slot0"}, []any{&slot0}).
	Call()
```

`poolABI` is a go-ethereum `abi.ABI` (e.g. from `abi.JSON`). `Target` is a hex address
string. Each call's outputs are decoded into the pointers passed to `AddCall`.

## Requests

| Method | RPC | Notes |
|---|---|---|
| `Call()` | `eth_call` to `Target` | exactly one call |
| `Aggregate()` | Multicall `aggregate` | fails if any call reverts; sets `Response.BlockNumber` |
| `TryAggregate()` | Multicall `tryAggregate` | per-call success in `Response.Result` |
| `TryBlockAndAggregate()` | Multicall `tryBlockAndAggregate` | as above, plus `Response.BlockNumber` |
| `GetCurrentBlockTimestamp()` | Multicall `getCurrentBlockTimestamp` | returns `uint64` |
| `GetStorageAt(account, key, args, v)` | `eth_getStorageAt` | decodes the slot per `args` into `v` |

Multicall methods need `SetMulticallContract`. With `SetRequireSuccess(true)`, the `try*`
methods revert if any call fails and return an error if a result can't be decoded.

```go
reserves := make([]struct {
	Reserve0, Reserve1 uint256.Int
	BlockTimestampLast uint32
}, len(pools))
req := client.R()
for i, p := range pools {
	req.AddCall(&ethrpc.Call{ABI: pairABI, Target: p, Method: "getReserves"}, []any{&reserves[i]})
}
res, err := req.TryAggregate()
// res.Result[i] reports whether call i succeeded
```

`Call.UnpackABI` lists alternative ABIs to try, in order, when decoding a result (e.g.
pools whose `getReserves` returns different shapes). Pass one output per ABI:
`AddCall(call, []any{&outV1, &outV2})`. It defaults to `Call.ABI`.

### Request options

| Setter | Effect |
|---|---|
| `SetContext(ctx)` | context for the RPC call |
| `SetBlockNumber(n)` / `SetBlockHash(h)` | call at a block (default: latest) |
| `SetFrom(addr)`, `SetGas(g)`, `SetGasPrice(p)` | `eth_call` message fields |
| `SetOverrides(map[common.Address]gethclient.OverrideAccount)` | state overrides; not combinable with a block hash |

`SetFrom`, `SetGas` and `SetGasPrice` also exist on `Client` as defaults for every
new request.

## Client options

```go
client := ethrpc.New(url). // or ethrpc.NewWithClient(ethClient)
	SetMulticallContract(multicall).
	SetRetryCount(3).                        // extra attempts after the first
	SetRetryDelay(200 * time.Millisecond).   // doubled on each retry
	SetRetryCondition(func(err error) bool { // retry only when this returns true
		return strings.Contains(err.Error(), "header not found")
	}).
	SetPreReqHook(func(c *ethrpc.Client, r *ethrpc.Request) error {
		return nil // runs after calldata is built, before the RPC is sent
	})
```

Retries apply to the contract call. `client.WithRetry(ctx, name, fn)` applies the same
policy to any other operation. `GetETHClient()` exposes the underlying `ethclient.Client`,
and the client also wraps `GetBlockNumber`, `BalanceAt`, `SuggestGasPrice` and
`EstimateGas`.

## ABI codec (`ethrpc/abi`)

All encoding and decoding goes through `github.com/KyberNetwork/ethrpc/abi`. It reuses
go-ethereum's parsed `abi.ABI` but writes values directly into the destination, without
allocating a `*big.Int` per integer or copying through `[]any`.

Supported integer types for any ABI `intN`/`uintN`:

| Go type | Decode | Encode |
|---|---|---|
| `uint256.Int`, `*uint256.Int` | any size; signed values stay two's complement | uintN: must fit N bits; intN: read as two's complement, must fit intN |
| `*big.Int` | same values as go-ethereum | must be in range |
| native `int8`..`int64`, `uint8`..`uint64`, `int`, `uint` | only if the Go type holds the whole ABI range | any value that fits |

For example, `int24` decodes into `int32` or `int64`, and `uint24` into `uint32` or `int32`.
`int24` → `uint32` (sign) and `uint160` → `uint64` (width) are rejected with an error,
never truncated. Words that don't fit the ABI type (e.g. a `uint24` above 2^24) are
rejected, including for `*big.Int` destinations.

The codec is also usable on its own, e.g. for decoding events:

```go
data, err := abi.Pack(&poolABI, "ticks", int32(-887220))

var tick struct {
	LiquidityGross uint256.Int
	LiquidityNet   uint256.Int // int128, two's complement
	// ...
}
err = abi.UnpackIntoInterface(&poolABI, &tick, "ticks", returnData)

var swap struct {
	Sender, Recipient common.Address
	Amount0, Amount1  uint256.Int
	SqrtPriceX96      uint256.Int
	Liquidity         uint256.Int
	Tick              int32
}
err = abi.UnpackLog(&poolABI, &swap, "Swap", &log) // data + indexed topics
```

Also available: `PackArgs` / `UnpackArgs` for bare `abi.Arguments`, and `ParseTopics`.
Behaviour otherwise follows go-ethereum's `accounts/abi`, including bounds checks,
error messages, struct field mapping (`abi:"name"` tags) and `interface{}` destinations.
Where it differs, it is stricter or returns an error in cases where go-ethereum panics.

## Development

```sh
go test ./...                              # the RPCTestSuite tests need network access
go test ./abi -run '^$' -bench . -benchmem # codec vs go-ethereum
go test ./abi -fuzz FuzzUnpack             # differential fuzzing against go-ethereum
```
