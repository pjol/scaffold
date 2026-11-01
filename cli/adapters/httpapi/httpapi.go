// Package httpapi exposes every scaffold operation over HTTP:
//
//	GET  /v1/ops          list operations with their input schemas
//	POST /v1/ops/{name}   run one; body is the JSON input, response the JSON output
//
// Operations write to the local filesystem, so bind to loopback only.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/pjol/scaffold/cli/core"
)

func Handler(kit *core.Kit) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ops", func(w http.ResponseWriter, r *http.Request) {
		type opInfo struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Input   any    `json:"input_schema"`
		}
		var ops []opInfo
		for _, op := range kit.Operations() {
			schema, err := op.InputSchema()
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "schema_failed", err.Error())
				return
			}
			ops = append(ops, opInfo{op.Name(), op.Summary(), schema})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ops": ops})
	})
	mux.HandleFunc("POST /v1/ops/{name}", func(w http.ResponseWriter, r *http.Request) {
		op, ok := kit.Find(r.PathValue("name"))
		if !ok {
			writeErr(w, http.StatusNotFound, "unknown_op", "no operation named "+r.PathValue("name"))
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "bad_body", err.Error())
			return
		}
		out, err := op.Invoke(r.Context(), body)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "op_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr uses the same error shape as concrete backends.
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
