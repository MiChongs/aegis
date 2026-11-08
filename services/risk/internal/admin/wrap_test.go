package admin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestWrapEncodesEmptyListsAsArrays(t *testing.T) {
	type link struct {
		Kind string `json:"kind"`
	}
	type sub struct {
		Nodes   []string        `json:"nodes"`
		Links   []link          `json:"links"`
		Props   map[string]any  `json:"props"`
		Prime   *int64          `json:"primaryId"`
		Raw     json.RawMessage `json:"raw"`
		Blob    []byte          `json:"blob"`
		At      time.Time       `json:"at"`
		Hidden  []int           `json:"-"`
		private []int
	}
	shared := &sub{Nodes: []string{"u:1:1"}}
	s := &Server{}
	h := s.wrap(func(context.Context, *app.RequestContext) (any, error) {
		return map[string]any{"graph": shared, "timeline": []int(nil), "rows": []sub{{}}, "n": 3}, nil
	})
	c := app.NewContext(0)
	h(context.Background(), c)
	body := string(c.Response.Body())
	for _, want := range []string{`"links":[]`, `"props":{}`, `"timeline":[]`, `"primaryId":null`, `"nodes":["u:1:1"]`,
		`"raw":null`, `"blob":null`, `"n":3`, `"rows":[{"nodes":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
	if shared.Links != nil || shared.Props != nil {
		t.Fatal("原值被改了")
	}
}
