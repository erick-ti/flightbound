package tripcomparison

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// maxBodyBytes bounds the request body. A valid request with three
// destinations and full-length names is under 2 KiB.
const maxBodyBytes = 16 << 10

var (
	errNotObject    = errors.New("the request body is not a JSON object")
	errTrailingData = errors.New("unexpected data after the JSON object")
)

type errorResponse struct {
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"field_errors,omitempty"`
}

// Handler serves trip comparison requests. Register it on its path without
// a method pattern: it answers every method itself so that all of its
// responses, including 405, are JSON.
func Handler() http.Handler {
	return http.HandlerFunc(serve)
}

func serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Message: "Use POST to request a trip comparison."})
		return
	}
	req, err := decode(w, r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Message: "The request body is too large."})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Message: "The request body must be a single JSON trip comparison object."})
		return
	}
	comparison, fieldErrors := Compare(req)
	if len(fieldErrors) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Message: "Some entries need attention.", FieldErrors: fieldErrors})
		return
	}
	writeJSON(w, http.StatusOK, comparison)
}

// decode reads exactly one JSON object with no unknown fields from a size-
// limited body. Trailing whitespace is allowed; any other trailing data is
// an error.
func decode(w http.ResponseWriter, r *http.Request) (Request, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	// Decoding into a pointer leaves it nil for a JSON null body, which is
	// not an object.
	var req *Request
	if err := dec.Decode(&req); err != nil {
		return Request{}, err
	}
	if req == nil {
		return Request{}, errNotObject
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			err = errTrailingData
		}
		return Request{}, err
	}
	return *req, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("writing trip comparison response failed", "err", err)
	}
}
