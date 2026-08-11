package httptransport

type AdminSAMLSettingsUpdateRequest struct {
	Enabled             *bool                              `json:"enabled,omitempty"`
	IDPMetadataURL      *string                            `json:"idpMetadataURL,omitempty"`
	IDPMetadataXML      *string                            `json:"idpMetadataXML,omitempty"`
	EntityID            *string                            `json:"entityID,omitempty"`
	MetadataURL         *string                            `json:"metadataURL,omitempty"`
	ACSURL              *string                            `json:"acsURL,omitempty"`
	SPCertificate       *string                            `json:"spCertificate,omitempty"`
	SPPrivateKey        *string                            `json:"spPrivateKey,omitempty"`
	NameIDFormat        *string                            `json:"nameIDFormat,omitempty"`
	SignAuthnRequests   *bool                              `json:"signAuthnRequests,omitempty"`
	ForceAuthn          *bool                              `json:"forceAuthn,omitempty"`
	AllowIDPInitiated   *bool                              `json:"allowIdpInitiated,omitempty"`
	AllowedDomains      *[]string                          `json:"allowedDomains,omitempty"`
	AdminGroupAttribute *string                            `json:"adminGroupAttribute,omitempty"`
	AdminGroupValue     *string                            `json:"adminGroupValue,omitempty"`
	AttrMapping         *AdminSAMLAttrMappingUpdateRequest `json:"attrMapping,omitempty"`
	FallbackToLocal     *bool                              `json:"fallbackToLocal,omitempty"`
	FrontendCallbackURL *string                            `json:"frontendCallbackURL,omitempty"`
}

type AdminSAMLAttrMappingUpdateRequest struct {
	Account     *string `json:"account,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
	Groups      *string `json:"groups,omitempty"`
}

type AdminSAMLTestRequest struct {
	IDPMetadataURL string `json:"idpMetadataURL"`
	IDPMetadataXML string `json:"idpMetadataXML"`
}

type AdminSAMLExchangeRequest struct {
	Ticket string `json:"ticket" binding:"required"`
}
