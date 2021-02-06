package oas

import (
	"github.com/TykTechnologies/tyk/apidef"
)

type Authentication struct {
	Enabled                bool   `bson:"enabled" json:"enabled"` // required
	StripAuthorizationData bool   `bson:"stripAuthorizationData,omitempty" json:"stripAuthorizationData,omitempty"`
	Token                  *Token `bson:"token,omitempty" json:"token,omitempty"`
}

func (a *Authentication) Fill(api apidef.APIDefinition) {
	a.Enabled = !api.UseKeylessAccess
	a.StripAuthorizationData = api.StripAuthData

	if api.AuthConfigs == nil || len(api.AuthConfigs) == 0 {
		return
	}

	if authToken, ok := api.AuthConfigs["authToken"]; ok {
		if a.Token == nil {
			a.Token = &Token{}
		}

		a.Token.Fill(api.UseStandardAuth, authToken)
	}
}

func (a *Authentication) ExtractTo(api *apidef.APIDefinition) {
	api.UseKeylessAccess = !a.Enabled
	api.StripAuthData = a.StripAuthorizationData

	if a.Token != nil {
		a.Token.ExtractTo(api)
	}
}

type Token struct {
	Enabled                 bool `bson:"enabled" json:"enabled"` // required
	AuthSources             `bson:",inline" json:",inline"`
	EnableClientCertificate bool       `bson:"enableClientCertificate,omitempty" json:"enableClientCertificate,omitempty"`
	Signature               *Signature `bson:"signatureValidation,omitempty" json:"signatureValidation,omitempty"`
}

func (t *Token) Fill(enabled bool, authToken apidef.AuthConfig) {
	t.Enabled = enabled

	// No need to check for emptiness like other optional fields(like Signature below) after filling because it is an inline field.
	t.AuthSources.Fill(authToken)

	t.EnableClientCertificate = authToken.UseCertificate

	if t.Signature == nil {
		t.Signature = &Signature{}
	}

	t.Signature.Fill(authToken)
	if (*t.Signature == Signature{}) {
		t.Signature = nil
	}
}

func (t *Token) ExtractTo(api *apidef.APIDefinition) {
	api.UseStandardAuth = t.Enabled

	authConfig := apidef.AuthConfig{}
	authConfig.UseCertificate = t.EnableClientCertificate

	t.AuthSources.ExtractTo(&authConfig)

	if t.Signature != nil {
		t.Signature.ExtractTo(&authConfig)
	}

	if api.AuthConfigs == nil {
		api.AuthConfigs = make(map[string]apidef.AuthConfig)
	}

	api.AuthConfigs["authToken"] = authConfig
}

/*type JWT struct {
	SkipKid                 bool              `json:"skip-kid,omitempty"`
	Source                  string            `json:"source,omitempty"`
	SigningMethod           string            `json:"signing-method,omitempty"`
	NotBeforeValidationSkew uint64            `json:"not-before-validation-skew,omitempty"`
	IssuedAtValidationSkew  uint64            `json:"issued-at-validation-skew,omitempty"`
	ExpiresAtValidationSkew uint64            `json:"expires-at-validation-skew,omitempty"`
	IdentityBaseField       string            `json:"identity-base-field,omitempty"`
	ClientBaseField         string            `json:"client-base-field,omitempty"`
	ScopeToPolicyMapping    map[string]string `json:"scope-to-policy-mapping,omitempty"`
	PolicyFieldName         string            `json:"policy-field-name,omitempty"`
	ScopeClaimName          string            `json:"scope-claim-name,omitempty"`
	DefaultPolicies         []string          `json:"default-policies,omitempty"`
}*/

type AuthSources struct {
	Header HeaderAuthSource `bson:"header" json:"header"` // required
	Cookie *AuthSource      `bson:"cookie,omitempty" json:"cookie,omitempty"`
	Param  *AuthSource      `bson:"param,omitempty" json:"param,omitempty"`
}

func (as *AuthSources) Fill(authConfig apidef.AuthConfig) {
	// Header
	as.Header = HeaderAuthSource{authConfig.AuthHeaderName}

	// Param
	if as.Param == nil {
		as.Param = &AuthSource{}
	}

	as.Param.Fill(authConfig.UseParam, authConfig.ParamName)
	if (*as.Param == AuthSource{}) {
		as.Param = nil
	}

	// Cookie
	if as.Cookie == nil {
		as.Cookie = &AuthSource{}
	}

	as.Cookie.Fill(authConfig.UseCookie, authConfig.CookieName)
	if (*as.Cookie == AuthSource{}) {
		as.Cookie = nil
	}
}

func (as *AuthSources) ExtractTo(authConfig *apidef.AuthConfig) {
	// Header
	authConfig.AuthHeaderName = as.Header.Name

	// Param
	if as.Param != nil {
		as.Param.ExtractTo(&authConfig.UseParam, &authConfig.ParamName)
	}

	// Cookie
	if as.Cookie != nil {
		as.Cookie.ExtractTo(&authConfig.UseCookie, &authConfig.CookieName)
	}
}

type HeaderAuthSource struct {
	Name string `bson:"name" json:"name"` // required
}

type AuthSource struct {
	Enabled bool   `bson:"enabled" json:"enabled"` // required
	Name    string `bson:"name,omitempty" json:"name,omitempty"`
}

func (as *AuthSource) Fill(enabled bool, name string) {
	as.Enabled = enabled
	as.Name = name
}

func (as *AuthSource) ExtractTo(enabled *bool, name *string) {
	*enabled = as.Enabled
	*name = as.Name
}

type Signature struct {
	Enabled          bool   `bson:"enabled" json:"enabled"` // required
	Algorithm        string `bson:"algorithm,omitempty" json:"algorithm,omitempty"`
	Header           string `bson:"header,omitempty" json:"header,omitempty"`
	Secret           string `bson:"secret,omitempty" json:"secret,omitempty"`
	AllowedClockSkew int64  `bson:"allowedClockSkew,omitempty" json:"allowedClockSkew,omitempty"`
	ErrorCode        int    `bson:"errorCode,omitempty" json:"errorCode,omitempty"`
	ErrorMessage     string `bson:"errorMessage,omitempty" json:"errorMessage,omitempty"`
}

func (s *Signature) Fill(authConfig apidef.AuthConfig) {
	signature := authConfig.Signature

	s.Enabled = authConfig.ValidateSignature
	s.Algorithm = signature.Algorithm
	s.Header = signature.Header
	s.Secret = signature.Secret
	s.AllowedClockSkew = signature.AllowedClockSkew
	s.ErrorCode = signature.ErrorCode
	s.ErrorMessage = signature.ErrorMessage
}

func (s *Signature) ExtractTo(authConfig *apidef.AuthConfig) {
	authConfig.ValidateSignature = s.Enabled

	authConfig.Signature.Algorithm = s.Algorithm
	authConfig.Signature.Header = s.Header
	authConfig.Signature.Secret = s.Secret
	authConfig.Signature.AllowedClockSkew = s.AllowedClockSkew
	authConfig.Signature.ErrorCode = s.ErrorCode
	authConfig.Signature.ErrorMessage = s.ErrorMessage
}