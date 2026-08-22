package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet_ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(100, 10) // generous limit, not what this test is checking
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q", body)
	}
}

func TestGet_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := New(100, 10)
	if _, err := c.Get(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error on 429 response")
	}
}

func TestGetWithHeaders_SendsProvidedHeaders(t *testing.T) {
	var gotKeyID, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeyID = r.Header.Get("APCA-API-KEY-ID")
		gotSecret = r.Header.Get("APCA-API-SECRET-KEY")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(100, 10)
	body, err := c.GetWithHeaders(context.Background(), srv.URL, map[string]string{
		"APCA-API-KEY-ID":     "key123",
		"APCA-API-SECRET-KEY": "secret456",
	})
	if err != nil {
		t.Fatalf("GetWithHeaders: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q", body)
	}
	if gotKeyID != "key123" {
		t.Errorf("APCA-API-KEY-ID header = %q, want %q", gotKeyID, "key123")
	}
	if gotSecret != "secret456" {
		t.Errorf("APCA-API-SECRET-KEY header = %q, want %q", gotSecret, "secret456")
	}
}
