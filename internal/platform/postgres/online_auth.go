package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/tenancy"
)

// OnlineAuthRepository is intentionally split by privilege. Methods that
// record the result of application-level password verification use
// tp_runtime; methods that consume grants or resolve authentication state use
// tp_auth_runtime. Neither pool is a migration or superuser pool.
type OnlineAuthRepository struct {
	runtimePool   *pgxpool.Pool
	authPool      *pgxpool.Pool
	bootstrapPool *pgxpool.Pool
}

func NewOnlineAuthRepository(runtimePool, authPool *pgxpool.Pool) (*OnlineAuthRepository, error) {
	if runtimePool == nil || authPool == nil {
		return nil, errors.New("runtime and authentication pools are required")
	}
	return &OnlineAuthRepository{runtimePool: runtimePool, authPool: authPool}, nil
}

// NewBootstrapAuthRepository is used only by the explicit bootstrap command.
// It deliberately has no runtime or authentication pool, preventing the
// command from accidentally reusing the HTTP server's database boundaries.
func NewBootstrapAuthRepository(bootstrapPool *pgxpool.Pool) (*OnlineAuthRepository, error) {
	if bootstrapPool == nil {
		return nil, errors.New("bootstrap pool is required")
	}
	return &OnlineAuthRepository{bootstrapPool: bootstrapPool}, nil
}

func (r *OnlineAuthRepository) ResolveLoginCredential(ctx context.Context, identifier string) (auth.LoginCredential, error) {
	if r == nil || r.authPool == nil || strings.TrimSpace(identifier) == "" {
		return auth.LoginCredential{}, auth.ErrInvalidInput
	}
	verificationNonce := make([]byte, 32)
	if _, err := rand.Read(verificationNonce); err != nil {
		return auth.LoginCredential{}, err
	}
	verificationID := uuid.New()
	var result auth.LoginCredential
	var parameters []byte
	err := r.authPool.QueryRow(ctx, `
		SELECT verification_id, user_account_id, person_id, password_credential_id,
		       password_hash, kdf_name, kdf_parameters
		FROM platform.resolve_login_credential($1, $2, $3)
	`, strings.ToLower(strings.TrimSpace(identifier)), verificationID, auth.HashSessionToken(verificationNonce)).Scan(
		&result.VerificationID, &result.UserAccountID, &result.PersonID, &result.PasswordCredentialID,
		&result.PasswordHash, &result.KDFName, &parameters,
	)
	if err != nil {
		return auth.LoginCredential{}, err
	}
	result.KDFParameters = json.RawMessage(parameters)
	result.VerificationNonce = verificationNonce
	return result, nil
}

func (r *OnlineAuthRepository) ConsumeRateLimit(ctx context.Context, keyHash []byte, scope string, limit, windowSeconds int) (bool, error) {
	if r == nil || r.authPool == nil || len(keyHash) != 32 || strings.TrimSpace(scope) == "" {
		return false, auth.ErrInvalidInput
	}
	var allowed bool
	err := r.authPool.QueryRow(ctx, `
		SELECT platform.consume_auth_rate_limit($1, $2, $3, $4)
	`, keyHash, strings.TrimSpace(scope), limit, windowSeconds).Scan(&allowed)
	return allowed, err
}

func (r *OnlineAuthRepository) RecordVerifiedPasswordLogin(ctx context.Context, grant auth.LoginGrant, expiresSeconds int) error {
	if r == nil || r.runtimePool == nil || grant.ID == uuid.Nil || grant.VerificationID == uuid.Nil || len(grant.VerificationNonceHash) != 32 || len(grant.GrantHash) != 32 {
		return auth.ErrInvalidInput
	}
	var id uuid.UUID
	err := r.runtimePool.QueryRow(ctx, `
		SELECT platform.record_verified_password_login($1, $2, $3, $4, $5)
	`, grant.VerificationID, grant.VerificationNonceHash, grant.ID, grant.GrantHash, expiresSeconds).Scan(&id)
	return err
}

