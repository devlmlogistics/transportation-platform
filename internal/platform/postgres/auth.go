package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/tenancy"
)

var (
	ErrAuthRepositoryUnavailable = errors.New("authentication repository requires a pool")
	ErrSecurityAuditInvalid      = errors.New("invalid security audit event")
)

// AuthRepository contains only persistence contracts for 1B.2-A. It does
// not authenticate HTTP requests, issue browser sessions, or perform WebAuthn
// ceremonies.
type AuthRepository struct {
	Pool  *pgxpool.Pool
	Clock func() time.Time
}

func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{
		Pool:  pool,
		Clock: func() time.Time { return time.Now().UTC() },
	}
}

func HashSessionToken(token []byte) []byte {
	hash := sha256.Sum256(token)
	return hash[:]
}

func (r *AuthRepository) CreateSession(ctx context.Context, request auth.CreateSessionRequest) (auth.SessionRecord, error) {
	if err := validateSessionRequest(request); err != nil {
		return auth.SessionRecord{}, err
	}
	if err := r.requirePool(); err != nil {
		return auth.SessionRecord{}, err
	}
	now := r.now()
	idleExpiresAt := now.Add(request.Policy.IdleTTL)
	absoluteExpiresAt := now.Add(request.Policy.AbsoluteTTL)
	if idleExpiresAt.After(absoluteExpiresAt) {
		idleExpiresAt = absoluteExpiresAt
	}

	var record auth.SessionRecord
	err := r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var sessionEpoch int64
		if err := tx.QueryRow(ctx, `
			SELECT ua.session_epoch
			FROM platform.user_account ua
			JOIN platform.person p ON p.id = ua.person_id
			WHERE ua.id = $1
			  AND ua.status = 'ACTIVE'
			  AND p.status = 'ACTIVE'
			FOR UPDATE
		`, request.UserAccountID).Scan(&sessionEpoch); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return auth.ErrInactiveAccount
			}
			return err
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO platform.auth_session (
				id, user_account_id, token_hash, csrf_secret_hash, status,
				created_at, last_seen_at, idle_expires_at, absolute_expires_at,
				session_epoch, context_version, active_company_id, active_operation_id,
				authenticator_credential_id, device_authorization_id, user_agent_hash, ip_prefix
			)
			VALUES ($1, $2, $3, $4, 'ACTIVE', $5, $5, $6, $7, $8, 0,
				$9, $10, $11, $12, $13, NULLIF($14, ''))
		`, request.ID, request.UserAccountID, request.TokenHash, nullableBytes(request.CSRFSecretHash),
			now, idleExpiresAt, absoluteExpiresAt, sessionEpoch,
			request.ActiveCompanyID, request.ActiveOperationID,
			request.AuthenticatorCredentialID, request.DeviceAuthorizationID,
			nullableBytes(request.UserAgentHash), request.IPPrefix)
		if err != nil {
			return err
		}

		if err := writeSecurityAuditEventTx(ctx, tx, auth.SecurityAuditEvent{
			ID:                   uuid.New(),
			EventType:            "SESSION_CREATED",
			Outcome:              "SUCCESS",
			SubjectUserAccountID: uuidPointer(request.UserAccountID),
			Details:              []byte(`{}`),
		}); err != nil {
			return err
		}

		record = auth.SessionRecord{
			ID:                request.ID,
			UserAccountID:     request.UserAccountID,
			SessionEpoch:      sessionEpoch,
			Status:            "ACTIVE",
			CreatedAt:         now,
			LastSeenAt:        now,
			IdleExpiresAt:     idleExpiresAt,
			AbsoluteExpiresAt: absoluteExpiresAt,
			ActiveCompanyID:   request.ActiveCompanyID,
			ActiveOperationID: request.ActiveOperationID,
			ContextVersion:    0,
		}
		return nil
	})
	return record, err
}

func (r *AuthRepository) ResolveSession(ctx context.Context, tokenHash []byte) (auth.ResolvedSession, error) {
	if len(tokenHash) != sha256.Size {
		return auth.ResolvedSession{}, auth.ErrInvalidSession
	}
	if err := r.requirePool(); err != nil {
		return auth.ResolvedSession{}, err
	}
	var record auth.ResolvedSession
	err := r.Pool.QueryRow(ctx, `
		SELECT session_id, user_account_id, person_id, session_epoch,
		       context_version, active_company_id, active_operation_id,
		       authenticator_credential_id, device_authorization_id, status,
		       idle_expires_at, absolute_expires_at, revoked_at, expired_at,
		       compromised_at
		FROM platform.resolve_auth_session($1)
	`, tokenHash).Scan(
		&record.ID, &record.UserAccountID, &record.PersonID, &record.SessionEpoch,
		&record.ContextVersion, &record.ActiveCompanyID, &record.ActiveOperationID,
		&record.AuthenticatorCredentialID, &record.DeviceAuthorizationID, &record.Status,
		&record.IdleExpiresAt, &record.AbsoluteExpiresAt, &record.RevokedAt,
		&record.ExpiredAt, &record.CompromisedAt,
	)
	if err != nil {
		return auth.ResolvedSession{}, err
	}
	return record, nil
}

func (r *AuthRepository) RevokeSession(ctx context.Context, userAccountID, sessionID uuid.UUID, reason string, at time.Time) error {
	if userAccountID == uuid.Nil || sessionID == uuid.Nil || strings.TrimSpace(reason) == "" {
		return auth.ErrInvalidInput
	}
	if err := r.requirePool(); err != nil {
		return err
	}
	if at.IsZero() {
		at = r.now()
	}
	return r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var changed bool
		err := tx.QueryRow(ctx, `
			SELECT platform.revoke_auth_session($1, $2, $3, $4)
		`, sessionID, userAccountID, at.UTC(), strings.TrimSpace(reason)).Scan(&changed)
		if err != nil {
			return err
		}
		if !changed {
			return pgx.ErrNoRows
		}
		return writeSecurityAuditEventTx(ctx, tx, auth.SecurityAuditEvent{
			ID:                   uuid.New(),
			EventType:            "SESSION_REVOKED",
			Outcome:              "SUCCESS",
			SubjectUserAccountID: uuidPointer(userAccountID),
			Details:              []byte(`{}`),
		})
	})
}

func (r *AuthRepository) RevokeAllSessions(ctx context.Context, userAccountID uuid.UUID, reason string, at time.Time) error {
	if userAccountID == uuid.Nil || strings.TrimSpace(reason) == "" {
		return auth.ErrInvalidInput
	}
	if err := r.requirePool(); err != nil {
		return err
	}
	if at.IsZero() {
		at = r.now()
	}
	return r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var currentEpoch int64
		if err := tx.QueryRow(ctx, `
			SELECT session_epoch
			FROM platform.user_account
			WHERE id = $1
			FOR UPDATE
		`, userAccountID).Scan(&currentEpoch); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE platform.user_account
			SET session_epoch = session_epoch + 1, updated_at = $2
			WHERE id = $1
		`, userAccountID, at.UTC()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			SELECT platform.revoke_all_auth_sessions($1, $2, $3)
		`, userAccountID, at.UTC(), strings.TrimSpace(reason)); err != nil {
			return err
		}
		return writeSecurityAuditEventTx(ctx, tx, auth.SecurityAuditEvent{
			ID:                   uuid.New(),
			EventType:            "SESSION_REVOKED_ALL",
			Outcome:              "SUCCESS",
			SubjectUserAccountID: uuidPointer(userAccountID),
			Details:              []byte(fmt.Sprintf(`{"previous_session_epoch":%d}`, currentEpoch)),
		})
	})
}

func (r *AuthRepository) CreateChallenge(ctx context.Context, request auth.CreateChallengeRequest) error {
	if err := validateChallengeRequest(request); err != nil {
		return err
	}
	if err := r.requirePool(); err != nil {
		return err
	}
	return r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO platform.auth_challenge (
				id, purpose, user_account_id, device_enrollment_request_id,
				challenge_hash, session_data, session_data_format, binding_hash,
				expires_at, request_fingerprint
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, request.ID, string(request.Purpose), request.UserAccountID,
			request.DeviceEnrollmentRequestID, request.ChallengeHash, request.SessionData,
			request.SessionDataFormat, request.BindingHash, request.ExpiresAt.UTC(),
			nullableBytes(request.RequestFingerprint))
		if err != nil {
			return err
		}
		return writeSecurityAuditEventTx(ctx, tx, auth.SecurityAuditEvent{
			ID:                   uuid.New(),
			EventType:            "CHALLENGE_CREATED",
			Outcome:              "SUCCESS",
			SubjectUserAccountID: request.UserAccountID,
			Details:              []byte(`{"purpose":"server_defined"}`),
		})
	})
}

func (r *AuthRepository) ConsumeChallenge(ctx context.Context, id uuid.UUID, purpose auth.ChallengePurpose, challengeHash, bindingHash []byte) (auth.ConsumedChallenge, error) {
	if id == uuid.Nil || strings.TrimSpace(string(purpose)) == "" || len(challengeHash) != sha256.Size || len(bindingHash) != sha256.Size {
		return auth.ConsumedChallenge{}, auth.ErrInvalidInput
	}
	if err := r.requirePool(); err != nil {
		return auth.ConsumedChallenge{}, err
	}
	var result auth.ConsumedChallenge
	err := r.Pool.QueryRow(ctx, `
		SELECT challenge_id, user_account_id, device_enrollment_request_id,
		       session_data, session_data_format
		FROM platform.consume_auth_challenge($1, $2, $3, $4)
	`, id, string(purpose), challengeHash, bindingHash).Scan(
		&result.ID, &result.UserAccountID, &result.DeviceEnrollmentRequestID,
		&result.SessionData, &result.SessionDataFormat,
	)
	if err != nil {
		return auth.ConsumedChallenge{}, err
	}
	return result, nil
}

func (r *AuthRepository) WriteSecurityAudit(ctx context.Context, event auth.SecurityAuditEvent) error {
	if err := r.requirePool(); err != nil {
		return err
	}
	return r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return writeSecurityAuditEventTx(ctx, tx, event)
	})
}

func writeSecurityAuditEventTx(ctx context.Context, tx pgx.Tx, event auth.SecurityAuditEvent) error {
	if event.ID == uuid.Nil || strings.TrimSpace(event.EventType) == "" {
		return ErrSecurityAuditInvalid
	}
	if event.Outcome != "SUCCESS" && event.Outcome != "FAILURE" && event.Outcome != "DENIED" {
		return ErrSecurityAuditInvalid
	}
	if event.CompanyOperationID != nil && event.CompanyID == nil {
		return ErrSecurityAuditInvalid
	}
	details := event.Details
	if len(details) == 0 {
		details = []byte(`{}`)
	}
	var object map[string]any
	if len(details) > 8192 || json.Unmarshal(details, &object) != nil || object == nil {
		return ErrSecurityAuditInvalid
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO platform.security_audit_event (
			id, event_type, outcome, actor_user_account_id,
			subject_user_account_id, company_id, company_operation_id,
			reason_code, correlation_id, details
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
	`, event.ID, strings.TrimSpace(event.EventType), event.Outcome,
		event.ActorUserAccountID, event.SubjectUserAccountID, event.CompanyID,
		event.CompanyOperationID, event.ReasonCode, event.CorrelationID, details)
	return err
}

