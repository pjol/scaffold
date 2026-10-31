package wallets

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rpcClient talks JSON-RPC over HTTP to the chains in WALLETS_CHAINS. Calls
// are batched (10 per request, the smallest limit common providers accept)
// and fall back to single calls when a provider refuses batches.
type rpcClient struct {
	http    *http.Client
	timeout time.Duration
}

func newRPCClient(timeout time.Duration) *rpcClient {
	return &rpcClient{http: &http.Client{Timeout: timeout}, timeout: timeout}
}

type rpcCall struct {
	Method string
	Params []any
}

type rpcResult struct {
	Value string // hex result
	Err   error
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

const rpcBatchSize = 10

var errRPCUnavailable = errors.New("rpc unavailable")

// do runs calls against url and returns one result per call, in order.
func (c *rpcClient) do(ctx context.Context, url string, calls []rpcCall) []rpcResult {
	out := make([]rpcResult, len(calls))
	for start := 0; start < len(calls); start += rpcBatchSize {
		end := min(start+rpcBatchSize, len(calls))
		c.batch(ctx, url, calls[start:end], out[start:end])
	}
	return out
}

func (c *rpcClient) batch(ctx context.Context, url string, calls []rpcCall, out []rpcResult) {
	reqs := make([]rpcRequest, len(calls))
	for i, call := range calls {
		reqs[i] = rpcRequest{JSONRPC: "2.0", ID: i + 1, Method: call.Method, Params: call.Params}
	}
	body, err := c.post(ctx, url, reqs)
	var resps []rpcResponse
	if err == nil {
		err = json.Unmarshal(body, &resps)
	}
	if err != nil {
		if ctx.Err() != nil || len(calls) == 1 {
			for i := range out {
				out[i].Err = errRPCUnavailable
			}
			return
		}
		// Some providers reject batches; retry one call at a time.
		for i := range calls {
			c.batch(ctx, url, calls[i:i+1], out[i:i+1])
		}
		return
	}
	byID := make(map[int]rpcResponse, len(resps))
	for _, r := range resps {
		byID[r.ID] = r
	}
	for i := range calls {
		r, ok := byID[i+1]
		switch {
		case !ok:
			out[i].Err = errRPCUnavailable
		case r.Error != nil:
			out[i].Err = fmt.Errorf("rpc error %d: %s", r.Error.Code, r.Error.Message)
		default:
			var s string
			if err := json.Unmarshal(r.Result, &s); err != nil {
				out[i].Err = fmt.Errorf("rpc result is not a string")
			} else {
				out[i].Value = s
			}
		}
	}
}

func (c *rpcClient) post(ctx context.Context, url string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc http %d", res.StatusCode)
	}
	return body, nil
}

// balanceOfData is the calldata for ERC-20 balanceOf(address).
func balanceOfData(owner Address) string {
	return "0x70a08231" + strings.Repeat("0", 24) + hex.EncodeToString(owner[:])
}

// eip1271Magic is the return value of isValidSignature(bytes32,bytes) for a
// valid signature.
const eip1271Magic = "1626ba7e"

// isValidSignatureData encodes isValidSignature(bytes32 hash, bytes sig).
func isValidSignatureData(hash, sig []byte) string {
	var b bytes.Buffer
	b.Write([]byte{0x16, 0x26, 0xba, 0x7e})
	b.Write(hash)
	b.Write(word(64)) // offset of the bytes argument
	b.Write(word(uint64(len(sig))))
	b.Write(sig)
	if pad := len(sig) % 32; pad != 0 {
		b.Write(make([]byte, 32-pad))
	}
	return "0x" + hex.EncodeToString(b.Bytes())
}

func word(n uint64) []byte {
	w := make([]byte, 32)
	binary.BigEndian.PutUint64(w[24:], n)
	return w
}

// verifyEIP1271 asks a smart-contract wallet whether sig is valid for hash
// (Safe, Coinbase Smart Wallet and other contract accounts). Undeployed
// counterfactual accounts (EIP-6492) are not supported.
func (c *rpcClient) verifyEIP1271(ctx context.Context, chain *Chain, account Address, hash, sig []byte) bool {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	res := c.do(ctx, chain.RPCURL, []rpcCall{{Method: "eth_call", Params: []any{
		map[string]string{"to": account.Lower(), "data": isValidSignatureData(hash, sig)}, "latest",
	}}})
	if res[0].Err != nil {
		return false
	}
	v := strings.TrimPrefix(res[0].Value, "0x")
	return len(v) >= 8 && strings.EqualFold(v[:8], eip1271Magic)
}

// ---- balances ----------------------------------------------------------------

// balanceCache keeps recent reads so a screen refresh doesn't hit the RPC
// provider for every wallet × token.
type balanceCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]cachedBalance
}

type cachedBalance struct {
	amount string
	at     time.Time
}

func newBalanceCache(ttl time.Duration) *balanceCache {
	return &balanceCache{ttl: ttl, m: map[string]cachedBalance{}}
}

func (bc *balanceCache) get(key string, now time.Time) (string, bool) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	v, ok := bc.m[key]
	if !ok || now.Sub(v.at) > bc.ttl {
		return "", false
	}
	return v.amount, true
}

func (bc *balanceCache) put(key, amount string, now time.Time) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if len(bc.m) > 10_000 {
		for k, v := range bc.m {
			if now.Sub(v.at) > bc.ttl {
				delete(bc.m, k)
			}
		}
	}
	bc.m[key] = cachedBalance{amount: amount, at: now}
}

// readBalances fills the amounts of items on one chain: cached values first,
// then one batched round trip for the rest.
func (s *Service) readBalances(ctx context.Context, chain *Chain, items []*Balance, now time.Time) {
	ctx, cancel := context.WithTimeout(ctx, s.rpc.timeout)
	defer cancel()
	var calls []rpcCall
	var pending []*Balance
	for _, b := range items {
		if v, ok := s.balances.get(b.cacheKey(), now); ok {
			b.Amount = &v
			continue
		}
		owner, _ := ParseAddress(b.Address)
		if b.TokenAddress == nil {
			calls = append(calls, rpcCall{Method: "eth_getBalance", Params: []any{owner.Lower(), "latest"}})
		} else {
			token, _ := ParseAddress(*b.TokenAddress)
			calls = append(calls, rpcCall{Method: "eth_call", Params: []any{
				map[string]string{"to": token.Lower(), "data": balanceOfData(owner)}, "latest",
			}})
		}
		pending = append(pending, b)
	}
	if len(calls) == 0 {
		return
	}
	results := s.rpc.do(ctx, chain.RPCURL, calls)
	for i, r := range results {
		b := pending[i]
		var n *big.Int
		err := r.Err
		if err == nil {
			// eth_call to an address without code returns "0x".
			n, err = hexQuantity(r.Value)
		}
		if err != nil {
			code := "invalid_response" // reverted call, no contract, malformed result
			if errors.Is(err, errRPCUnavailable) {
				code = "rpc_unavailable"
			}
			b.Error = &code
			continue
		}
		v := n.String()
		b.Amount = &v
		s.balances.put(b.cacheKey(), v, now)
	}
}
