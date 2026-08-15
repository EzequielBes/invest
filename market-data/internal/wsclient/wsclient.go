package wsclient

import (
	"context"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type Option func(*options)

type options struct {
	onConnect func(*websocket.Conn) error // e.g. send a subscribe message
}

// OnConnect registers a hook run immediately after each successful dial,
// used to send exchange-specific subscribe messages.
func OnConnect(f func(*websocket.Conn) error) Option {
	return func(o *options) { o.onConnect = f }
}

// Connect dials url and calls onMessage for every text/binary frame
// received, reconnecting with exponential backoff (capped at 30s) whenever
// the connection drops, until ctx is cancelled.
func Connect(ctx context.Context, url string, onMessage func([]byte), opts ...Option) error {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
		if err != nil {
			log.Printf("wsclient: dial %s failed: %v, retrying in %s", url, err, backoff)
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		if o.onConnect != nil {
			if err := o.onConnect(conn); err != nil {
				log.Printf("wsclient: onConnect for %s failed: %v", url, err)
				conn.Close()
				if !sleep(ctx, backoff) {
					return ctx.Err()
				}
				backoff = nextBackoff(backoff, maxBackoff)
				continue
			}
		}

		backoff = time.Second // reset after a successful connect
		readLoop(ctx, conn, onMessage)
		conn.Close()
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, onMessage func([]byte)) {
	for {
		if ctx.Err() != nil {
			return
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("wsclient: read error: %v", err)
			return
		}
		onMessage(msg)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}
