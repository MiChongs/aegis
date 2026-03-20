package httptransport

type AdminStorageConfigListRequest struct {
	AppID    int64  `json:"appid" form:"appid"`
	Provider string `json:"provider" form:"provider"`
}

type AdminStorageConfigDetailRequest struct {
	AppID    int64 `json:"appid" form:"appid"`
	ConfigID int64 `json:"config_id" form:"config_id" binding:"required"`
}

type AdminStorageConfigSaveRequest struct {
	AppID         int64          `json:"appid" form:"appid"`
	ConfigID      int64          `json:"config_id" form:"config_id"`
	Provider      string         `json:"provider" form:"provider"`
	ConfigName    string         `json:"config_name" form:"config_name"`
	AccessMode    string         `json:"access_mode" form:"access_mode"`
	Enabled       *bool          `json:"enabled"`
	IsDefault     *bool          `json:"is_default"`
	ProxyDownload *bool          `json:"proxy_download"`
	BaseURL       string         `json:"base_url" form:"base_url"`
	RootPath      string         `json:"root_path" form:"root_path"`
	Description   string         `json:"description" form:"description"`
	ConfigData    map[string]any `json:"config_data"`
}

type StorageObjectLinkRequest struct {
	ConfigName string `json:"config_name" form:"config_name"`
	ObjectKey  string `json:"object_key" form:"object_key" binding:"required"`
	Download   bool   `json:"download" form:"download"`
	FileName   string `json:"file_name" form:"file_name"`
	ExpiresIn  int    `json:"expires_in" form:"expires_in"`
}
