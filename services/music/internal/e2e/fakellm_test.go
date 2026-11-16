package e2e

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestFakeLLMServe 本机端到端用的假大模型（OpenAI 兼容）：MUSIC_FAKE_LLM_ADDR=127.0.0.1:18199 时常驻服务，直到进程被杀。
//
//	打标签：「二期歌曲」00–29 → 流行 / 治愈，30–59 → 摇滚 / 活力；歌手知识：同组相邻歌手；曲库扩展：固定 10 首曲库外 + 2 首曲库内。
//	语义向量：含「流行」的文本偏第一维，其余偏第二维，加一点按文本哈希的扰动。
func TestFakeLLMServe(t *testing.T) {
	addr := os.Getenv("MUSIC_FAKE_LLM_ADDR")
	if addr == "" {
		t.Skip("MUSIC_FAKE_LLM_ADDR 未设置")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		system, user := req.Messages[0].Content, req.Messages[1].Content
		var content any
		switch {
		case strings.Contains(system, "打标签"):
			var in []struct {
				I     int    `json:"i"`
				Title string `json:"title"`
			}
			_ = json.Unmarshal([]byte(user), &in)
			items := []any{}
			for _, x := range in {
				n := 0
				fmt.Sscanf(strings.TrimPrefix(x.Title, "三期歌曲"), "%d", &n)
				g, m := "pop", "healing"
				if n >= 30 {
					g, m = "rock", "energetic"
				}
				items = append(items, map[string]any{"i": x.I, "genres": []string{g}, "moods": []string{m}, "scenes": []string{"relax"},
					"era": "2010s", "energy": 0.5, "vocal": "male", "flags": []string{}, "confidence": 0.9})
			}
			content = map[string]any{"items": items}
		case strings.Contains(system, "最相近的歌手"):
			var in []struct {
				I      int    `json:"i"`
				Artist string `json:"artist"`
			}
			_ = json.Unmarshal([]byte(user), &in)
			items := []any{}
			for _, x := range in {
				n := 0
				fmt.Sscanf(strings.TrimPrefix(x.Artist, "三期歌手"), "%d", &n)
				base := n / 6 * 6
				var sim []string
				for k := base; k < base+6; k++ {
					if k != n {
						sim = append(sim, fmt.Sprintf("三期歌手%d", k))
					}
				}
				items = append(items, map[string]any{"i": x.I, "similar": sim, "genres": []string{"pop"}, "lang": "zh"})
			}
			content = map[string]any{"items": items}
		default: // 曲库扩展
			items := []any{map[string]string{"title": "三期歌曲05", "artist": "三期歌手1"}, map[string]string{"title": "三期歌曲11", "artist": "三期歌手2"}}
			for k := 0; k < 10; k++ {
				items = append(items, map[string]string{"title": fmt.Sprintf("曲库外的歌%d", k), "artist": "曲库外歌手"})
			}
			content = map[string]any{"items": items}
		}
		raw, _ := json.Marshal(content)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": string(raw)}}},
			"usage": map[string]any{"total_tokens": 100}})
	})
	mux.HandleFunc("POST /embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input      []string `json:"input"`
			Dimensions int      `json:"dimensions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		dim := max(req.Dimensions, 8)
		data := []any{}
		for i, text := range req.Input {
			v := make([]float64, dim)
			if strings.Contains(text, "流行") {
				v[0] = 1
			} else {
				v[1] = 1
			}
			h := fnv.New32a()
			_, _ = h.Write([]byte(text))
			v[2+int(h.Sum32()%uint32(dim-2))] = 0.2
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]any{"total_tokens": len(req.Input) * 10}})
	})
	t.Logf("fake llm on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		t.Fatal(err)
	}
}