func (r *AuthRepository) withPlatformTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return WithTenantTx(ctx, r.Pool, tenancy.Platform(), fn)
}

func (r *AuthRepository) requirePool() error {
	if r == nil || r.Pool == nil {
		return ErrAuthRepositoryUnavailable
	}
	return nil
}

func (r *AuthRepository) now() time.Time {
	if r.Clock == nil {
		return time.Now().UTC()
	}
	return r.Clock().UTC()
}

func validateSessionRequest(request auth.CreateSessionRequest) error {
	if request.ID == uuid.Nil || request.UserAccountID == uuid.Nil || len(request.TokenHash) != sha256.Size {
		return auth.ErrInvalidInput
	}
	if request.CSRFSecretHash != nil && len(request.CSRFSecretHash) != sha256.Size {
		return auth.ErrInvalidInput
	}
	if request.Policy.IdleTTL <= 0 || request.Policy.AbsoluteTTL <= 0 || request.Policy.IdleTTL > request.Policy.AbsoluteTTL {
		return auth.ErrInvalidInput
	}
	if request.ActiveOperationID != nil && request.ActiveCompanyID == nil {
		return auth.ErrInvalidInput
	}
	return nil
}

func validateChallengeRequest(request auth.CreateChallengeRequest) error {
	if request.ID == uuid.Nil || strings.TrimSpace(string(request.Purpose)) == "" ||
		len(request.ChallengeHash) != sha256.Size || len(request.BindingHash) != sha256.Size ||
		len(request.SessionData) == 0 || strings.TrimSpace(request.SessionDataFormat) == "" || request.ExpiresAt.IsZero() {
		return auth.ErrInvalidInput
	}
	if request.RequestFingerprint != nil && len(request.RequestFingerprint) > 128 {
		return auth.ErrInvalidInput
	}
	return nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func uuidPointer(value uuid.UUID) *uuid.UUID {
	return &value
}
