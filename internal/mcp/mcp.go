// Package mcp 实现 goto-mcp 使用的 MCP stdio JSON-RPC 子集：
// 每条消息一行 JSON，stdout 只输出协议消息，日志必须走 stderr。
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// JSON-RPC 标准错误码。
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
)

// Request 是客户端发来的 JSON-RPC 请求或通知。
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// IsNotification 判断是否为通知（无 id，不需要响应）。
func (r Request) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// Response 是服务端回复的 JSON-RPC 响应。
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError 是 JSON-RPC 协议错误。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Result 构造成功响应。
func Result(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}

// Err 构造协议错误响应。
func Err(id json.RawMessage, code int, message string) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}}
}

// Handler 处理请求与通知。
type Handler interface {
	HandleRequest(req Request) Response
	HandleNotification(method string, params json.RawMessage)
}

// Serve 从 in 逐行读取 JSON-RPC 消息并写入响应，直到 EOF。
// 未知方法返回 -32601，无法解析的消息返回 -32700。
func Serve(in io.Reader, out io.Writer, handler Handler) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		line := scanner.Bytes()
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			writeResponse(enc, Err(json.RawMessage("null"), CodeParseError, "请求不是合法 JSON"))
			continue
		}
		if req.IsNotification() {
			handler.HandleNotification(req.Method, req.Params)
			continue
		}
		resp := handler.HandleRequest(req)
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("写入响应失败: %w", err)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("读取请求失败: %w", err)
	}
	return nil
}

func writeResponse(enc *json.Encoder, resp Response) {
	_ = enc.Encode(resp)
}
