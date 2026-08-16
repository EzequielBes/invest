package wsclient

import (
	"context"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type Option func(*options)

type options struct {
	onConnect   func(*websocket.Conn) error // e.g. send a subscribe message
	pingPayload []byte                      // exchange-specific app-level ping text frame; nil falls back to a protocol-level ping
}

// OnConnect registers a hook run immediately after each successful dial,
// used to send exchange-specific subscribe messages.
func OnConnect(f func(*websocket.Conn) error) Option {
	return func(o *options) { o.onConnect = f }
}

// PingMessage registers an app-level text payload to send as a keepalive
// ping on every pingInterval for the life of the connection, instead of a
// protocol-level WS ping frame. Some exchanges (OKX, Bybit) expect a
// documented text ping (e.g. the literal string "ping", or
// {"op":"ping"}) rather than a protocol ping frame, and reply with their own
// text "pong" rather than a protocol pong — so for those, the reply arrives
// as a normal message through onMessage (which already refreshes the read
// deadline) rather than via the protocol pong handler. If PingMessage isn't
// supplied, Connect falls back to sending protocol-level ping frames and
// relies on SetPongHandler to refresh the read deadline on the reply.
func PingMessage(payload []byte) Option {
	return func(o *options) { o.pingPayload = payload }
}

// pingInterval is how often a keepalive ping is sent — comfortably inside
// OKX's ~30s idle-connection timeout. readTimeout is how long ReadMessage
// may block before a silently-dead connection is treated as an error and
// reconnected; it must be larger than pingInterval so at least one ping
// round-trip fits before the deadline fires.
const (
	pingInterval = 20 * time.Second
	readTimeout  = 45 * time.Second
)

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

		backoff = time.Second // reset after a successful connect+subscribe
		readLoop(ctx, conn, onMessage, o.pingPayload)
		conn.Close()

		// readLoop only returns when the connection dropped (or ctx was
		// cancelled, handled by the ctx.Err() check at the top of the next
		// iteration) — always back off before redialing here too.
		if !sleep(ctx, backoff) {
			return ctx.Err()
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// readLoop reads frames until the connection errors or ctx is cancelled. It
// also guards against a silently-dead TCP connection (I1/I2): a read
// deadline is set up front and refreshed on every successful read, so a
// connection that stops producing any traffic — including keepalive
// pings/pongs — causes ReadMessage to return an error within readTimeout
// instead of blocking forever and never triggering wsclient's
// backoff/reconnect logic. A background goroutine sends a keepalive ping
// every pingInterval (an app-level text payload if pingPayload is set,
// otherwise a protocol-level ping frame) for the life of the connection.
func readLoop(ctx context.Context, conn *websocket.Conn, onMessage func([]byte), pingPayload []byte) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	conn.SetReadDeadline(time.Now().Add(readTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		return nil
	})

	go pingLoop(conn, pingPayload, done)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("wsclient: read error: %v", err)
			return
		}
		conn.SetReadDeadline(time.Now().Add(readTimeout))
		onMessage(msg)
	}
}

// pingLoop sends a keepalive ping every pingInterval until done is closed
// (readLoop returning) or a write fails (the read side will detect the dead
// connection via its own deadline shortly after). gorilla/websocket
// connections support one concurrent reader and one concurrent writer;
// pingLoop is readLoop's only writer, so no additional synchronization is
// needed between them.
func pingLoop(conn *websocket.Conn, pingPayload []byte, done <-chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			var err error
			if pingPayload != nil {
				err = conn.WriteMessage(websocket.TextMessage, pingPayload)
			} else {
				err = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
			}
			if err != nil {
				return
			}
		case <-done:
			return
		}
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
