package httptransport

// LeaderboardListQuery 通用排行榜查询（/api/leaderboard/points/:type 等）
type LeaderboardListQuery struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}

// LeaderboardSummary 概览响应：多榜单 Top N + 我的排名汇总，供前端首屏展示
type LeaderboardSummary struct {
	Integral     *LeaderboardTopSection `json:"integral,omitempty"`
	Experience   *LeaderboardTopSection `json:"experience,omitempty"`
	Level        *LeaderboardTopSection `json:"level,omitempty"`
	SignToday    *LeaderboardTopSection `json:"signToday,omitempty"`
	SignStreak   *LeaderboardTopSection `json:"signStreak,omitempty"`
	SignMonthly  *LeaderboardTopSection `json:"signMonthly,omitempty"`
	GeneratedAt  string                 `json:"generatedAt"`
}

// LeaderboardTopSection 榜单中的一个"榜"片段（Top N + 我排名）
type LeaderboardTopSection struct {
	Type   string `json:"type"`
	Total  int64  `json:"total"`
	Items  any    `json:"items"`  // []RankingItem（从 service 透传）
	MyRank any    `json:"myRank"` // *RankingItem
}
