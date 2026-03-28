package httptransport

import "encoding/json"

type SecondFactorVerifyRequest struct {
	ChallengeID  string `json:"challengeId" form:"challengeId" binding:"required"`
	Code         string `json:"code" form:"code"`
	RecoveryCode string `json:"recoveryCode" form:"recoveryCode"`
}

type TOTPEnableRequest struct {
	EnrollmentID string `json:"enrollmentId" form:"enrollmentId" binding:"required"`
	Code         string `json:"code" form:"code" binding:"required"`
}

type TOTPDisableRequest struct {
	Code         string `json:"code" form:"code"`
	RecoveryCode string `json:"recoveryCode" form:"recoveryCode"`
}

type RecoveryCodesRegenerateRequest struct {
	Code         string `json:"code" form:"code"`
	RecoveryCode string `json:"recoveryCode" form:"recoveryCode"`
}

type PasskeyRegistrationFinishRequest struct {
	ChallengeID    string          `json:"challengeId" form:"challengeId" binding:"required"`
	Credential     json.RawMessage `json:"credential"`
	Payload        json.RawMessage `json:"payload"`
	CredentialName string          `json:"credentialName" form:"credentialName"`
}

type PasskeyLoginBeginRequest struct {
	AppID    int64  `json:"appid" form:"appid" binding:"required"`
	MarkCode string `json:"markcode" form:"markcode"`
}

type PasskeyLoginVerifyRequest struct {
	AppID       int64           `json:"appid" form:"appid" binding:"required"`
	ChallengeID string          `json:"challengeId" form:"challengeId" binding:"required"`
	Credential  json.RawMessage `json:"credential"`
	Payload     json.RawMessage `json:"payload"`
	MarkCode    string          `json:"markcode" form:"markcode"`
}
