package httpsvc

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

type fieldError struct {
	Field       string `json:"field"`
	Description string `json:"description"`
}

type errorEnvelope struct {
	Error struct {
		Code      string       `json:"code"`
		Message   string       `json:"message"`
		Status    string       `json:"status"`
		RequestID string       `json:"request_id,omitempty"`
		Fields    []fieldError `json:"fields,omitempty"`
	} `json:"error"`
}

func errorHandler(
	_ context.Context,
	_ *runtime.ServeMux,
	_ runtime.Marshaler,
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	st := status.Convert(err)

	var env errorEnvelope
	env.Error.Status = st.Code().String()
	env.Error.Code = st.Code().String()
	env.Error.Message = st.Message()
	if rid := r.Header.Get("X-Request-Id"); rid != "" {
		env.Error.RequestID = rid
	}

	for _, detail := range st.Details() {
		switch d := detail.(type) {
		case *errdetails.ErrorInfo:
			if d.GetReason() != "" {
				env.Error.Code = d.GetReason()
			}
		case *errdetails.LocalizedMessage:
			if d.GetMessage() != "" {
				env.Error.Message = d.GetMessage()
			}
		case *errdetails.BadRequest:
			for _, fv := range d.GetFieldViolations() {
				env.Error.Fields = append(env.Error.Fields, fieldError{
					Field:       fv.GetField(),
					Description: fv.GetDescription(),
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(runtime.HTTPStatusFromCode(st.Code()))
	if body, merr := json.Marshal(env); merr == nil {
		_, _ = w.Write(body)
	}
}
