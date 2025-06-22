// conn.go
package main

import (
	"io"
	"time"

	"github.com/gorilla/websocket"
)

// Conn is an interface to abstract the websocket.Conn for testing purposes.
// It includes all methods from websocket.Conn that are used in the application.
// This allows us to use a real websocket.Conn in production and a mockConn in tests.
type Conn interface {
	Close() error
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	NextWriter(messageType int) (io.WriteCloser, error)
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
	WriteJSON(v interface{}) error
}

// Ensure that the real *websocket.Conn satisfies our interface.
// This is a compile-time check that will fail if the interface is incorrect.
var _ Conn = (*websocket.Conn)(nil)
