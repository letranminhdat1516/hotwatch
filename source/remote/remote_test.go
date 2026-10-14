package remote

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"hotwatch/hub"
	"hotwatch/model"
	"hotwatch/source"
	"hotwatch/transport"
)

var _ source.Source = New(nil, "")

func dialTo(srv *httptest.Server) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", srv.Listener.Addr().String())
	}
}

func serve(t *testing.T, status int, body any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInfoOK(t *testing.T) {
	srv := serve(t, 200, model.HostInfo{ProtocolVersion: model.ProtocolVersion, Hostname: "box"})
	info, err := New(dialTo(srv), "tok").Info(context.Background())
	if err != nil || info.Hostname != "box" || hub.Classify(err) != hub.EvOK {
		t.Fatalf("info %+v err %v", info, err)
	}
}

func TestUnauthorized(t *testing.T) {
	srv := serve(t, 401, model.APIError{Code: "unauthorized", Message: "no"})
	_, err := New(dialTo(srv), "bad").Snapshot(context.Background())
	if got := hub.Classify(err); got != hub.EvUnauthorized {
		t.Fatalf("classify = %v (err %v)", got, err)
	}
}

func TestUnavailableIsFail(t *testing.T) {
	srv := serve(t, 503, model.APIError{Code: "unavailable", Message: "warming up"})
	_, err := New(dialTo(srv), "t").Snapshot(context.Background())
	if got := hub.Classify(err); got != hub.EvFail {
		t.Fatalf("classify = %v (err %v)", got, err)
	}
}

func TestHostKeyErrors(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		hk := &transport.HostKeyError{Host: "h:22", Mismatch: mismatch}
		dial := func(context.Context) (net.Conn, error) { return nil, hk }
		_, err := New(dial, "t").Info(context.Background())
		want := hub.EvHostKeyUnknown
		if mismatch {
			want = hub.EvHostKeyMismatch
		}
		if got := hub.Classify(err); got != want {
			t.Errorf("mismatch=%v: classify = %v (err %v)", mismatch, got, err)
		}
	}
}
