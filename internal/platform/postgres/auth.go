package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/tenancy"
)

var (
	ErrAuthRepositoryUnavailable   = errors.New("authentication repository requires a pool")
	ErrSecurityAuditBoundaryClosed = errors.New("generic authentication audit writer is closed")
)

// AuthRepository contains only persistence contracts for 1B.2-A. It is
// intentionally constructed only inside this package with a pool connected as
// tp_auth_runtime. It does not authenticate HTTP requests, issue browser
// sessions, or perform WebAuthn ceremonies.
type AuthRepository struct {
	pool *pgxpool.Pool
}

func newAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{
		pool: pool,
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
	var record auth.SessionRecord
	err := r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT session_id, user_account_id, session_epoch, status,
			       created_at, last_seen_at, idle_expires_at, absolute_expires_at,
			       active_company_id, active_operation_id, context_version
			FROM platform.create_auth_session(
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
			)
		`, request.ID, uuid.New(), request.UserAccountID, request.TokenHash,
			nullableBytes(request.CSRFSecretHash),
			int64(request.Policy.IdleTTL/time.Second),
			int64(request.Policy.AbsoluteTTL/time.Second),
			request.ActiveCompanyID, request.ActiveOperationID,
			request.AuthenticatorCredentialID, request.DeviceAuthorizationID,
			nullableBytes(request.UserAgentHash), request.IPPrefix).Scan(
			&record.ID, &record.UserAccountID, &record.SessionEpoch, &record.Status,
			&record.CreatedAt, &record.LastSeenAt, &record.IdleExpiresAt,
			&record.AbsoluteExpiresAt, &record.ActiveCompanyID,
			&record.ActiveOperationID, &record.ContextVersion,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return auth.ErrInactiveAccount
			}
			return err
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
	err := r.pool.QueryRow(ctx, `
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

func (r *AuthRepository) RevokeSession(ctx context.Context, sessionID uuid.UUID, tokenHash []byte, reason string) error {
	if sessionID == uuid.Nil || len(tokenHash) != sha256.Size || strings.TrimSpace(reason) == "" {
		return auth.ErrInvalidInput
	}
	if err := r.requirePool(); err != nil {
		return err
	}
	return r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var changed bool
		err := tx.QueryRow(ctx, `
			SELECT platform.revoke_auth_session($1, $2, $3, $4)
		`, sessionID, uuid.New(), tokenHash, strings.TrimSpace(reason)).Scan(&changed)
		if err != nil {
			return err
		}
		if !changed {
			return pgx.ErrNoRows
		}
		return nil
	})
}

func (r *AuthRepository) RevokeAllSessions(ctx context.Context, tokenHash []byte, reason string) error {
	if len(tokenHash) != sha256.Size || strings.TrimSpace(reason) == "" {
		return auth.ErrInvalidInput
	}
	if err := r.requirePool(); err != nil {
		return err
	}
	return r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var changed int
		if err := tx.QueryRow(ctx, `
			SELECT platform.revoke_all_auth_sessions($1, $2, $3)
		`, uuid.New(), tokenHash, strings.TrimSpace(reason)).Scan(&changed); err != nil {
			return err
		}
		if changed == 0 {
			return pgx.ErrNoRows
		}
		return nil
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
			SELECT platform.create_auth_challenge(
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
			)
		`, request.ID, uuid.New(), string(request.Purpose), request.UserAccountID,
			request.DeviceEnrollmentRequestID, request.ChallengeHash, request.SessionData,
			request.SessionDataFormat, request.BindingHash, request.ExpiresAt.UTC(),
			nullableBytes(request.RequestFingerprint))
		if err != nil {
			return err
		}
		return nil
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
	err := r.withPlatformTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT challenge_id, user_account_id, device_enrollment_request_id,
			       session_data, session_data_format
			FROM platform.consume_auth_challenge($1, $2, $3, $4, $5)
		`, id, uuid.New(), string(purpose), challengeHash, bindingHash).Scan(
			&result.ID, &result.UserAccountID, &result.DeviceEnrollmentRequestID,
			&result.SessionData, &result.SessionDataFormat,
		)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return auth.ConsumedChallenge{}, err
	}
	return result, nil
}

func (r *AuthRepository) WriteSecurityAudit(ctx context.Context, event auth.SecurityAuditEvent) error {
	return ErrSecurityAuditBoundaryClosed
}

func (r *AuthRepository) withPlatformTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return WithTenantTx(ctx, r.pool, tenancy.Platform(), fn)
}

func (r *AuthRepository) requirePool() error {
	if r == nil || r.pool == nil {
		return ErrAuthRepositoryUnavailable
	}
	return nil
}

func validateSessionRequest(request auth.CreateSessionRequest) error {
	if request.ID == uuid.Nil || request.UserAccountID == uuid.Nil || len(request.TokenHash) != sha256.Size {
		return auth.ErrInvalidInput
	}
	if request.CSRFSecretHash != nil && len(request.CSRFSecretHash) != sha256.Size {
		return auth.ErrInvalidInput
	}
	if request.Policy.IdleTTL < time.Second || request.Policy.AbsoluteTTL < time.Second ||
		request.Policy.IdleTTL > request.Policy.AbsoluteTTL ||
		request.Policy.IdleTTL/time.Second <= 0 || request.Policy.AbsoluteTTL/time.Second <= 0 {
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
