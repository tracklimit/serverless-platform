package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"serverless-platform/internal/db"
	"serverless-platform/internal/model"
	natspkg "serverless-platform/internal/nats"
)

// --- mocks ---

type mockStore struct {
	mu         sync.Mutex
	statuses   map[int64]string
	errors     map[int64]string
	active     []*db.Deployment
	functions  map[string]*db.Function
	workspaces map[int64]*db.Workspace
}

func newMockStore() *mockStore {
	return &mockStore{
		statuses:   make(map[int64]string),
		errors:     make(map[int64]string),
		functions:  make(map[string]*db.Function),
		workspaces: make(map[int64]*db.Workspace),
	}
}

func (m *mockStore) UpdateDeploymentStatus(_ context.Context, id int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[id] = status
	return nil
}

func (m *mockStore) UpdateDeploymentError(_ context.Context, id int64, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[id] = errMsg
	m.statuses[id] = "failed"
	return nil
}

func (m *mockStore) ListActiveDeployments(_ context.Context) ([]*db.Deployment, error) {
	return m.active, nil
}

func (m *mockStore) GetFunction(_ context.Context, _ int64, name string) (*db.Function, error) {
	fn, ok := m.functions[name]
	if !ok {
		return nil, nil
	}
	return fn, nil
}

func (m *mockStore) GetWorkspaceByID(_ context.Context, id int64) (*db.Workspace, error) {
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, nil
	}
	return ws, nil
}

func (m *mockStore) getStatus(id int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statuses[id]
}

func (m *mockStore) getError(id int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.errors[id]
}

type mockDeployer struct {
	mu       sync.Mutex
	calls    int
	failN    int // fail the first N calls
	deployFn func(ctx context.Context, namespace string, fn *model.Function) (string, error)
}

func (m *mockDeployer) EnsureNamespace(_ context.Context, _ string) error     { return nil }
func (m *mockDeployer) EnsureNamespaceRBAC(_ context.Context, _ string) error { return nil }

func (m *mockDeployer) Deploy(ctx context.Context, namespace string, fn *model.Function) (string, error) {
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.mu.Unlock()

	if call <= m.failN {
		return "", errors.New("simulated deploy failure")
	}
	if m.deployFn != nil {
		return m.deployFn(ctx, namespace, fn)
	}
	return "https://" + fn.Name + "." + namespace + ".example.com", nil
}

type mockPublisher struct {
	mu     sync.Mutex
	events []*natspkg.DeployStatusEvent
}

func (m *mockPublisher) PublishDeployStatus(_ context.Context, event *natspkg.DeployStatusEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockPublisher) lastStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.events) == 0 {
		return ""
	}
	return m.events[len(m.events)-1].Status
}

// --- test helpers ---

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

func publishDeploy(t *testing.T, js jetstream.JetStream, req *natspkg.DeployRequest) {
	t.Helper()
	data, err := json.Marshal(req)
	require.NoError(t, err)
	_, err = js.Publish(context.Background(), natspkg.SubjectDeployRequest, data)
	require.NoError(t, err)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- tests ---

func TestHappyPath(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ms := newMockStore()
	md := &mockDeployer{}
	mp := &mockPublisher{}

	w := &DeployWorker{
		db:        ms,
		deployer:  md,
		nats:      client,
		publisher: mp,
		logger:    testLogger(),
	}

	stream, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       natspkg.ConsumerDeploy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: natspkg.SubjectDeployRequest,
		MaxDeliver:    4,
		AckWait:       30 * time.Second,
	})
	require.NoError(t, err)
	w.consumer = consumer

	req := &natspkg.DeployRequest{
		DeploymentID: 1,
		WorkspaceID:  1,
		FunctionName: "hello",
		Namespace:    "fn-test",
		Runtime:      "python",
		DeployType:   "managed",
		Code:         "def handler(req): return 'hello'",
	}
	publishDeploy(t, client.JetStream(), req)

	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	require.NoError(t, err)
	w.handleMessage(ctx, msg)

	assert.Equal(t, string(model.DeployStatusRunning), ms.getStatus(1))
	assert.Equal(t, string(model.DeployStatusRunning), mp.lastStatus())
	assert.Empty(t, ms.getError(1))
}

func TestDeployFailure_Retry(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ms := newMockStore()
	md := &mockDeployer{failN: 1} // fail first call
	mp := &mockPublisher{}

	w := &DeployWorker{
		db:        ms,
		deployer:  md,
		nats:      client,
		publisher: mp,
		logger:    testLogger(),
	}

	stream, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       natspkg.ConsumerDeploy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: natspkg.SubjectDeployRequest,
		MaxDeliver:    4,
		AckWait:       2 * time.Second,
	})
	require.NoError(t, err)
	w.consumer = consumer

	publishDeploy(t, client.JetStream(), &natspkg.DeployRequest{
		DeploymentID: 2,
		FunctionName: "retry-fn",
		Namespace:    "fn-test",
	})

	// First delivery — should fail and nak
	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	require.NoError(t, err)
	w.handleMessage(ctx, msg)

	// Status should be deploying (failed mid-deploy, not yet marked failed)
	assert.Equal(t, string(model.DeployStatusDeploying), ms.getStatus(2))

	// Second delivery — should succeed
	msg2, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	require.NoError(t, err)
	w.handleMessage(ctx, msg2)

	assert.Equal(t, string(model.DeployStatusRunning), ms.getStatus(2))
}

