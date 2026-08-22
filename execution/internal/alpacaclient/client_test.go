package alpacaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGet_SendsAuthHeaders(t *testing.T) {
	var gotKeyID, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeyID = r.Header.Get("APCA-API-KEY-ID")
		gotSecret = r.Header.Get("APCA-API-SECRET-KEY")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New("key123", "secret456", srv.URL)
	body, err := c.get(context.Background(), "/v2/account")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q", body)
	}
	if gotKeyID != "key123" {
		t.Errorf("APCA-API-KEY-ID = %q, want key123", gotKeyID)
	}
	if gotSecret != "secret456" {
		t.Errorf("APCA-API-SECRET-KEY = %q, want secret456", gotSecret)
	}
}

func TestGet_ErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	c := New("key", "secret", srv.URL)
	if _, err := c.get(context.Background(), "/v2/account"); err == nil {
		t.Fatal("expected error on 401 response")
	}
}
