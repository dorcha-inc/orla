package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/stages"
	"go.uber.org/zap"
)

// stageRequestBody is the wire shape for PUT /api/v1/stages/{id}. Pointer
// fields are nilable so PUT cannot distinguish "unset" from "zero" on
// non-pointer fields; that's deliberate — use PATCH for partial updates.
type stageRequestBody struct {
	Backend                 string            `json:"backend,omitempty"`
	SchedulingPolicy        string            `json:"scheduling_policy,omitempty"`
	RequestSchedulingPolicy string            `json:"request_scheduling_policy,omitempty"`
	Priority                *int              `json:"priority,omitempty"`
	RequestPriority         *int              `json:"request_priority,omitempty"`
	MaxConcurrency          *int              `json:"max_concurrency,omitempty"`
	ReasoningEffort         string            `json:"reasoning_effort,omitempty"`
	FlushOnComplete         bool              `json:"flush_on_complete,omitempty"`
	ReasoningHint           string            `json:"reasoning_hint,omitempty"`
	Labels                  map[string]string `json:"labels,omitempty"`
}

func (b *stageRequestBody) toStage(id string) *stages.Stage {
	labels := b.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	return &stages.Stage{
		ID:                      id,
		Backend:                 b.Backend,
		SchedulingPolicy:        b.SchedulingPolicy,
		RequestSchedulingPolicy: b.RequestSchedulingPolicy,
		Priority:                b.Priority,
		RequestPriority:         b.RequestPriority,
		MaxConcurrency:          b.MaxConcurrency,
		ReasoningEffort:         b.ReasoningEffort,
		FlushOnComplete:         b.FlushOnComplete,
		ReasoningHint:           b.ReasoningHint,
		Labels:                  labels,
	}
}

func (s *AgenticServer) handleUpsertStage(w http.ResponseWriter, r *http.Request) {
	if s.layer.StageRegistry == nil {
		http.Error(w, "stage registry not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "stage id is required in URL path", http.StatusBadRequest)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req stageRequestBody
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	stage := req.toStage(id)
	if err := s.layer.StageRegistry.Upsert(r.Context(), stage); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	loaded, err := s.layer.StageRegistry.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	zap.L().Info("Upserted stage", zap.String("id", id), zap.String("backend", loaded.Backend))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, loaded)
}

func (s *AgenticServer) handlePatchStage(w http.ResponseWriter, r *http.Request) {
	if s.layer.StageRegistry == nil {
		http.Error(w, "stage registry not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "stage id is required in URL path", http.StatusBadRequest)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	raw, err := readJSONObject(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	patch, err := buildStagePatch(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	updated, err := s.layer.StageRegistry.Patch(r.Context(), id, patch)
	if errors.Is(err, stages.ErrNotFound) {
		http.Error(w, fmt.Sprintf("stage %q not found", id), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	zap.L().Info("Patched stage", zap.String("id", id))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, updated)
}

func (s *AgenticServer) handleGetStage(w http.ResponseWriter, r *http.Request) {
	if s.layer.StageRegistry == nil {
		http.Error(w, "stage registry not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "stage id is required in URL path", http.StatusBadRequest)
		return
	}

	stage, err := s.layer.StageRegistry.Get(r.Context(), id)
	if errors.Is(err, stages.ErrNotFound) {
		http.Error(w, fmt.Sprintf("stage %q not found", id), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, stage)
}

// ListStagesResponse is the response body for GET /api/v1/stages.
type ListStagesResponse struct {
	Stages []*stages.Stage `json:"stages"`
}

func (s *AgenticServer) handleListStages(w http.ResponseWriter, r *http.Request) {
	if s.layer.StageRegistry == nil {
		http.Error(w, "stage registry not configured", http.StatusServiceUnavailable)
		return
	}
	list, err := s.layer.StageRegistry.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, ListStagesResponse{Stages: list})
}

func (s *AgenticServer) handleDeleteStage(w http.ResponseWriter, r *http.Request) {
	if s.layer.StageRegistry == nil {
		http.Error(w, "stage registry not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "stage id is required in URL path", http.StatusBadRequest)
		return
	}
	if err := s.layer.StageRegistry.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	zap.L().Info("Deleted stage", zap.String("id", id))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, map[string]bool{"success": true})
}

// readJSONObject decodes the request body into a map[string]json.RawMessage
// so callers can distinguish "field absent" from "field present with zero value".
func readJSONObject(r interface{ Read(p []byte) (int, error) }) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}
	return raw, nil
}

func buildStagePatch(raw map[string]json.RawMessage) (stages.Patch, error) {
	var p stages.Patch
	for k, v := range raw {
		switch k {
		case "backend":
			s, err := unmarshalString(k, v)
			if err != nil {
				return p, err
			}
			p.Backend = &s
		case "scheduling_policy":
			s, err := unmarshalString(k, v)
			if err != nil {
				return p, err
			}
			p.SchedulingPolicy = &s
		case "request_scheduling_policy":
			s, err := unmarshalString(k, v)
			if err != nil {
				return p, err
			}
			p.RequestSchedulingPolicy = &s
		case "priority":
			n, err := unmarshalIntPtr(k, v)
			if err != nil {
				return p, err
			}
			p.Priority = n
		case "request_priority":
			n, err := unmarshalIntPtr(k, v)
			if err != nil {
				return p, err
			}
			p.RequestPriority = n
		case "max_concurrency":
			n, err := unmarshalIntPtr(k, v)
			if err != nil {
				return p, err
			}
			p.MaxConcurrency = n
		case "reasoning_effort":
			s, err := unmarshalString(k, v)
			if err != nil {
				return p, err
			}
			p.ReasoningEffort = &s
		case "flush_on_complete":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				return p, fmt.Errorf("flush_on_complete: %w", err)
			}
			p.FlushOnComplete = &b
		case "reasoning_hint":
			s, err := unmarshalString(k, v)
			if err != nil {
				return p, err
			}
			p.ReasoningHint = &s
		case "labels":
			var m map[string]string
			if err := json.Unmarshal(v, &m); err != nil {
				return p, fmt.Errorf("labels: %w", err)
			}
			p.Labels = &m
		default:
			return p, fmt.Errorf("unknown field %q", k)
		}
	}
	return p, nil
}

func unmarshalString(field string, v json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", fmt.Errorf("%s: %w", field, err)
	}
	return s, nil
}

// unmarshalIntPtr decodes a JSON integer into *int. PATCH cannot clear an
// int field to null today — callers should PUT the whole stage to clear.
func unmarshalIntPtr(field string, v json.RawMessage) (*int, error) {
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	return &n, nil
}
