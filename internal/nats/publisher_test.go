package nats_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsclient "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	natspkg "serverless-platform/internal/nats"
)

func startTestServer(t *testing.T) *server.Server {
	t.Helper()
	opts := &server.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	}
	srv, err := server.NewServer(opts)
	require.NoError(t, err)
	srv.Start()
	t.Cleanup(srv.Shutdown)

	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready")
	}
	return srv
}

func testClient(t *testing.T, srv *server.Server) *natspkg.Client {
	t.Helper()
	client, err := natspkg.NewClient(srv.ClientURL(), "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestPublishDeployRequest(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx := context.Background()

	_, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	pub := natspkg.NewPublisher(client)

	req := &natspkg.DeployRequest{
		DeploymentID: 42,
		WorkspaceID:  1,
		FunctionName: "hello-world",
		Namespace:    "fn-test",
		Runtime:      "python",
		DeployType:   "managed",
		Code:         "def handler(req): return 'hello'",
		Public:       true,
	}
	err = pub.PublishDeployRequest(ctx, req)
	require.NoError(t, err)

	// Subscribe and consume the message
	js := client.JetStream()
	consumer, err := js.CreateConsumer(ctx, natspkg.StreamName, jetstream.ConsumerConfig{
		FilterSubject: natspkg.SubjectDeployRequest,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msg, err := consumer.Next(jetstream.FetchMaxWait(2 * time.Second))
	require.NoError(t, err)

	var got natspkg.DeployRequest
	require.NoError(t, json.Unmarshal(msg.Data(), &got))

	assert.Equal(t, req.DeploymentID, got.DeploymentID)
	assert.Equal(t, req.FunctionName, got.FunctionName)
	assert.Equal(t, req.Namespace, got.Namespace)
	assert.Equal(t, req.Runtime, got.Runtime)
	assert.Equal(t, req.Code, got.Code)
	assert.Equal(t, req.Public, got.Public)
}

func TestPublishDeployStatus(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx := context.Background()

	_, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	pub := natspkg.NewPublisher(client)

	status := &natspkg.DeployStatusEvent{
		DeploymentID: 42,
		FunctionName: "hello-world",
		Status:       "running",
		URL:          "https://hello-world.fn-test.example.com",
	}
	err = pub.PublishDeployStatus(ctx, status)
	require.NoError(t, err)

	// Subscribe to the status subject
	conn, err := natsclient.Connect(srv.ClientURL())
	require.NoError(t, err)
	defer conn.Close()

	js, err := jetstream.New(conn)
	require.NoError(t, err)

	consumer, err := js.CreateConsumer(ctx, natspkg.StreamName, jetstream.ConsumerConfig{
		FilterSubject: natspkg.SubjectDeployStatus + ".>",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msg, err := consumer.Next(jetstream.FetchMaxWait(2 * time.Second))
	require.NoError(t, err)

	var got natspkg.DeployStatusEvent
	require.NoError(t, json.Unmarshal(msg.Data(), &got))

	assert.Equal(t, status.DeploymentID, got.DeploymentID)
	assert.Equal(t, status.FunctionName, got.FunctionName)
	assert.Equal(t, status.Status, got.Status)
	assert.Equal(t, status.URL, got.URL)
}

func TestStreamPersistence(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx := context.Background()

	_, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	pub := natspkg.NewPublisher(client)

	err = pub.PublishDeployRequest(ctx, &natspkg.DeployRequest{
		DeploymentID: 1,
		FunctionName: "persist-test",
	})
	require.NoError(t, err)

	// Create a first consumer, consume the message, and nak it (simulating restart)
	js := client.JetStream()
	c1, err := js.CreateConsumer(ctx, natspkg.StreamName, jetstream.ConsumerConfig{
		Durable:       "persist-test",
		FilterSubject: natspkg.SubjectDeployRequest,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msg, err := c1.Next(jetstream.FetchMaxWait(2 * time.Second))
	require.NoError(t, err)
	_ = msg.Nak() // simulate not processing

	// Re-create consumer (simulating worker restart) — message should still be available
	c2, err := js.CreateOrUpdateConsumer(ctx, natspkg.StreamName, jetstream.ConsumerConfig{
		Durable:       "persist-test",
		FilterSubject: natspkg.SubjectDeployRequest,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	require.NoError(t, err)

	msg2, err := c2.Next(jetstream.FetchMaxWait(2 * time.Second))
	require.NoError(t, err)

	var got natspkg.DeployRequest
	require.NoError(t, json.Unmarshal(msg2.Data(), &got))
	assert.Equal(t, int64(1), got.DeploymentID)
	assert.Equal(t, "persist-test", got.FunctionName)
}
