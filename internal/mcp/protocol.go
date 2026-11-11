package mcp

import (
	"context"
	"encoding/json"
	"time"
)

const timeout = 20 * time.Second

const wireMax = 10 * 1024 * 1024

type caller interface {
	Call(context.Context, string, any, any) error
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	Method  string          `json:"method,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
