package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/shinya/shineflow/domain/executor"
	"github.com/shinya/shineflow/domain/workflow"
)

type fakeHTTP struct {
	req  executor.HTTPRequest
	resp executor.HTTPResponse
	err  error
}

func (f *fakeHTTP) Do(_ context.Context, req executor.HTTPRequest) (executor.HTTPResponse, error) {
	f.req = req
	return f.resp, f.err
}

func TestHTTPHappy200(t *testing.T) {
	client := &fakeHTTP{resp: executor.HTTPResponse{StatusCode: 200, Body: []byte(`{"ok":true}`)}}
	exe := httpRequestFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Services: executor.ExecServices{HTTPClient: client},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortDefault {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["status"].(int); v != 200 {
		t.Fatalf("status: %v", out.Outputs["status"])
	}
	if client.req.Method != "GET" || client.req.URL != "https://x" {
		t.Fatalf("request: %+v", client.req)
	}
}

func TestHTTP4xxFiresError(t *testing.T) {
	exe := httpRequestFactory(nil)
	out, err := exe.Execute(context.Background(), executor.ExecInput{
		Config: json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Services: executor.ExecServices{
			HTTPClient: &fakeHTTP{resp: executor.HTTPResponse{StatusCode: 404, Body: []byte(`not found`)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.FiredPort != workflow.PortError {
		t.Fatalf("port: %s", out.FiredPort)
	}
	if v, _ := out.Outputs["status"].(int); v != 404 {
		t.Fatalf("status: %v", out.Outputs["status"])
	}
}

func TestHTTPTransportErrPropagates(t *testing.T) {
	exe := httpRequestFactory(nil)
	wantErr := errors.New("connect refused")
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Services: executor.ExecServices{HTTPClient: &fakeHTTP{err: wantErr}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped %v, got %v", wantErr, err)
	}
}

func TestHTTPClientNotConfigured(t *testing.T) {
	exe := httpRequestFactory(nil)
	_, err := exe.Execute(context.Background(), executor.ExecInput{
		Config:   json.RawMessage(`{"method":"GET","url":"https://x"}`),
		Services: executor.ExecServices{HTTPClient: nil},
	})
	if !errors.Is(err, ErrPortNotConfigured) {
		t.Fatalf("expected ErrPortNotConfigured, got %v", err)
	}
}
