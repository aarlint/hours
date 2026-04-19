package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// apiError wraps a status code + error message
type apiError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

func (e *apiError) Error() string { return e.Message }

func newAPIError(status int, format string, args ...interface{}) *apiError {
	return &apiError{Status: status, Message: fmt.Sprintf(format, args...)}
}

// jsonHandler wraps a function that returns (data, err) and writes JSON.
func jsonHandler(fn func(w http.ResponseWriter, r *http.Request) (interface{}, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data, err := fn(w, r)
		if err != nil {
			var apiErr *apiError
			if errors.As(err, &apiErr) {
				writeError(w, apiErr.Status, err)
				return
			}
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if data == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := json.NewEncoder(w).Encode(data); err != nil {
			// already wrote headers
			return
		}
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decodeBody(r *http.Request, dst interface{}) error {
	if r.Body == nil {
		return fmt.Errorf("empty request body")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func pathInt(r *http.Request, key string) (int, error) {
	raw := r.PathValue(key)
	if raw == "" {
		return 0, fmt.Errorf("missing path parameter: %s", key)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %s", key, raw)
	}
	return n, nil
}
