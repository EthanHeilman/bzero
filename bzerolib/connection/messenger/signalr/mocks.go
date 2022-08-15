package signalr

import "net/http"

func handleNegotiate(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
