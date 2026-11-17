package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// swarmparse.go：解析 docker 命令（--format '{{json .}}'）的输出。全部是纯函数，便于测试。

// flexBool docker 的 json 输出里布尔值有时是字符串
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*b = flexBool(s == "true")
	return nil
}

// ServiceRow docker service ls --format '{{json .}}'
type ServiceRow struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	Mode     string `json:"Mode"`
	Replicas string `json:"Replicas"`
	Image    string `json:"Image"`
	Ports    string `json:"Ports"`
}

// NodeRow docker node ls --format '{{json .}}'
type NodeRow struct {
	ID            string   `json:"ID"`
	Hostname      string   `json:"Hostname"`
	Status        string   `json:"Status"`
	Availability  string   `json:"Availability"`
	ManagerStatus string   `json:"ManagerStatus"`
	EngineVersion string   `json:"EngineVersion"`
	Self          flexBool `json:"Self"`
}

// NodeInfo docker node inspect（只取用到的字段）
type NodeInfo struct {
	ID   string `json:"ID"`
	Spec struct {
		Labels       map[string]string `json:"Labels"`
		Role         string            `json:"Role"`
		Availability string            `json:"Availability"`
	} `json:"Spec"`
	Description struct {
		Hostname  string `json:"Hostname"`
		Resources struct {
			NanoCPUs    int64 `json:"NanoCPUs"`
			MemoryBytes int64 `json:"MemoryBytes"`
		} `json:"Resources"`
	} `json:"Description"`
	Status struct {
		State string `json:"State"`
		Addr  string `json:"Addr"`
	} `json:"Status"`
	ManagerStatus *struct {
		Leader bool   `json:"Leader"`
		Addr   string `json:"Addr"`
	} `json:"ManagerStatus"`
}

