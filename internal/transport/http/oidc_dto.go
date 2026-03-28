package httptransport

type AdminOIDCSettingsUpdateRequest struct {
	Enabled         *bool                              `json:"enabled,omitempty"`
	IssuerURL       *string                            `json:"issuerURL,omitempty"`
	ClientID        *string                            `json:"clientID,omitempty"`
	ClientSecret    *string                            `json:"clientSecret,omitempty"`
	RedirectURL     *string                            `json:"redirectURL,omitempty"`
	Scopes          *[]string                          `json:"scopes,omitempty"`
	AllowedDomains  *[]string                          `json:"allowedDomains,omitempty"`
	AdminGroupClaim *string                            `json:"adminGroupClaim,omitempty"`
	AdminGroupValue *string                            `json:"adminGroupValue,omitempty"`
	AttrMapping     *AdminOIDCAttrMappingUpdateRequest `json:"attrMapping,omitempty"`
	FallbackToLocal     *bool                              `json:"fallbackToLocal,omitempty"`
	FrontendCallbackURL *string                            `json:"frontendCallbackURL,omitempty"`
}

type AdminOIDCAttrMappingUpdateRequest struct {
	Account     *string `json:"account,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
}

type AdminOIDCTestRequest struct {
	IssuerURL string `json:"issuerURL" binding:"required"`
}

type AdminOIDCExchangeRequest struct {
	Ticket string `json:"ticket" binding:"required"`
}
