// Package api exposes the HTTP API and the WebSocket endpoint. Every HTTP response, successful or not, is a
// PetitionResponse whose statusCode is also the status code of the response.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Judamaro/notyhub-golang/internal/domain"
)

// Modules reported in PetitionResponse.Module.
const (
	ModuleNotificationService    = "NotificationService"
	ModuleConnectionService      = "ConnectionService"
	ModuleTokenService           = "TokenService"
	ModuleRequestValidation      = "RequestValidation"
	ModuleGlobalExceptionHandler = "GlobalExceptionHandler"
	ModuleHealthCheck            = "HealthCheck"
	ModuleAPI                    = "Api"
)

// PetitionResponse is the envelope of every HTTP response.
type PetitionResponse struct {
	// Success tells whether the petition was processed successfully.
	Success bool `json:"success"`
	// Message describes how the petition was processed.
	Message string `json:"message"`
	// Module is the module or service that generated Message.
	Module string `json:"module"`
	// Result is the returned information, or null. On validation failures it holds the errors grouped by field.
	Result any `json:"result"`
	// StatusCode is the HTTP status code of the response.
	StatusCode int `json:"statusCode"`
}

// Ok creates a successful petition response.
func Ok(module, message string, result any, statusCode int) PetitionResponse {
	return PetitionResponse{Success: true, Message: message, Module: module, Result: result, StatusCode: statusCode}
}

// Fail creates a failed petition response.
func Fail(module, message string, statusCode int, result any) PetitionResponse {
	return PetitionResponse{Success: false, Message: message, Module: module, Result: result, StatusCode: statusCode}
}

// write sends the response as JSON with its status code.
func (p PetitionResponse) write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(p.StatusCode)
	_ = json.NewEncoder(w).Encode(p)
}

// validationResult is the result of a validation failure: the errors grouped by field.
func validationResult(fields map[string][]string) map[string]any {
	return map[string]any{"errors": fields}
}

// statusFor maps a business error type to an HTTP status code.
func statusFor(t domain.ErrorType) int {
	switch t {
	case domain.ErrorTypeValidation:
		return http.StatusBadRequest
	case domain.ErrorTypeNotFound:
		return http.StatusNotFound
	case domain.ErrorTypeConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// writeError answers a failed operation. Business errors keep their module and description; validation errors
// return their fields as the result. Any other error is unexpected and hidden behind a generic 500.
func writeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, module string, err error) {
	e, ok := domain.AsError(err)
	if !ok {
		logger.ErrorContext(r.Context(), "Unhandled error processing request", "method", r.Method, "path", r.URL.Path, "error", err)
		Fail(ModuleGlobalExceptionHandler, "An unexpected error occurred.", http.StatusInternalServerError, nil).write(w)
		return
	}

	var result any
	if e.Fields != nil {
		result = validationResult(e.Fields)
	}
	Fail(module, e.Description, statusFor(e.Type), result).write(w)
}

// writeStatus answers a status produced by the HTTP pipeline (unknown route, method, media type, authentication).
func writeStatus(w http.ResponseWriter, statusCode int) {
	message := "The request could not be processed."
	if text := http.StatusText(statusCode); text != "" {
		message = text + "."
	}
	Fail(ModuleAPI, message, statusCode, nil).write(w)
}
