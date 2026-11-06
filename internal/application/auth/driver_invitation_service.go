package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrDriverInvitationInvalid  = errors.New("driver enrollment invitation is invalid or expired")
	ErrDriverInvitationConflict = errors.New("driver enrollment invitation conflicts with an active invitation or request")
)

type DriverEnrollmentInvitationService struct {
	Store DriverEnrollmentInvitationStore
}

func NewDriverEnrollmentInvitationService(store DriverEnrollmentInvitationStore) (*DriverEnrollmentInvitationService, error) {
	if store == nil {
		return nil, errors.New("driver enrollment invitation store is required")
	}
	return &DriverEnrollmentInvitationService{Store: store}, nil
}

type IssuedDriverEnrollmentInvitation struct {
	Invitation DriverEnrollmentInvitation
	Token      []byte
	Code       string
}

func (s *DriverEnrollmentInvitationService) Create(ctx context.Context, sessionToken []byte, session AuthenticatedSession, input DriverEnrollmentInvitationInput) (IssuedDriverEnrollmentInvitation, error) {
	if s == nil || s.Store == nil || len(sessionToken) != 32 || session.ID == uuid.Nil || session.SessionPolicy != AdministrativeSession || input.PersonID == uuid.Nil || input.CompanyID == uuid.Nil {
		return IssuedDriverEnrollmentInvitation{}, ErrDriverInvitationInvalid
	}
	if input.ExpiresSeconds == 0 {
		input.ExpiresSeconds = 900
	}
	if input.ExpiresSeconds < 300 || input.ExpiresSeconds > 3600 {
		return IssuedDriverEnrollmentInvitation{}, ErrInvalidInput
	}
	token, err := randomBytes(32)
	if err != nil {
		return IssuedDriverEnrollmentInvitation{}, err
	}
	code, err := randomConfirmationCode()
	if err != nil {
		return IssuedDriverEnrollmentInvitation{}, err
	}
	invitationID := uuid.New()
	invitation, err := s.Store.CreateDriverEnrollmentInvitation(ctx, HashSessionToken(sessionToken), DriverEnrollmentInvitationCreateRequest{
		TokenHash: HashSessionToken(token), ConfirmationHash: HashSessionToken([]byte(code)),
		InvitationID: invitationID, AuditID: uuid.New(), Input: input,
	})
	if errors.Is(err, ErrDriverInvitationConflict) {
		return IssuedDriverEnrollmentInvitation{}, err
	}
	if err != nil {
		return IssuedDriverEnrollmentInvitation{}, ErrDriverInvitationInvalid
	}
	return IssuedDriverEnrollmentInvitation{Invitation: invitation, Token: token, Code: code}, nil
}

func (s *DriverEnrollmentInvitationService) List(ctx context.Context, sessionToken []byte, session AuthenticatedSession) ([]DriverEnrollmentInvitation, error) {
	if s == nil || s.Store == nil || len(sessionToken) != 32 || session.SessionPolicy != AdministrativeSession {
		return nil, ErrDriverInvitationInvalid
	}
	return s.Store.ListDriverEnrollmentInvitations(ctx, HashSessionToken(sessionToken))
}

func (s *DriverEnrollmentInvitationService) Candidates(ctx context.Context, sessionToken []byte, session AuthenticatedSession, companyID uuid.UUID, operationID *uuid.UUID) ([]DriverEnrollmentCandidate, error) {
	if s == nil || s.Store == nil || len(sessionToken) != 32 || session.SessionPolicy != AdministrativeSession || companyID == uuid.Nil {
		return nil, ErrDriverInvitationInvalid
	}
	return s.Store.ListDriverEnrollmentCandidates(ctx, HashSessionToken(sessionToken), companyID, operationID)
}

func (s *DriverEnrollmentInvitationService) Revoke(ctx context.Context, sessionToken []byte, session AuthenticatedSession, invitationID uuid.UUID, reason string) error {
	if s == nil || s.Store == nil || len(sessionToken) != 32 || session.SessionPolicy != AdministrativeSession || invitationID == uuid.Nil {
		return ErrDriverInvitationInvalid
	}
	if err := s.Store.RevokeDriverEnrollmentInvitation(ctx, HashSessionToken(sessionToken), invitationID, reason); err != nil {
		return ErrDriverInvitationInvalid
	}
	return nil
}

func (s *DriverEnrollmentInvitationService) Resolve(ctx context.Context, token []byte) (DriverEnrollmentInvitationPreview, error) {
	if s == nil || s.Store == nil || len(token) != 32 {
		return DriverEnrollmentInvitationPreview{}, ErrDriverInvitationInvalid
	}
	preview, err := s.Store.ResolveDriverEnrollmentInvitation(ctx, HashSessionToken(token))
	if err != nil {
		return DriverEnrollmentInvitationPreview{}, ErrDriverInvitationInvalid
	}
	return preview, nil
}

func (s *DriverEnrollmentInvitationService) Consume(ctx context.Context, input DriverEnrollmentConsumeInput) (DriverEnrollmentActivation, error) {
	if s == nil || s.Store == nil || len(input.Token) != 32 || input.InstallationID == uuid.Nil || len(input.InstallationPublicKey) < 16 || strings.TrimSpace(input.KeyAlgorithm) == "" || len(input.ConfirmationCode) != 8 {
		return DriverEnrollmentActivation{}, ErrDriverInvitationInvalid
	}
	for _, char := range input.ConfirmationCode {
		if char < '0' || char > '9' {
			return DriverEnrollmentActivation{}, ErrDriverInvitationInvalid
		}
	}
	sessionID := uuid.New()
	token, err := randomBytes(32)
	if err != nil {
		return DriverEnrollmentActivation{}, err
	}
	csrf, err := randomBytes(32)
	if err != nil {
		return DriverEnrollmentActivation{}, err
	}
	requestID := uuid.New()
	correlation := uuid.NewString()
	bindingInput := append([]byte("tp-device-binding\x00"), sessionID[:]...)
	bindingInput = append(bindingInput, input.InstallationID[:]...)
	binding := sha256.Sum256(bindingInput)
	activation, err := s.Store.ConsumeDriverEnrollmentInvitation(ctx, DriverEnrollmentConsumeRequest{
		TokenHash: HashSessionToken(input.Token), ConfirmationHash: HashSessionToken([]byte(input.ConfirmationCode)),
		SessionID: sessionID, SessionTokenHash: HashSessionToken(token), CSRFSecretHash: HashSessionToken(csrf),
		UserAgentHash: digestBytes(input.UserAgent), IPPrefix: privacyIPPrefix(input.RemoteAddr),
		RequestID: requestID, CorrelationCode: correlation, InstallationID: input.InstallationID,
		InstallationPublicKey: append([]byte(nil), input.InstallationPublicKey...), KeyAlgorithm: strings.TrimSpace(input.KeyAlgorithm),
		BindingHash: binding[:], InvitationAuditID: uuid.New(), RequestAuditID: uuid.New(),
		SessionAuditID: uuid.New(), LoginAuditID: uuid.New(),
	})
	if err != nil || activation.Session.ID == uuid.Nil || activation.Request.ID == uuid.Nil {
		return DriverEnrollmentActivation{}, ErrDriverInvitationInvalid
	}
	activation.Token = token
	activation.CSRFToken = csrf
	return activation, nil
}

func randomConfirmationCode() (string, error) {
	limit := big.NewInt(100000000)
	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%08d", value.Int64()), nil
}
