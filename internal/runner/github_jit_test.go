package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-github/v67/github"
)

func TestGitHubDriverJITResolvesGroupAndSetsUniqueRunner(t *testing.T) {
	var names []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "runner-groups") {
			_ = json.NewEncoder(w).Encode(map[string]any{"runner_groups": []any{map[string]any{"id": 42, "name": "omni-runner"}}})
			return
		}
		var req struct {
			Name          string   `json:"name"`
			RunnerGroupID int64    `json:"runner_group_id"`
			Labels        []string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		names = append(names, req.Name)
		if req.RunnerGroupID != 42 || len(req.Labels) != 1 || req.Labels[0] != "self-hosted" {
			t.Errorf("request = %#v", req)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"encoded_jit_config":"opaque","runner":{"name":"server-name"}}`))
	}))
	defer srv.Close()
	client := github.NewClient(http.DefaultClient)
	base, _ := client.BaseURL.Parse(srv.URL + "/")
	client.BaseURL = base
	d, err := newGitHubDriverWithClient(client, "org:syscode-labs", "omni-runner", nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := d.GetJITConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.GetJITConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.EncodedConfig != "opaque" || second.EncodedConfig != "opaque" || len(names) != 2 || names[0] == names[1] {
		t.Fatalf("configs/names = %#v, %#v, %#v", first, second, names)
	}
}
