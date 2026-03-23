package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type pullImageReq struct {
	Reference string `json:"reference"` // Docker image reference.
}

func (s *Server) pullImage(w http.ResponseWriter, r *http.Request) {
	var req pullImageReq
	if err := decodeJSON(r, &req); err != nil || req.Reference == "" {
		writeError(w, http.StatusBadRequest, "reference required (e.g., 'ubuntu:22.04')")
		return
	}

	img, err := s.imageManager.Pull(r.Context(), req.Reference)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, img)
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	imgs := s.imageManager.List()
	writeJSON(w, http.StatusOK, map[string]any{"images": imgs, "total": len(imgs)})
}

func (s *Server) getImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	img, ok := s.imageManager.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}
	writeJSON(w, http.StatusOK, img)
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.imageManager.Delete(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
