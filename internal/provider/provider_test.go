// Copyright (c) DevOpser LLC
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"

	devopser "github.com/DevOpser-io/devopser-go"
)

// fakeRuns is a tiny run API: runs reach awaiting_approval, then finish as
// final once approved.
type fakeRuns struct {
	mu       sync.Mutex
	final    string
	approved bool
	reads    int
	kinds    []string
}

func (f *fakeRuns) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/runs/"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.kinds = append(f.kinds, body["kind"].(string))
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "run-1", "status": "queued"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/approve/"):
			f.approved = true
			json.NewEncoder(w).Encode(map[string]any{"id": "run-1", "status": "approved"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/infra/runs/run-1/"):
			f.reads++
			status := "planning"
			if f.reads > 1 {
				status = "awaiting_approval"
			}
			if f.approved {
				status = f.final
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "run-1", "status": status, "error_code": "apply_failed"})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}
}

func newRunResource(t *testing.T, f *fakeRuns) *workspaceRunResource {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	client, err := devopser.NewClient(&devopser.Config{Address: srv.URL, Token: "dvo_test"})
	if err != nil {
		t.Fatal(err)
	}
	return &workspaceRunResource{client: client, pollInterval: time.Millisecond}
}

func TestRunConfirmsItselfWhenManualConfirmIsFalse(t *testing.T) {
	f := &fakeRuns{final: "applied"}
	r := newRunResource(t, f)
	id, err := r.execute(context.Background(), 5, devopser.RunKindApply,
		&runOptionsModel{ManualConfirm: types.BoolValue(false), WaitForRun: types.BoolValue(true)})
	if err != nil || id != "run-1" || !f.approved {
		t.Fatalf("id=%q err=%v approved=%v", id, err, f.approved)
	}
}

func TestRunFailsWhenTheRunErrors(t *testing.T) {
	f := &fakeRuns{final: "errored"}
	r := newRunResource(t, f)
	_, err := r.execute(context.Background(), 5, devopser.RunKindDestroy,
		&runOptionsModel{ManualConfirm: types.BoolValue(false), WaitForRun: types.BoolValue(true)})
	if err == nil || !strings.Contains(err.Error(), "errored") {
		t.Fatalf("want an errored failure, got %v", err)
	}
	if f.kinds[0] != "destroy" {
		t.Fatalf("queued kind %v", f.kinds)
	}
}

func TestRunWithManualConfirmNeverApproves(t *testing.T) {
	f := &fakeRuns{final: "applied"}
	r := newRunResource(t, f)
	_, err := r.execute(context.Background(), 5, devopser.RunKindApply,
		&runOptionsModel{ManualConfirm: types.BoolValue(true), WaitForRun: types.BoolValue(false)})
	if err != nil || f.approved {
		t.Fatalf("err=%v approved=%v", err, f.approved)
	}
}

func TestToDynamicHandlesNestedOutputs(t *testing.T) {
	var v any
	json.Unmarshal([]byte(`{"arn":"a","n":3,"ok":true,"list":["x",1],"obj":{"k":null}}`), &v)
	d, err := toDynamic(v)
	if err != nil || d.IsNull() {
		t.Fatalf("got %v, %v", d, err)
	}
	if !strings.Contains(d.String(), `"arn"`) {
		t.Fatalf("unexpected %s", d.String())
	}
}

func TestTofuVersionMapping(t *testing.T) {
	cases := map[string]string{"1.9.4": "1.9", "1.10.2": "1.10", "1.5.7": "", "0.14.0": "", "": ""}
	for in, want := range cases {
		got := tofuVersion(types.StringValue(in))
		if (got == nil && want != "") || (got != nil && *got != want) {
			t.Errorf("%q: got %v want %q", in, got, want)
		}
	}
}

func TestCLIConfigToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".terraform.d"), 0o700)
	os.WriteFile(filepath.Join(home, ".terraform.d", "credentials.tfrc.json"),
		[]byte(`{"credentials":{"app.devopser.io":{"token":"dvo_cli"}}}`), 0o600)
	if got := cliConfigToken("app.devopser.io"); got != "dvo_cli" {
		t.Fatalf("got %q", got)
	}
	if got := cliConfigToken("other.example"); got != "" {
		t.Fatalf("got %q", got)
	}
}
