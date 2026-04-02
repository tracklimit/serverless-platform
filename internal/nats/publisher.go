package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

type Publisher struct {
	js jetstream.JetStream
}

func NewPublisher(client *Client) *Publisher {
	return &Publisher{js: client.JetStream()}
}

func (p *Publisher) PublishDeployRequest(ctx context.Context, req *DeployRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal deploy request: %w", err)
	}

	_, err = p.js.Publish(ctx, SubjectDeployRequest, data)
	if err != nil {
		return fmt.Errorf("publish deploy request: %w", err)
	}
	return nil
}

func (p *Publisher) PublishDeployStatus(ctx context.Context, status *DeployStatusEvent) error {
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal deploy status: %w", err)
	}

	subject := fmt.Sprintf("%s.%s", SubjectDeployStatus, status.FunctionName)
	_, err = p.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("publish deploy status: %w", err)
	}
	return nil
}
