package lottery

import (
	"strconv"
	"time"
)

// 推演任务：一次大规模推演落成一行（表 lottery_compute_jobs，迁移 000157），由任意一个实例认领执行。

// 推演任务的状态。
const (
	ComputeQueued    = "queued"
	ComputeRunning   = "running"
	ComputeSucceeded = "succeeded"
	ComputeFailed    = "failed"
	ComputeCancelled = "cancelled"
)

// 推演任务的上界。
const (
	// MaxComputeJobName 任务备注名的长度
	MaxComputeJobName = 32
	// MaxComputeAttempts 一个任务最多被认领几次：反复把实例跑垮（或实例反复重启）到次数后判失败
	MaxComputeAttempts = 3
	// MaxComputePending 一个应用同时排队与执行中的任务数
	MaxComputePending = 5
	// ComputeJobsKept 一个应用保留最近多少个已结束的任务
	ComputeJobsKept = 100
)

// ComputeStatuses 全部状态。
func ComputeStatuses() []string {
	return []string{ComputeQueued, ComputeRunning, ComputeSucceeded, ComputeFailed, ComputeCancelled}
}

// ComputePersonaSummary 任务摘要里的一种人群。
type ComputePersonaSummary struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Share       float64 `json:"share"`
	DrawsPerDay int     `json:"drawsPerDay"`
}

// ComputeSummary 任务的规模摘要（列表上用，不必解整个场景）。
type ComputeSummary struct {
	Users        int                     `json:"users"`
	Days         int                     `json:"days"`
	DrawsPerDay  int                     `json:"drawsPerDay"`
	Replications int                     `json:"replications"`
	Draws        int64                   `json:"draws"`
	Seed         string                  `json:"seed"`
	Source       string                  `json:"source,omitempty"`
	Personas     []ComputePersonaSummary `json:"personas"`
}

// SummaryOfScenario 从场景取规模摘要。
func SummaryOfScenario(scenario ComputeScenario) ComputeSummary {
	shares := normalizedShares(scenario.Personas)
	summary := ComputeSummary{
		Users: scenario.Users, Days: scenario.Days, DrawsPerDay: scenario.DrawsPerDay,
		Replications: scenario.Replications, Draws: scenario.TotalDraws(),
		Seed: strconv.FormatUint(scenario.Seed, 10), Source: scenario.Source,
		Personas: make([]ComputePersonaSummary, len(scenario.Personas)),
	}
	for index, persona := range scenario.Personas {
		draws := persona.DrawsPerDay
		if draws <= 0 {
			draws = scenario.DrawsPerDay
		}
		summary.Personas[index] = ComputePersonaSummary{Key: persona.Key, Name: persona.Name,
			Share: round6(shares[index]), DrawsPerDay: draws}
	}
	return summary
}

// ComputeJob 一个推演任务。
type ComputeJob struct {
	ID           int64          `json:"id"`
	AppID        int64          `json:"appid"`
	ActivityID   *int64         `json:"activityId,omitempty"`
	ActivityName string         `json:"activityName"`
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	Summary      ComputeSummary `json:"summary"`
	// Progress / Total 已合并与总共的平行世界数
	Progress   int        `json:"progress"`
	Total      int        `json:"total"`
	Message    string     `json:"message,omitempty"`
	Attempts   int        `json:"attempts"`
	DurationMs int64      `json:"durationMs"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	// Result 完成后的结果；列表上不带
	Result *ComputeResult `json:"result,omitempty"`
	// Scenario / Owner 只在服务端用
	Scenario *ComputeScenario `json:"-"`
	Owner    string           `json:"-"`
}

// Finished 任务已经结束（完成、失败或取消）。
func (j ComputeJob) Finished() bool {
	return j.Status == ComputeSucceeded || j.Status == ComputeFailed || j.Status == ComputeCancelled
}
