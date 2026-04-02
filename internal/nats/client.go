package nats

import (
	"context"
	"fmt"
	"time"

	natsclient "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamName = "DEPLOYS"

	SubjectDeployRequest = "deploy.request"
	SubjectDeployStatus  = "deploy.status"

	ConsumerDeploy = "deploy-worker"
)

type Client struct {
	conn *natsclient.Conn
	js   jetstream.JetStream
}

func NewClient(url, token string) (*Client, error) {
	opts := []natsclient.Option{
		natsclient.Name("serverless-platform"),
		natsclient.ReconnectWait(2 * time.Second),
		natsclient.MaxReconnects(-1),
	}
	if token != "" {
		opts = append(opts, natsclient.Token(token))
	}

	conn, err := natsclient.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}

	return &Client{conn: conn, js: js}, nil
}

func (c *Client) EnsureStream(ctx context.Context) (jetstream.Stream, error) {
	cfg := jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{"deploy.>"},
		Retention: jetstream.LimitsPolicy,
		MaxMsgs:   10_000,
		MaxAge:    7 * 24 * time.Hour,
		Storage:   jetstream.FileStorage,
		Replicas:  1,
	}

	stream, err := c.js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}
	return stream, nil
}

func (c *Client) JetStream() jetstream.JetStream { return c.js }

func (c *Client) Close() error {
	c.conn.Close()
	return nil
}
