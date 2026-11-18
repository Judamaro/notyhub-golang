package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

const maxBodyBytes = 1 << 20

// decodeJSON reads the JSON body of the request into a T. It answers 415 when the body is not JSON and a 400
// RequestValidation response when the body is missing or malformed; ok is false in both cases.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (value T, ok bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) {
		writeStatus(w, http.StatusUnsupportedMediaType)
		return value, false
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))

	var decoded *T
	err = dec.Decode(&decoded)
	switch {
	case errors.Is(err, io.EOF), err == nil && decoded == nil:
		writeBodyError(w, "The request body is missing or could not be read.")
		return value, false
	case isReadError(err):
		writeBodyError(w, "The request body is missing or could not be read.")
		return value, false
	case err != nil:
		writeBodyError(w, "The request body is not valid JSON.")
		return value, false
	}

	// Anything after the JSON value makes the body invalid.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeBodyError(w, "The request body is not valid JSON.")
		return value, false
	}

	return *decoded, true
}

func isReadError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func writeBodyError(w http.ResponseWriter, message string) {
	Fail(ModuleRequestValidation, "One or more validation errors occurred.", http.StatusBadRequest,
		validationResult(map[string][]string{"body": {message}})).write(w)
}