// ServiceInfo docker service inspect 的精简格式（见 serviceInspectFormat，刻意不取环境变量）
type ServiceInfo struct {
	Name   string            `json:"name"`
	Image  string            `json:"image"`
	Labels map[string]string `json:"labels"`
	Mode   struct {
		Replicated *struct {
			Replicas int `json:"Replicas"`
		} `json:"Replicated"`
		ReplicatedJob *struct {
			MaxConcurrent    int `json:"MaxConcurrent"`
			TotalCompletions int `json:"TotalCompletions"`
		} `json:"ReplicatedJob"`
		Global *struct{} `json:"Global"`
	} `json:"mode"`
	Update *struct {
		State       string    `json:"State"`
		StartedAt   time.Time `json:"StartedAt"`
		CompletedAt time.Time `json:"CompletedAt"`
		Message     string    `json:"Message"`
	} `json:"update"`
	Job *struct {
		JobIteration struct {
			Index int64 `json:"Index"`
		} `json:"JobIteration"`
		LastExecution time.Time `json:"LastExecution"`
	} `json:"job"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// serviceInspectFormat 只取状态相关字段，不把服务的环境变量（含密钥）拉到本机
const serviceInspectFormat = `{"name":{{json .Spec.Name}},"image":{{json .Spec.TaskTemplate.ContainerSpec.Image}},"labels":{{json .Spec.Labels}},"mode":{{json .Spec.Mode}},"update":{{json .UpdateStatus}},"job":{{json .JobStatus}},"updatedAt":{{json .UpdatedAt}}}`

// IsJob 一次性任务
func (s ServiceInfo) IsJob() bool { return s.Mode.ReplicatedJob != nil }

// UpdateState 最近一次更新状态，没有更新过时为空
func (s ServiceInfo) UpdateState() string {
	if s.Update == nil {
		return ""
	}
	return s.Update.State
}

// TaskInfo docker inspect <任务> 的精简格式（见 taskInspectFormat）
type TaskInfo struct {
	ID           string    `json:"id"`
	ServiceID    string    `json:"service"`
	NodeID       string    `json:"node"`
	Slot         int       `json:"slot"`
	DesiredState string    `json:"desired"`
	State        string    `json:"state"`
	Message      string    `json:"message"`
	Err          string    `json:"err"`
	Timestamp    time.Time `json:"ts"`
	ContainerID  string    `json:"container"`
	JobIteration *struct {
		Index int64 `json:"Index"`
	} `json:"job"`
}

const taskInspectFormat = `{"id":{{json .ID}},"service":{{json .ServiceID}},"node":{{json .NodeID}},"slot":{{json .Slot}},"desired":{{json .DesiredState}},"state":{{json .Status.State}},"message":{{json .Status.Message}},"err":{{json .Status.Err}},"ts":{{json .Status.Timestamp}},"container":{{with .Status.ContainerStatus}}{{json .ContainerID}}{{else}}""{{end}},"job":{{json .JobIteration}}}`

// TaskRow docker stack ps / service ps --format '{{json .}}'
type TaskRow struct {
	ID           string `json:"ID"`
	Name         string `json:"Name"`
	Image        string `json:"Image"`
	Node         string `json:"Node"`
	DesiredState string `json:"DesiredState"`
	CurrentState string `json:"CurrentState"`
	Error        string `json:"Error"`
}

// ParseJSONLines 每行一个 JSON 对象；空行跳过
func ParseJSONLines[T any](out string) ([]T, error) {
	var items []T
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || line == "null" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, fmt.Errorf("第 %d 行不是合法的 JSON：%w", n, err)
		}
		items = append(items, v)
	}
	return items, sc.Err()
}

// Sections 把 "@@名字" 分隔的多段输出切开
func Sections(out string) map[string]string {
	res := map[string]string{}
	cur := ""
	var b strings.Builder
	flush := func() {
		if cur != "" {
			res[cur] = b.String()
		}
		b.Reset()
	}
	for _, line := range strings.SplitAfter(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "@@") && !strings.Contains(t, " ") {
			flush()
			cur = strings.TrimPrefix(t, "@@")
			continue
		}
		b.WriteString(line)
	}
	flush()
	return res
}

var replicasPattern = regexp.MustCompile(`^(\d+)/(\d+)`)
var completedPattern = regexp.MustCompile(`\((\d+)/(\d+) completed\)`)

// Replicas docker service ls 的 Replicas 列
type Replicas struct {
	Running   int
	Desired   int
	Completed int
	Total     int // 一次性任务的总完成数
	Job       bool
	Valid     bool
}

// ParseReplicas 解析 "2/2"、"0/1 (max 3 per node)"、"0/1 (1/1 completed)"
func ParseReplicas(s string) Replicas {
	var r Replicas
	m := replicasPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return r
	}
	r.Running, _ = strconv.Atoi(m[1])
	r.Desired, _ = strconv.Atoi(m[2])
	r.Valid = true
	if c := completedPattern.FindStringSubmatch(s); c != nil {
		r.Job = true
		r.Completed, _ = strconv.Atoi(c[1])
		r.Total, _ = strconv.Atoi(c[2])
	}
	return r
}

// SplitImage 拆出仓库与标签（去掉 @sha256 摘要）
func SplitImage(image string) (repo, tag string) {
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash {
		return image[:colon], image[colon+1:]
	}
	return image, "latest"
}

// ShortService 去掉栈名前缀：aegis_aegis-api → aegis-api
func ShortService(stack, name string) string {
	return strings.TrimPrefix(name, stack+"_")
}

// ─────────────────────────── 迁移任务状态 ───────────────────────────

// JobOutcome 一次性任务某一轮的结果
type JobOutcome struct {
	Done      bool
	Succeeded bool
	Failed    int
	Running   int
	LastError string
}

// EvaluateJob 统计某一轮（JobIteration）的任务：有一个 complete 即成功；失败次数达到上限即失败
func EvaluateJob(tasks []TaskInfo, iteration int64, maxAttempts int) JobOutcome {
	var o JobOutcome
	var latest time.Time
	for _, t := range tasks {
		if t.JobIteration == nil || t.JobIteration.Index != iteration {
			continue
		}
		switch t.State {
		case "complete":
			o.Done, o.Succeeded = true, true
		case "failed", "rejected", "orphaned":
			o.Failed++
			if t.Timestamp.After(latest) {
				latest = t.Timestamp
				o.LastError = firstNonEmpty(t.Err, t.Message)
			}
		case "shutdown", "remove":
		default:
			o.Running++
		}
	}
	if !o.Succeeded && maxAttempts > 0 && o.Failed >= maxAttempts && o.Running == 0 {
		o.Done = true
	}
	return o
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ─────────────────────────── 滚动更新收敛 ───────────────────────────

// Convergence 某个服务的收敛判断
type Convergence struct {
	Service string
	Running int
	Desired int
	State   string // UpdateStatus.State
	Done    bool
	Failed  bool
	Detail  string
}

// EvaluateConvergence 判断服务是否完成本次更新。since 之前开始的更新状态不算本次的
func EvaluateConvergence(info ServiceInfo, rep Replicas, since time.Time) Convergence {
	c := Convergence{Service: info.Name, Running: rep.Running, Desired: rep.Desired}
	state := ""
	if info.Update != nil && !info.Update.StartedAt.Before(since) {
		state = info.Update.State
		c.Detail = info.Update.Message
	}
	c.State = state
	switch state {
	case "paused", "rollback_paused":
		c.Failed, c.Done = true, true
		return c
	case "rollback_completed":
		c.Failed, c.Done = true, true
		if c.Detail == "" {
			c.Detail = "更新失败，Swarm 已自动回滚"
		}
		return c
	case "updating", "rollback_started":
		return c
	}
	c.Done = rep.Valid && rep.Running >= rep.Desired
	return c
}

// ─────────────────────────── 节点总览 ───────────────────────────

// AegisLabels 节点上 aegis.* 标签，按键排序后的 k=v 列表
func AegisLabels(labels map[string]string) []string {
	var out []string
	for k, v := range labels {
		if strings.HasPrefix(k, "aegis.") {
			out = append(out, k+"="+v)
		}
	}
	sort.Strings(out)
	return out
}

// TasksPerNode 每个节点上正在运行的任务数（按节点主机名）
func TasksPerNode(tasks []TaskRow) map[string]int {
	m := map[string]int{}
	for _, t := range tasks {
		if t.DesiredState == "Running" && strings.HasPrefix(t.CurrentState, "Running") {
			m[t.Node]++
		}
	}
	return m
}

// ServicesPerNode 每个服务在各节点上的运行任务数
func ServicesPerNode(stack string, tasks []TaskRow) map[string]map[string]int {
	m := map[string]map[string]int{}
	for _, t := range tasks {
		if t.DesiredState != "Running" || !strings.HasPrefix(t.CurrentState, "Running") {
			continue
		}
		name := t.Name
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i]
		}
		name = ShortService(stack, name)
		if m[name] == nil {
			m[name] = map[string]int{}
		}
		m[name][t.Node]++
	}
	return m
}

// HealthProbe 一条探活结果：角色、地址、HTTP 状态码
type HealthProbe struct {
	Role string
	Addr string
	Code string
}

// ParseHealthProbes 解析 "api 10.0.5.3 200" 形式的输出
func ParseHealthProbes(out string) []HealthProbe {
	var res []HealthProbe
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 {
			res = append(res, HealthProbe{Role: f[0], Addr: f[1], Code: f[2]})
		}
	}
	return res
}
