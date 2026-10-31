// Package lsp implements the language server for .goi design files.
//
// It speaks the Language Server Protocol over stdio: JSON-RPC messages framed
// with a Content-Length header. Only the parts a design file needs are
// implemented — diagnostics, completion, hover, symbols, folding and
// go-to-definition for imports — and the code is organised so the transport
// (this file), the document store (server.go) and the language logic
// (features.go) stay separate.
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// message is one JSON-RPC message, request, notification or response.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the error member of a failed response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error codes the protocol reserves.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// readMessage reads one framed message. A zero-length Content-Length is
// treated as end of stream so a client that disconnects mid-frame does not
// wedge the loop.
func readMessage(r *bufio.Reader) (*message, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" {
				return nil, io.EOF
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
			length = n
		}
	}
	if length <= 0 {
		return nil, io.EOF
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("bad message body: %w", err)
	}
	return &m, nil
}

// writeMessage frames and writes one message.
func writeMessage(w io.Writer, m *message) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
