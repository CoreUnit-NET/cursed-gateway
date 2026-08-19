package completion_api

import (
	"net/http"
)

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.Server.ListModels(r.Context())
	if err != nil {
		h.Server.writeUpstreamError(w, r, err)
		return
	}

	out := modelListResponse{Object: "list", Data: make([]modelObject, 0, len(models))}
	for _, m := range models {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out.Data = append(out.Data, modelObject{
			ID:      m.ID,
			Object:  "model",
			Created: 0,
			OwnedBy: "cursor",
			Name:    name,
			Root:    m.ID,
			Parent:  nil,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