func (r *OnlineAuthRepository) IssueAuthSession(ctx context.Context, grant auth.LoginGrant, request auth.CreateSessionRequest, auditID uuid.UUID) (auth.SessionRecord, error) {
	if r == nil || r.authPool == nil || grant.ID == uuid.Nil || len(grant.GrantHash) != 32 || auditID == uuid.Nil {
		return auth.SessionRecord{}, auth.ErrInvalidInput
	}
	if err := validateSessionRequest(request); err != nil {
		return auth.SessionRecord{}, err
	}
	var record auth.SessionRecord
	err := r.authPool.QueryRow(ctx, `
		SELECT session_id, user_account_id, session_epoch, status,
		       created_at, last_seen_at, idle_expires_at, absolute_expires_at,
		       active_company_id, active_operation_id, context_version
		FROM platform.issue_auth_session_from_verified_login(
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
	`, grant.GrantHash, request.ID, auditID, uuid.New(), request.TokenHash,
		nullableBytes(request.CSRFSecretHash), "ADMINISTRATIVE",
		request.ActiveCompanyID, request.ActiveOperationID,
		request.AuthenticatorCredentialID, request.DeviceAuthorizationID,
		nullableBytes(request.UserAgentHash), request.IPPrefix).Scan(
		&record.ID, &record.UserAccountID, &record.SessionEpoch, &record.Status,
		&record.CreatedAt, &record.LastSeenAt, &record.IdleExpiresAt,
		&record.AbsoluteExpiresAt, &record.ActiveCompanyID,
		&record.ActiveOperationID, &record.ContextVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SessionRecord{}, auth.ErrInvalidSession
	}
	return record, err
}

func (r *OnlineAuthRepository) ResolveHTTPSession(ctx context.Context, tokenHash []byte) (auth.AuthenticatedSession, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 {
		return auth.AuthenticatedSession{}, auth.ErrInvalidSession
	}
	var result auth.AuthenticatedSession
	var policy string
	err := r.authPool.QueryRow(ctx, `
		SELECT session_id, user_account_id, person_id, session_epoch,
		       context_version, active_company_id, active_operation_id,
		       session_policy, csrf_secret_hash, status, idle_expires_at,
		       absolute_expires_at, created_at, last_seen_at
		FROM platform.resolve_auth_session_http($1)
	`, tokenHash).Scan(
		&result.ID, &result.UserAccountID, &result.PersonID, &result.SessionEpoch,
		&result.ContextVersion, &result.ActiveCompanyID, &result.ActiveOperationID,
		&policy, &result.CSRFSecretHash, &result.Status, &result.IdleExpiresAt,
		&result.AbsoluteExpiresAt, &result.CreatedAt, &result.LastSeenAt,
	)
	if err != nil {
		return auth.AuthenticatedSession{}, err
	}
	result.SessionPolicy = auth.SessionKind(policy)
	return result, nil
}

func (r *OnlineAuthRepository) ListAuthSessions(ctx context.Context, tokenHash []byte) ([]auth.SessionSummary, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 {
		return nil, auth.ErrInvalidSession
	}
	rows, err := r.authPool.Query(ctx, `
		SELECT session_id, status, created_at, last_seen_at,
		       idle_expires_at, absolute_expires_at, active_company_id,
		       active_operation_id, session_policy, is_current
		FROM platform.list_auth_sessions($1)
	`, tokenHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []auth.SessionSummary
	for rows.Next() {
		var item auth.SessionSummary
		var policy string
		if err := rows.Scan(&item.ID, &item.Status, &item.CreatedAt, &item.LastSeenAt,
			&item.IdleExpiresAt, &item.AbsoluteExpiresAt, &item.ActiveCompanyID,
			&item.ActiveOperationID, &policy, &item.Current); err != nil {
			return nil, err
		}
		item.SessionPolicy = auth.SessionKind(policy)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *OnlineAuthRepository) RevokeSession(ctx context.Context, sessionID uuid.UUID, tokenHash []byte, reason string) error {
	if r == nil || r.authPool == nil {
		return auth.ErrInvalidInput
	}
	return r.withAuthTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var changed bool
		if err := tx.QueryRow(ctx, `SELECT platform.revoke_auth_session($1, $2, $3, $4)`, sessionID, uuid.New(), tokenHash, reason).Scan(&changed); err != nil {
			return err
		}
		if !changed {
			return pgx.ErrNoRows
		}
		return nil
	})
}

func (r *OnlineAuthRepository) RevokeAllSessions(ctx context.Context, tokenHash []byte, reason string) error {
	if r == nil || r.authPool == nil {
		return auth.ErrInvalidInput
	}
	return r.withAuthTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var changed int
		if err := tx.QueryRow(ctx, `SELECT platform.revoke_all_auth_sessions($1, $2, $3)`, uuid.New(), tokenHash, reason).Scan(&changed); err != nil {
			return err
		}
		if changed == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
}

