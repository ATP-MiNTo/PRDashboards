package mw

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// MaxBodyBytes is the largest accepted request body.
const MaxBodyBytes = 64 << 10

// Validate checks the content type, size and JSON shape of write bodies, then validates the
// request against the OpenAPI document. Requests for paths missing from the document pass
// through, so the router can answer 404.
func Validate(spec *openapi3.T) (func(http.Handler) http.Handler, error) {
	router, err := legacy.NewRouter(spec)
	if err != nil {
		return nil, err
	}
	opts := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Protected(r) {
				next.ServeHTTP(w, r)
				return
			}
			if !isWrite(r) && r.ContentLength != 0 {
				WriteProblem(w, http.StatusBadRequest, CodeInvalidRequest, "this request takes no body")
				return
			}
			if isWrite(r) && r.ContentLength != 0 {
				body, ok := readJSONBody(w, r)
				if !ok {
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
			route, params, err := router.FindRoute(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if err := check(w, r, route, params, opts); err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					WriteProblem(w, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "")
					return
				}
				code := CodeInvalidRequest
				if isWrite(r) {
					code = CodeInvalidBody
				}
				WriteProblem(w, http.StatusBadRequest, code, "request does not match the API schema")
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func check(w http.ResponseWriter, r *http.Request, route *routers.Route, params map[string]string, opts *openapi3filter.Options) error {
	var saved []byte
	if r.Body != nil && r.Body != http.NoBody {
		var err error
		if saved, err = io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes)); err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(saved))
	}
	err := openapi3filter.ValidateRequest(r.Context(), &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: params,
		Route:      route,
		Options:    opts,
	})
	if saved != nil {
		r.Body = io.NopCloser(bytes.NewReader(saved))
	}
	return err
}

func readJSONBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		WriteProblem(w, http.StatusUnsupportedMediaType, CodeUnsupportedMedia, "")
		return nil, false
	}
	if r.ContentLength > MaxBodyBytes {
		WriteProblem(w, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteProblem(w, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "")
		} else {
			WriteProblem(w, http.StatusBadRequest, CodeInvalidBody, "")
		}
		return nil, false
	}
	if !singleJSONValue(body) {
		WriteProblem(w, http.StatusBadRequest, CodeInvalidBody, "body must be one JSON value")
		return nil, false
	}
	return body, true
}

func singleJSONValue(b []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(b))
	var v json.RawMessage
	if dec.Decode(&v) != nil {
		return false
	}
	_, err := dec.Token()
	return errors.Is(err, io.EOF)
}
