package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func HT(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Println("I am called")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
