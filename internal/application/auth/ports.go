package auth

import "context"

type Principal struct {
	UserAccountID string
	PersonID      string
}

type AuthenticateRequest struct {
	Provider string
	Payload  []byte
}

type AccessRequest struct {
	Principal     Principal
	CompanyID     string
	OperationType string
	Capability    string
	Action        string
	ResourceID    string
}

type Authenticator interface {
	Authenticate(context.Context, AuthenticateRequest) (Principal, error)
}

type Authorizer interface {
	Authorize(context.Context, AccessRequest) error
}

type DeviceAuthorizer interface {
	AuthorizeInstallation(context.Context, Principal, string, string) error
	RevokeInstallation(context.Context, Principal, string) error
}
