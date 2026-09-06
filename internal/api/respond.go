package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type errorBody struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string, fields map[string]string) {
	var b errorBody
	b.Error.Code, b.Error.Message, b.Error.Fields = code, msg, fields
	writeJSON(w, status, b)
}

func badRequest(w http.ResponseWriter, msg string) { writeError(w, 400, "bad_request", msg, nil) }

func unauthorized(w http.ResponseWriter) {
	writeError(w, 401, "unauthorized", "authentication required", nil)
}

func forbidden(w http.ResponseWriter) { writeError(w, 403, "forbidden", "not allowed", nil) }

func notFound(w http.ResponseWriter) { writeError(w, 404, "not_found", "not found", nil) }

func conflict(w http.ResponseWriter, msg string) { writeError(w, 409, "conflict", msg, nil) }

func gone(w http.ResponseWriter, msg string) { writeError(w, 410, "gone", msg, nil) }

func validation(w http.ResponseWriter, f map[string]string) {
	writeError(w, 422, "validation", "invalid input", f)
}

func internal(w http.ResponseWriter) { writeError(w, 500, "internal", "internal error", nil) }

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}
