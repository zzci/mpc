package coordclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// statusServer is a minimal A3 stand-in. Member-auth correctness is already
// covered by the mockCoord harness for the B endpoints (Status reuses the
// exact same signRequest path); this server focuses on the A3 response and
// the api.md error envelope so the test stays fast and isolated.
func statusServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func statusClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(url, "g1", "m1", mustKey(t),
		WithRetryPolicy(RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestStatus_A3_OK(t *testing.T) {
	srv := statusServer(t, http.StatusOK,
		`{"requestId":"r1","status":"PENDING"}`)
	got, err := statusClient(t, srv.URL).Status(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.RequestID != "r1" || got.Status != "PENDING" {
		t.Fatalf("unexpected status view: %+v", got)
	}
}

func TestStatus_A3_FailReason(t *testing.T) {
	srv := statusServer(t, http.StatusOK,
		`{"requestId":"r1","status":"FAILED","fail_reason":"bad-sig"}`)
	got, err := statusClient(t, srv.URL).Status(context.Background(), "r1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Status != "FAILED" || got.FailReason != "bad-sig" {
		t.Fatalf("fail_reason not surfaced: %+v", got)
	}
}

func TestStatus_A3_LockedPassthrough(t *testing.T) {
	srv := statusServer(t, http.StatusServiceUnavailable,
		`{"error":{"code":"LOCKED","message":"store locked"}}`)
	_, err := statusClient(t, srv.URL).Status(context.Background(), "r1")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

func TestStatus_A3_NotFound(t *testing.T) {
	srv := statusServer(t, http.StatusNotFound,
		`{"error":{"code":"NOT_FOUND","message":"unknown requestId"}}`)
	_, err := statusClient(t, srv.URL).Status(context.Background(), "r1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestStatus_EmptyRequestID(t *testing.T) {
	srv := statusServer(t, http.StatusOK, `{}`)
	if _, err := statusClient(t, srv.URL).Status(context.Background(), ""); err == nil {
		t.Fatal("expected empty-requestId error")
	}
}

// TestStatus_DecodeError proves a malformed 2xx body is a typed decode error,
// not a silent zero value.
func TestStatus_DecodeError(t *testing.T) {
	srv := statusServer(t, http.StatusOK, `not-json`)
	if _, err := statusClient(t, srv.URL).Status(context.Background(), "r1"); err == nil {
		t.Fatal("expected decode error")
	}
}
