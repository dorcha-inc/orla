package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/harvard-cns/orla/internal/mappings"
	"github.com/harvard-cns/orla/internal/wire"
)

// RegisterMappingRoutes mounts the mapping-variant endpoints onto r.
// Routes:
//
//	POST   /api/v1/mappings         create or replace with name in the body
//	GET    /api/v1/mappings         list
//	GET    /api/v1/mappings/{name}  read one
//	DELETE /api/v1/mappings/{name}  remove
func RegisterMappingRoutes(r chi.Router, reg mappings.Registry, auditMW func(http.Handler) http.Handler) {
	h := &mappingHandler{reg: reg}
	r.Route("/api/v1/mappings", func(r chi.Router) {
		if auditMW != nil {
			r.Use(auditMW)
		}
		r.Post("/", h.put)
		r.Get("/", h.list)
		r.Route("/{name}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Delete("/", h.delete)
		})
	})
}

type mappingHandler struct {
	reg mappings.Registry
}

func (h *mappingHandler) put(w http.ResponseWriter, r *http.Request) {
	var req wire.PutMappingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Overrides) == 0 {
		writeErrorMsg(w, http.StatusBadRequest,
			"overrides must map at least one stage to a backend")
		return
	}
	for stageID, backend := range req.Overrides {
		if stageID == "" {
			writeErrorMsg(w, http.StatusBadRequest, "override stage id must not be empty")
			return
		}
		if backend == "" {
			writeErrorMsg(w, http.StatusBadRequest,
				"override backend for stage "+stageID+" must not be empty")
			return
		}
	}
	v, err := h.reg.Put(r.Context(), req.Name, req.Overrides)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *mappingHandler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.reg.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mappings": rows})
}

func (h *mappingHandler) get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	v, err := h.reg.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, mappings.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *mappingHandler) delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.reg.Delete(r.Context(), name); err != nil {
		if errors.Is(err, mappings.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
