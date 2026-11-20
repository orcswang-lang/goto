// Package mcp 实现 goto-mcp 使用的 MCP stdio JSON-RPC 子集：
// 每条消息一行 JSON，stdout 只输出协议消息，日志必须走 stderr。
package mcp

import (
	"bufio"
	"bytes"
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

// maxLineBytes 是单条请求消息的行长上限。
const maxLineBytes = 1024 * 1024

var errLineTooLong = errors.New("line too long")

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
// 未知方法返回 -32601，无法解析的消息返回 -32700；
// 超过 maxLineBytes 的行会被丢弃并回报 -32700，服务继续处理后续消息。
func Serve(in io.Reader, out io.Writer, handler Handler) error {
	reader := bufio.NewReader(in)
	enc := json.NewEncoder(out)
	for {
		line, err := readLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if errors.Is(err, errLineTooLong) {
				writeResponse(enc, Err(json.RawMessage("null"), CodeParseError, "请求超过 1MB 上限，已丢弃"))
				continue
			}
			return fmt.Errorf("读取请求失败: %w", err)
		}
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
}

// readLine 读取一行（去掉尾部 \n）。
// 行长超过 maxLineBytes 时消费整行并返回 errLineTooLong；EOF 且无数据返回 io.EOF。
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		line = append(line, part...)
		if err == nil {
			return bytes.TrimSuffix(line, []byte("\n")), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(line) > maxLineBytes {
				// 丢弃该行剩余内容，保持流同步。
				for {
					if _, err2 := r.ReadSlice('\n'); err2 == nil {
						return nil, errLineTooLong
					} else if !errors.Is(err2, bufio.ErrBufferFull) {
						return nil, errLineTooLong
					}
				}
			}
			continue
		}
		if len(line) > 0 {
			return line, nil // EOF 前的最后一行（无换行）
		}
		return nil, err
	}
}

func writeResponse(enc *json.Encoder, resp Response) {
	_ = enc.Encode(resp)
}
