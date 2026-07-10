package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/harvard-cns/orla/internal/stages"
	"github.com/harvard-cns/orla/internal/wire"
)

// RegisterStageRoutes mounts the stage endpoints onto r. Routes:
//
//	GET    /api/v1/stages
//	GET    /api/v1/stages/{id}
//	PUT    /api/v1/stages/{id}   (full replace)
//	PATCH  /api/v1/stages/{id}   (partial update)
//	DELETE /api/v1/stages/{id}
//
// Auto-create on first sighting is *not* exposed via REST, it happens
// implicitly inside the proxy when a request arrives for an unknown
// stage. The mapper is expected to use PUT/PATCH for explicit
// configuration.
func RegisterStageRoutes(r chi.Router, reg stages.Registry) {
	h := &stageHandler{reg: reg}
	r.Route("/api/v1/stages", func(r chi.Router) {
		r.Get("/", h.list)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Put("/", h.put)
			r.Patch("/", h.patch)
			r.Delete("/", h.delete)
		})
	})
}

type stageHandler struct {
	reg stages.Registry
}

const maxPromptLen = 10 << 20 // 10 MB

func (h *stageHandler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.reg.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stages": rows})
}

func (h *stageHandler) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorMsg(w, http.StatusBadRequest, "id is required")
		return
	}
	s, err := h.reg.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, stages.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *stageHandler) put(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorMsg(w, http.StatusBadRequest, "id is required")
		return
	}
	var body wire.MapStageRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Prompt) > maxPromptLen {
		writeErrorMsg(w, http.StatusBadRequest,
			fmt.Sprintf("prompt exceeds max length %d, got %d", maxPromptLen, len(body.Prompt)))
		return
	}
	s, err := h.reg.Replace(r.Context(), &stages.Stage{
		ID:              id,
		Backend:         body.Backend,
		ReasoningEffort: body.ReasoningEffort,
		Prompt:          body.Prompt,
		Labels:          body.Labels,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *stageHandler) patch(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorMsg(w, http.StatusBadRequest, "id is required")
		return
	}
	var body stages.PatchRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Prompt != nil && len(*body.Prompt) > maxPromptLen {
		writeErrorMsg(w, http.StatusBadRequest,
			fmt.Sprintf("prompt exceeds max length %d, got %d", maxPromptLen, len(*body.Prompt)))
		return
	}
	s, err := h.reg.Patch(r.Context(), id, body)
	if err != nil {
		if errors.Is(err, stages.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *stageHandler) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorMsg(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.reg.Delete(r.Context(), id); err != nil {
		if errors.Is(err, stages.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
