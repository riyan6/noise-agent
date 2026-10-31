package client

import (
	"context"
	"fmt"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"net/http"
	"net/url"
	"noise-agent/internal/model"
	"time"
)

func (c *Client) Stream(ctx context.Context, outgoing <-chan model.ClientMessage, incoming chan<- model.ServerMessage) error {
	endpoint, err := url.Parse(c.baseURL + "/api/v1/agent/connect")
	if err != nil {
		return err
	}
	switch endpoint.Scheme {
	case "https":
		endpoint.Scheme = "wss"
	case "http":
		endpoint.Scheme = "ws"
	default:
		return fmt.Errorf("endpoint must use HTTP or HTTPS")
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	conn, response, err := websocket.Dial(dialCtx, endpoint.String(), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}}})
	cancelDial()
	if err != nil {
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return ErrUnauthorized
		}
		return fmt.Errorf("connect to worker: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(128 << 10)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errors := make(chan error, 1)
	go func() {
		defer cancel()
		for {
			var message model.ServerMessage
			if err := wsjson.Read(streamCtx, conn, &message); err != nil {
				errors <- err
				return
			}
			select {
			case incoming <- message:
			case <-streamCtx.Done():
				return
			}
		}
	}()
	select {
	case incoming <- model.ServerMessage{Type: "connected"}:
	case <-streamCtx.Done():
		return streamCtx.Err()
	}
	for {
		select {
		case <-streamCtx.Done():
			select {
			case err := <-errors:
				return err
			default:
				return streamCtx.Err()
			}
		case message := <-outgoing:
			writeCtx, stop := context.WithTimeout(streamCtx, 5*time.Second)
			err := wsjson.Write(writeCtx, conn, message)
			stop()
			if err != nil {
				return err
			}
		}
	}
}
