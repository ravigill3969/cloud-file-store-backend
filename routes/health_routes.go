package routes

import (
	"encoding/json"
	"net/http"
)

func HealthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /check", ht)
}

func ht(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
