package api

import "net/http"

func (rs *ingestRoutes) producerSubscriptions(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RootUUID  string `json:"root_uuid"`
		Namespace string `json:"namespace"`
		After     string `json:"after"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.service.Subscriptions(r.Context(), ingestToken(r), input.RootUUID, input.Namespace, input.After)
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
