package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidInput      = errors.New("invalid authentication input")
	ErrInactiveAccount   = errors.New("authentication account is inactive")
	ErrInvalidSession    = errors.New("authentication session is invalid")
	ErrChallengeConsumed = errors.New("authentication challenge was already consumed")
)

type Principal struct {
	UserAccountID       string
	PersonID            string
	SessionID           string
	SessionEpoch        int64
	ContextVersion      int64
	ActiveCompanyID     string
	ActiveOperationID   string
	CSRFSecretHash      []byte
	AuthenticationLevel string
	StepUpExpiresAt     *time.Time
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

// SessionKind selects a server-side policy. It is never supplied by an HTTP
// client; the future authentication boundary derives it from the authenticated
// flow and risk policy.
type SessionKind string

const (
	AdministrativeSession   SessionKind = "ADMINISTRATIVE"
	DriverSession           SessionKind = "DRIVER"
	DriverActivationSession SessionKind = "DRIVER_ACTIVATION"
)

type SessionPolicy struct {
	IdleTTL     time.Duration
	AbsoluteTTL time.Duration
}

func PolicyFor(kind SessionKind) (SessionPolicy, error) {
	switch kind {
	case AdministrativeSession:
		return SessionPolicy{IdleTTL: 30 * time.Minute, AbsoluteTTL: 8 * time.Hour}, nil
	case DriverSession:
		return SessionPolicy{IdleTTL: 60 * time.Minute, AbsoluteTTL: 12 * time.Hour}, nil
	case DriverActivationSession:
		return SessionPolicy{IdleTTL: 15 * time.Minute, AbsoluteTTL: time.Hour}, nil
	default:
		return SessionPolicy{}, ErrInvalidInput
	}
}

type CreateSessionRequest struct {
	ID                        uuid.UUID
	UserAccountID             uuid.UUID
	TokenHash                 []byte
	CSRFSecretHash            []byte
	Policy                    SessionPolicy
	ActiveCompanyID           *uuid.UUID
	ActiveOperationID         *uuid.UUID
	AuthenticatorCredentialID *uuid.UUID
	DeviceAuthorizationID     *uuid.UUID
	UserAgentHash             []byte
	IPPrefix                  string
}

type SessionRecord struct {
	ID                uuid.UUID
	UserAccountID     uuid.UUID
	SessionEpoch      int64
	Status            string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	ActiveCompanyID   *uuid.UUID
	ActiveOperationID *uuid.UUID
	ContextVersion    int64
}

type ResolvedSession struct {
	SessionRecord
	PersonID                  uuid.UUID
	AuthenticatorCredentialID *uuid.UUID
	DeviceAuthorizationID     *uuid.UUID
	RevokedAt                 *time.Time
	ExpiredAt                 *time.Time
	CompromisedAt             *time.Time
}

type SessionRepository interface {
	CreateSession(context.Context, CreateSessionRequest) (SessionRecord, error)
	ResolveSession(context.Context, []byte) (ResolvedSession, error)
	RevokeSession(context.Context, uuid.UUID, []byte, string) error
	RevokeAllSessions(context.Context, []byte, string) error
}

type ChallengePurpose string

const (
	LoginChallenge              ChallengePurpose = "LOGIN"
	DriverLoginChallengePurpose ChallengePurpose = "DRIVER_LOGIN"
	RegistrationChallenge       ChallengePurpose = "REGISTRATION"
	StepUpChallenge             ChallengePurpose = "STEP_UP"
	RecoveryChallenge           ChallengePurpose = "RECOVERY"
	DeviceEnrollmentChallenge   ChallengePurpose = "DEVICE_ENROLLMENT"
)

type CreateChallengeRequest struct {
	ID                        uuid.UUID
	Purpose                   ChallengePurpose
	UserAccountID             *uuid.UUID
	DeviceEnrollmentRequestID *uuid.UUID
	ChallengeHash             []byte
	SessionData               []byte
	SessionDataFormat         string
	BindingHash               []byte
	ExpiresAt                 time.Time
	RequestFingerprint        []byte
}

type ConsumedChallenge struct {
	ID                        uuid.UUID
	UserAccountID             *uuid.UUID
	DeviceEnrollmentRequestID *uuid.UUID
	SessionData               []byte
	SessionDataFormat         string
}

type ChallengeRepository interface {
	CreateChallenge(context.Context, CreateChallengeRequest) error
	ConsumeChallenge(context.Context, uuid.UUID, ChallengePurpose, []byte, []byte) (ConsumedChallenge, error)
}

// SecurityAuditEvent is intentionally independent of HTTP payloads. Actor and
// subject IDs must come from server-side authentication state.
type SecurityAuditEvent struct {
	ID                   uuid.UUID
	EventType            string
	Outcome              string
	ActorUserAccountID   *uuid.UUID
	SubjectUserAccountID *uuid.UUID
	CompanyID            *uuid.UUID
	CompanyOperationID   *uuid.UUID
	ReasonCode           *string
	CorrelationID        *string
	Details              []byte
}

type SecurityAuditWriter interface {
	WriteSecurityAudit(context.Context, SecurityAuditEvent) error
}

// LoginCredential is the minimum server-side material required to verify a
// password. It is never constructed from an HTTP payload.
type LoginCredential struct {
	VerificationID       uuid.UUID
	VerificationNonce    []byte
	UserAccountID        uuid.UUID
	PersonID             uuid.UUID
	PasswordCredentialID uuid.UUID
	PasswordHash         string
	KDFName              string
	KDFParameters        json.RawMessage
}

type AuthenticatedSession struct {
	ID                  uuid.UUID
	UserAccountID       uuid.UUID
	PersonID            uuid.UUID
	SessionEpoch        int64
	ContextVersion      int64
	ActiveCompanyID     *uuid.UUID
	ActiveOperationID   *uuid.UUID
	SessionPolicy       SessionKind
	Status              string
	CreatedAt           time.Time
	LastSeenAt          time.Time
	IdleExpiresAt       time.Time
	AbsoluteExpiresAt   time.Time
	CSRFSecretHash      []byte
	AuthenticationLevel string
	StepUpExpiresAt     *time.Time
	StepUpCredentialID  *uuid.UUID
}

type AuthenticatorCredentialRecord struct {
	ID               uuid.UUID
	CredentialID     []byte
	PublicKey        []byte
	SignCount        uint32
	Transports       []string
	DisplayName      string
	Status           string
	BackupEligible   bool
	BackupState      *string
	UserVerifiedLast bool
	CreatedAt        time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
}

type WebAuthnChallenge struct {
	ID                uuid.UUID
	ChallengeHash     []byte
	SessionData       []byte
	SessionDataFormat string
	BindingHash       []byte
	UserAccountID     uuid.UUID
	DeviceRequestID   *uuid.UUID
}

type DeviceEnrollmentRequest struct {
	ID                 uuid.UUID
	CorrelationCode    string
	CompanyID          uuid.UUID
	CompanyOperationID *uuid.UUID
	InstallationID     uuid.UUID
	Status             string
	ExpiresAt          time.Time
}

type DriverActivationRequestStatus struct {
	RequestID           uuid.UUID
	CorrelationCode     string
	CompanyID           uuid.UUID
	CompanyOperationID  *uuid.UUID
	InstallationID      uuid.UUID
	Status              string
	ExpiresAt           time.Time
	AuthorizationStatus *string
}

type DeviceEnrollmentRequestView struct {
	ID                 uuid.UUID
	CorrelationCode    string
	UserAccountID      uuid.UUID
	Identifier         string
	LegalName          string
	CompanyID          uuid.UUID
	CompanyOperationID *uuid.UUID
	InstallationID     uuid.UUID
	Status             string
	RequestedAt        time.Time
	ApprovedAt         *time.Time
	ExpiresAt          time.Time
}

type DeviceAuthorizationView struct {
	ID                 uuid.UUID
	UserAccountID      uuid.UUID
	Identifier         string
	LegalName          string
	CompanyID          uuid.UUID
	CompanyOperationID *uuid.UUID
	InstallationID     uuid.UUID
	KeyAlgorithm       string
	Status             string
	RequestedAt        time.Time
	ApprovedAt         *time.Time
	ExpiresAt          *time.Time
}

type DeviceEnrollmentRequestInput struct {
	ID                    uuid.UUID
	CorrelationCode       string
	CompanyID             uuid.UUID
	CompanyOperationID    *uuid.UUID
	InstallationID        uuid.UUID
	InstallationPublicKey []byte
	KeyAlgorithm          string
	BindingHash           []byte
	ExpiresAt             time.Time
	AuditID               uuid.UUID
}

type WebAuthnStore interface {
	BeginWebAuthnChallenge(context.Context, []byte, WebAuthnChallengeRequest) error
	GetWebAuthnChallenge(context.Context, []byte, uuid.UUID, ChallengePurpose) (WebAuthnChallenge, error)
	CompleteWebAuthnRegistration(context.Context, []byte, CompleteRegistrationRequest) (uuid.UUID, *uuid.UUID, error)
	CompleteWebAuthnStepUp(context.Context, []byte, CompleteStepUpRequest) (time.Time, error)
	ListAuthenticatorCredentials(context.Context, []byte) ([]AuthenticatorCredentialRecord, error)
	RevokeOwnAuthenticator(context.Context, []byte, uuid.UUID, string) error
	CreateDeviceEnrollmentRequest(context.Context, []byte, DeviceEnrollmentRequestInput) (DeviceEnrollmentRequest, error)
	ListDeviceEnrollmentRequests(context.Context, []byte) ([]DeviceEnrollmentRequestView, error)
	ListDeviceAuthorizations(context.Context, []byte) ([]DeviceAuthorizationView, error)
	ApproveDeviceEnrollmentRequest(context.Context, []byte, uuid.UUID, string) error
	RejectDeviceEnrollmentRequest(context.Context, []byte, uuid.UUID, string) error
	RevokeDeviceAuthorization(context.Context, []byte, uuid.UUID, string) error
}

// DriverMobileStore is the narrow pre-authentication boundary for recurring
// driver login. It never accepts a user/account id from HTTP; the SQL
// functions derive the account and device scope from the identifier,
// installation and single-use challenge.
type DriverMobileStore interface {
	ConsumeRateLimit(context.Context, []byte, string, int, int) (bool, error)
	ListDriverLoginCredentials(context.Context, string, uuid.UUID) ([]DriverLoginCredential, error)
	ListDriverLoginCredentialsForChallenge(context.Context, uuid.UUID) ([]DriverLoginCredential, error)
	ListDriverAuthorizedContexts(context.Context, []byte) ([]DriverAuthorizedContext, error)
	BeginDriverLoginChallenge(context.Context, DriverLoginChallengeRequest) error
	GetDriverLoginChallenge(context.Context, uuid.UUID) (DriverLoginChallenge, error)
	CompleteDriverLogin(context.Context, CompleteDriverLoginRequest) (SessionRecord, error)
}

type DriverAuthorizedContext struct {
	CompanyID             uuid.UUID  `json:"company_id"`
	CompanyOperationID    *uuid.UUID `json:"company_operation_id"`
	DeviceAuthorizationID uuid.UUID  `json:"device_authorization_id"`
	CompanyName           *string    `json:"company_name"`
	OperationStatus       *string    `json:"operation_status"`
}

type DriverLoginCredential struct {
	UserAccountID         uuid.UUID
	DeviceAuthorizationID uuid.UUID
	InstallationID        uuid.UUID
	CompanyID             uuid.UUID
	CompanyOperationID    *uuid.UUID
	InstallationPublicKey []byte
	KeyAlgorithm          string
	Credential            AuthenticatorCredentialRecord
}

type DriverLoginChallengeRequest struct {
	Identifier            string
	InstallationID        uuid.UUID
	ChallengeID           uuid.UUID
	DeviceAuthorizationID uuid.UUID
	ChallengeHash         []byte
	SessionData           []byte
	SessionDataFormat     string
	BindingHash           []byte
	ExpiresAt             time.Time
	AuditID               uuid.UUID
}

type DriverLoginChallenge struct {
	ID                    uuid.UUID
	ChallengeHash         []byte
	SessionData           []byte
	SessionDataFormat     string
	BindingHash           []byte
	UserAccountID         uuid.UUID
	DeviceAuthorizationID uuid.UUID
	InstallationID        uuid.UUID
	CompanyID             uuid.UUID
	CompanyOperationID    *uuid.UUID
	InstallationPublicKey []byte
	KeyAlgorithm          string
}

type CompleteDriverLoginRequest struct {
	ChallengeID    uuid.UUID
	ChallengeHash  []byte
	BindingHash    []byte
	CredentialKey  []byte
	SignCount      uint32
	SessionID      uuid.UUID
	TokenHash      []byte
	CSRFSecretHash []byte
	UserAgentHash  []byte
	IPPrefix       string
	AuditID        uuid.UUID
	LoginAuditID   uuid.UUID
}

type WebAuthnChallengeRequest struct {
	ID                uuid.UUID
	Purpose           ChallengePurpose
	ChallengeHash     []byte
	SessionData       []byte
	SessionDataFormat string
	BindingHash       []byte
	ExpiresAt         time.Time
	DeviceRequestID   *uuid.UUID
	AuditID           uuid.UUID
}

type CompleteRegistrationRequest struct {
	ChallengeID           uuid.UUID
	ChallengeHash         []byte
	BindingHash           []byte
	CredentialID          uuid.UUID
	CredentialKey         []byte
	PublicKey             []byte
	SignCount             uint32
	Transports            []string
	BackupEligible        bool
	BackupState           string
	UserVerified          bool
	DisplayName           string
	Metadata              json.RawMessage
	DeviceAuthorizationID uuid.UUID
	AuditID               uuid.UUID
}

type CompleteStepUpRequest struct {
	ChallengeID   uuid.UUID
	ChallengeHash []byte
	BindingHash   []byte
	CredentialKey []byte
	AuditID       uuid.UUID
}

type SessionSummary struct {
	ID                uuid.UUID
	Status            string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	ActiveCompanyID   *uuid.UUID
	ActiveOperationID *uuid.UUID
	SessionPolicy     SessionKind
	Current           bool
}

type LoginGrant struct {
	ID                    uuid.UUID
	GrantHash             []byte
	VerificationID        uuid.UUID
	VerificationNonceHash []byte
}

type Invitation struct {
	ID        uuid.UUID
	ExpiresAt time.Time
}

type OnlineAuthStore interface {
	ResolveLoginCredential(context.Context, string) (LoginCredential, error)
	ConsumeRateLimit(context.Context, []byte, string, int, int) (bool, error)
	RecordVerifiedPasswordLogin(context.Context, LoginGrant, int) error
	IssueAuthSession(context.Context, LoginGrant, CreateSessionRequest, uuid.UUID) (SessionRecord, error)
	ResolveHTTPSession(context.Context, []byte) (AuthenticatedSession, error)
	ListAuthSessions(context.Context, []byte) ([]SessionSummary, error)
	RevokeSession(context.Context, uuid.UUID, []byte, string) error
	RevokeAllSessions(context.Context, []byte, string) error
	SetAuthSessionContext(context.Context, []byte, uuid.UUID, *uuid.UUID, *uuid.UUID) (int64, error)
	CreateAdminInvitation(context.Context, []byte, uuid.UUID, []byte, string, string, int) (Invitation, error)
	ConsumeAdminInvitation(context.Context, []byte, string, json.RawMessage, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (uuid.UUID, uuid.UUID, string, error)
	PrepareBootstrapRequest(context.Context, uuid.UUID, []byte, []byte, string, string, string, json.RawMessage, int) error
	CompleteBootstrapRequest(context.Context, []byte, []byte, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (uuid.UUID, uuid.UUID, error)
	RecordLoginFailure(context.Context, uuid.UUID, string) error
}

type DriverActivationStore interface {
	OnlineAuthStore
	IssueDriverActivationSession(context.Context, LoginGrant, CreateSessionRequest, uuid.UUID) (SessionRecord, error)
}

type DriverActivationStatusStore interface {
	GetDriverActivationRequestStatus(context.Context, []byte, uuid.UUID, uuid.UUID) (DriverActivationRequestStatus, error)
}