func TestMaxRetriesExhausted(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ms := newMockStore()
	md := &mockDeployer{failN: 100} // always fail
	mp := &mockPublisher{}

	w := &DeployWorker{
		db:        ms,
		deployer:  md,
		nats:      client,
		publisher: mp,
		logger:    testLogger(),
	}

	stream, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       natspkg.ConsumerDeploy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: natspkg.SubjectDeployRequest,
		MaxDeliver:    4,
		AckWait:       1 * time.Second,
	})
	require.NoError(t, err)
	w.consumer = consumer

	publishDeploy(t, client.JetStream(), &natspkg.DeployRequest{
		DeploymentID: 3,
		FunctionName: "fail-fn",
		Namespace:    "fn-test",
	})

	// Process all deliveries until max retries
	for i := 0; i < 4; i++ {
		msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
		require.NoError(t, err)
		w.handleMessage(ctx, msg)
	}

	assert.Equal(t, "failed", ms.getStatus(3))
	assert.Contains(t, ms.getError(3), "simulated deploy failure")
	assert.Equal(t, string(model.DeployStatusFailed), mp.lastStatus())
}

func TestInvalidMessage(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ms := newMockStore()
	md := &mockDeployer{}
	mp := &mockPublisher{}

	w := &DeployWorker{
		db:        ms,
		deployer:  md,
		nats:      client,
		publisher: mp,
		logger:    testLogger(),
	}

	stream, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "invalid-test",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: natspkg.SubjectDeployRequest,
		MaxDeliver:    4,
	})
	require.NoError(t, err)
	w.consumer = consumer

	// Publish garbage bytes
	_, err = client.JetStream().Publish(ctx, natspkg.SubjectDeployRequest, []byte("not json"))
	require.NoError(t, err)

	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	require.NoError(t, err)
	w.handleMessage(ctx, msg)

	// Should not have any status updates (message was terminated, not processed)
	assert.Empty(t, ms.statuses)
}

func TestRecoverySweep(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx := context.Background()

	ms := newMockStore()
	ms.workspaces[1] = &db.Workspace{ID: 1, Slug: "test-ws"}
	ms.functions["recover-fn"] = &db.Function{
		ID:         5,
		Name:       "recover-fn",
		Runtime:    "python",
		DeployType: "managed",
		Code:       "def handler(req): return 'hello'",
	}
	ms.active = []*db.Deployment{
		{
			ID:           99,
			FunctionID:   5,
			FunctionName: "recover-fn",
			WorkspaceID:  1,
			Status:       "deploying",
		},
	}

	md := &mockDeployer{}
	mp := &mockPublisher{}

	w := &DeployWorker{
		db:        ms,
		deployer:  md,
		nats:      client,
		publisher: mp,
		logger:    testLogger(),
	}

	err := w.recoverActiveDeployments(ctx)
	require.NoError(t, err)

	assert.Equal(t, string(model.DeployStatusRunning), ms.getStatus(99))
	assert.Empty(t, ms.getError(99))
}

func TestConcurrentDeploys(t *testing.T) {
	srv := startTestServer(t)
	client := testClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ms := newMockStore()
	md := &mockDeployer{}
	mp := &mockPublisher{}

	w := &DeployWorker{
		db:        ms,
		deployer:  md,
		nats:      client,
		publisher: mp,
		logger:    testLogger(),
	}

	stream, err := client.EnsureStream(ctx)
	require.NoError(t, err)

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       natspkg.ConsumerDeploy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: natspkg.SubjectDeployRequest,
		MaxDeliver:    4,
	})
	require.NoError(t, err)
	w.consumer = consumer

	publishDeploy(t, client.JetStream(), &natspkg.DeployRequest{
		DeploymentID: 10, FunctionName: "fn-a", Namespace: "fn-test",
	})
	publishDeploy(t, client.JetStream(), &natspkg.DeployRequest{
		DeploymentID: 11, FunctionName: "fn-b", Namespace: "fn-test",
	})

	for i := 0; i < 2; i++ {
		msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
		require.NoError(t, err)
		w.handleMessage(ctx, msg)
	}

	assert.Equal(t, string(model.DeployStatusRunning), ms.getStatus(10))
	assert.Equal(t, string(model.DeployStatusRunning), ms.getStatus(11))
}