func (r *OnlineAuthRepository) SetAuthSessionContext(ctx context.Context, tokenHash []byte, auditID uuid.UUID, companyID, operationID *uuid.UUID) (int64, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || auditID == uuid.Nil {
		return 0, auth.ErrInvalidInput
	}
	var version int64
	err := r.authPool.QueryRow(ctx, `
		SELECT context_version
		FROM platform.set_auth_session_context($1, $2, $3, $4)
	`, tokenHash, auditID, companyID, operationID).Scan(&version)
	return version, err
}

func (r *OnlineAuthRepository) CreateAdminInvitation(ctx context.Context, sessionTokenHash []byte, invitationID uuid.UUID, tokenHash []byte, identifier, legalName string, expiresSeconds int) (auth.Invitation, error) {
	if r == nil || r.runtimePool == nil || len(sessionTokenHash) != 32 || invitationID == uuid.Nil || len(tokenHash) != 32 {
		return auth.Invitation{}, auth.ErrInvalidInput
	}
	var result auth.Invitation
	err := r.runtimePool.QueryRow(ctx, `
		SELECT invitation_id, expires_at
		FROM platform.create_admin_invitation($1::bytea, $2::uuid, $3::bytea, $4::text, $5::text, $6::integer)
	`, sessionTokenHash, invitationID, tokenHash, identifier, legalName, expiresSeconds).Scan(&result.ID, &result.ExpiresAt)
	return result, err
}

func (r *OnlineAuthRepository) ConsumeAdminInvitation(ctx context.Context, tokenHash []byte, passwordHash string, parameters json.RawMessage, personID, accountID, credentialID, auditID uuid.UUID) (uuid.UUID, uuid.UUID, string, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 {
		return uuid.Nil, uuid.Nil, "", auth.ErrInvalidInput
	}
	var result struct {
		AccountID  uuid.UUID
		PersonID   uuid.UUID
		Identifier string
	}
	err := r.authPool.QueryRow(ctx, `
		SELECT user_account_id, person_id, normalized_identifier
		FROM platform.consume_admin_invitation($1, $2, $3, $4, $5, $6, $7)
	`, tokenHash, passwordHash, parameters, personID, accountID, credentialID, auditID).Scan(&result.AccountID, &result.PersonID, &result.Identifier)
	return result.AccountID, result.PersonID, result.Identifier, err
}

func (r *OnlineAuthRepository) PrepareBootstrapRequest(ctx context.Context, requestID uuid.UUID, requestHash, secretHash []byte, identifier, legalName, passwordHash string, parameters json.RawMessage, expiresSeconds int) error {
	if r == nil || r.bootstrapPool == nil || requestID == uuid.Nil {
		return auth.ErrInvalidInput
	}
	var id uuid.UUID
	err := r.bootstrapPool.QueryRow(ctx, `
		SELECT platform.prepare_bootstrap_request($1, $2, $3, $4, $5, $6, $7, $8)
	`, requestID, requestHash, secretHash, identifier, legalName, passwordHash, parameters, expiresSeconds).Scan(&id)
	return err
}

func (r *OnlineAuthRepository) CompleteBootstrapRequest(ctx context.Context, requestHash, secretHash []byte, personID, accountID, credentialID, roleAssignmentID, auditID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if r == nil || r.bootstrapPool == nil {
		return uuid.Nil, uuid.Nil, auth.ErrInvalidInput
	}
	var result struct{ AccountID, PersonID uuid.UUID }
	err := r.bootstrapPool.QueryRow(ctx, `
		SELECT user_account_id, person_id
		FROM platform.complete_bootstrap_request($1, $2, $3, $4, $5, $6, $7)
	`, requestHash, secretHash, personID, accountID, credentialID, roleAssignmentID, auditID).Scan(&result.AccountID, &result.PersonID)
	return result.AccountID, result.PersonID, err
}

func (r *OnlineAuthRepository) RecordLoginFailure(ctx context.Context, auditID uuid.UUID, reason string) error {
	if r == nil || r.authPool == nil || auditID == uuid.Nil {
		return auth.ErrInvalidInput
	}
	_, err := r.authPool.Exec(ctx, `SELECT platform.record_auth_login_failure($1, $2)`, auditID, reason)
	return err
}

func (r *OnlineAuthRepository) withAuthTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return WithTenantTx(ctx, r.authPool, tenancy.Platform(), fn)
}
