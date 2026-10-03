package httpjson

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Header.Get("X-Test") != "1" || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("headers %v", r.Header)
			}
			w.Write([]byte(`{"a": 3}`))
		case "/bad":
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		default:
			w.Write([]byte(`not json`))
		}
	}))
	defer srv.Close()

	var out struct{ A int }
	h := http.Header{"X-Test": {"1"}}
	if err := Do(context.Background(), srv.Client(), "POST", srv.URL+"/ok", h, map[string]int{"x": 1}, &out); err != nil || out.A != 3 {
		t.Fatalf("ok path: %v, %+v", err, out)
	}

	err := Do(context.Background(), srv.Client(), "GET", srv.URL+"/bad", nil, nil, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 400 || len(se.Body) == 0 {
		t.Fatalf("want a StatusError with body, got %v", err)
	}
	if got := err.Error(); got != "unexpected HTTP status 400" {
		t.Fatalf("error text leaks the body: %q", got)
	}

	if err := Do(context.Background(), srv.Client(), "GET", srv.URL+"/other", nil, nil, &out); err == nil {
		t.Fatal("non-JSON body must fail")
	}
}
