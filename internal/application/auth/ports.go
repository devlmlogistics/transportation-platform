package auth

import (
	"context"
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

// SessionKind selects a server-side policy. It is never supplied by an HTTP
// client; the future authentication boundary derives it from the authenticated
// flow and risk policy.
type SessionKind string

const (
	AdministrativeSession SessionKind = "ADMINISTRATIVE"
	DriverSession         SessionKind = "DRIVER"
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
		return SessionPolicy{IdleTTL: 2 * time.Hour, AbsoluteTTL: 12 * time.Hour}, nil
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
	LoginChallenge            ChallengePurpose = "LOGIN"
	RegistrationChallenge     ChallengePurpose = "REGISTRATION"
	StepUpChallenge           ChallengePurpose = "STEP_UP"
	RecoveryChallenge         ChallengePurpose = "RECOVERY"
	DeviceEnrollmentChallenge ChallengePurpose = "DEVICE_ENROLLMENT"
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
