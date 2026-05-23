package webhook

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPostSendsJSONBody(t *testing.T) {
	var gotBody []byte
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = buf
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newClient(5 * time.Second)
	if err := post(c, srv.URL, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("post: %v", err)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if string(gotBody) != `{"a":1}` {
		t.Errorf("body = %q", gotBody)
	}
}

func TestPostNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	if err := post(newClient(5*time.Second), srv.URL, []byte(`{}`)); err == nil {
		t.Fatal("expected error on 500")
	}
}
