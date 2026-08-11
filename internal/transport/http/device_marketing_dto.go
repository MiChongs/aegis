package httptransport

type DeviceMarketingQuery struct {
	Platform     string `form:"platform"`
	Keyword      string `form:"keyword"`
	Source       string `form:"source"`
	Manufacturer string `form:"manufacturer"`
	Page         int    `form:"page"`
	Limit        int    `form:"limit"`
}

type DeviceMarketingCreateRequest struct {
	Platform            string `json:"platform" form:"platform" binding:"required"`
	Identifier          string `json:"identifier" form:"identifier" binding:"required"`
	MarketingName       string `json:"marketingName" form:"marketingName" binding:"required"`
	Manufacturer        string `json:"manufacturer" form:"manufacturer"`
	ManufacturerIconURL string `json:"manufacturerIconUrl" form:"manufacturerIconUrl"`
	DeviceImageURL      string `json:"deviceImageUrl" form:"deviceImageUrl"`
	Notes               string `json:"notes" form:"notes"`
}

type DeviceMarketingUpdateRequest struct {
	Platform            *string `json:"platform" form:"platform"`
	Identifier          *string `json:"identifier" form:"identifier"`
	MarketingName       *string `json:"marketingName" form:"marketingName"`
	Manufacturer        *string `json:"manufacturer" form:"manufacturer"`
	ManufacturerIconURL *string `json:"manufacturerIconUrl" form:"manufacturerIconUrl"`
	DeviceImageURL      *string `json:"deviceImageUrl" form:"deviceImageUrl"`
	Notes               *string `json:"notes" form:"notes"`
}
