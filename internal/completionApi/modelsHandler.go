package completion_api

import (
	"net/http"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

func (h *Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var models []cursor_api_sdk.Model
	err := h.Server.withAccess(ctx, func(access string) error {
		m, err := h.Server.API.ListModels(ctx, access)
		if err != nil {
			return err
		}
		models = m
		return nil
	})
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
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
