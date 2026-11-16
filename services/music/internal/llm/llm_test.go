package llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatJSONAndEmbed(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/chat/completions":
			if calls == 1 { // 第一次 500，应重试
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": "```json\n{\"items\":[{\"i\":0}]}\n```"}}},
				"usage":   map[string]any{"total_tokens": 12},
			})
		case "/embeddings":
			var body struct {
				Input      []string `json:"input"`
				Dimensions int      `json:"dimensions"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			data := []any{}
			for i := range body.Input {
				data = append(data, map[string]any{"index": i, "embedding": []float64{3, 4, 0, 0, 9}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]any{"total_tokens": 3}})
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k", "m", "e", 4, 1)
	var out struct {
		Items []struct{ I int } `json:"items"`
	}
	u, err := c.ChatJSON(context.Background(), "s", "u", &out)
	if err != nil || len(out.Items) != 1 || u.TotalTokens != 12 {
		t.Fatalf("chat: %v %+v %+v", err, out, u)
	}
	vecs, _, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil || len(vecs) != 2 || len(vecs[0]) != 4 || math.Abs(float64(vecs[0][0])-0.6) > 1e-6 {
		t.Fatalf("embed: %v %v", err, vecs)
	}
	if New("", "", "", "", 0, 0).Enabled() {
		t.Fatal("empty base url must be disabled")
	}
}
