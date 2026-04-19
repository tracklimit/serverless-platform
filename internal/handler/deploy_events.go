package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go/jetstream"

	"serverless-platform/internal/auth"
	natspkg "serverless-platform/internal/nats"
)

type DeployEventsHandler struct {
	db DeploymentStore
	js jetstream.JetStream
}

func NewDeployEventsHandler(db DeploymentStore, js jetstream.JetStream) *DeployEventsHandler {
	return &DeployEventsHandler{db: db, js: js}
}

// Stream opens an SSE connection and forwards deploy status events for a single deployment.
// GET /api/v1/deploys/{id}/events
func (h *DeployEventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// Use ResponseController so flushing works through the middleware chain
	// (tracing, metrics, RequestID, Logger, Recoverer). A direct type assertion
	// on w fails once any of those wrap the ResponseWriter.
	rc := http.NewResponseController(w)

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid deployment id")
		return
	}

	// Authorization: deployment must belong to caller's workspace. GetDeployment
	// already JOINs functions and populates dep.FunctionName, so the subject
	// needed for the NATS subscription is available without a second lookup.
	dep, err := h.db.GetDeployment(r.Context(), id)
	if err != nil || dep == nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	ws, err := h.db.GetWorkspaceBySlug(r.Context(), auth.WorkspaceSlugFromContext(r.Context()))
	if err != nil || ws == nil || ws.ID != dep.WorkspaceID {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	subject := natspkg.SubjectDeployStatus + "." + dep.FunctionName
	cons, err := h.js.CreateOrUpdateConsumer(r.Context(), natspkg.StreamName, jetstream.ConsumerConfig{
		DeliverPolicy: jetstream.DeliverAllPolicy,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckNonePolicy,
	})
	if err != nil {
		_, _ = fmt.Fprintf(w, ": nats subscribe error: %s\n\n", err.Error())
		_ = rc.Flush()
		return
	}

	iter, err := cons.Messages()
	if err != nil {
		return
	}
	defer iter.Stop()

	// Close the iterator when the client disconnects.
	go func() {
		<-r.Context().Done()
		iter.Stop()
	}()

	for {
		msg, err := iter.Next()
		if err != nil {
			return
		}

		var ev natspkg.DeployStatusEvent
		if err := json.Unmarshal(msg.Data(), &ev); err != nil {
			continue
		}
		if ev.DeploymentID != id {
			continue // not our deployment
		}

		data, _ := json.Marshal(ev)
		_, _ = fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
		_ = rc.Flush()

		if ev.Status == "RUNNING" || ev.Status == "FAILED" {
			return // terminal state
		}
	}
}
