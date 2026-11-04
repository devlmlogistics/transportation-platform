package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"transportation-platform/internal/application/auth"
	"transportation-platform/internal/platform/tenancy"
)

// OnlineAuthRepository is intentionally split by privilege. Methods that
// record the result of application-level password verification use
// tp_runtime; ordinary authentication state uses tp_auth_runtime; the final
// DRIVER session issuance boundary uses tp_driver_auth_runtime. Neither pool
// is a migration or superuser pool.
type OnlineAuthRepository struct {
	runtimePool    *pgxpool.Pool
	authPool       *pgxpool.Pool
	driverAuthPool *pgxpool.Pool
	bootstrapPool  *pgxpool.Pool
}

func NewOnlineAuthRepository(runtimePool, authPool, driverAuthPool *pgxpool.Pool) (*OnlineAuthRepository, error) {
	if runtimePool == nil || authPool == nil || driverAuthPool == nil {
		return nil, errors.New("runtime, authentication and driver-auth pools are required")
	}
	return &OnlineAuthRepository{runtimePool: runtimePool, authPool: authPool, driverAuthPool: driverAuthPool}, nil
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

func (r *OnlineAuthRepository) IssueDriverActivationSession(ctx context.Context, grant auth.LoginGrant, request auth.CreateSessionRequest, auditID uuid.UUID) (auth.SessionRecord, error) {
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
		FROM platform.issue_driver_activation_session_from_verified_login(
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`, grant.GrantHash, request.ID, auditID, uuid.New(), request.TokenHash,
		nullableBytes(request.CSRFSecretHash), nullableBytes(request.UserAgentHash), request.IPPrefix).Scan(
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
		       session_policy, authentication_level, step_up_expires_at, step_up_credential_id,
		       csrf_secret_hash, status, idle_expires_at,
		       absolute_expires_at, created_at, last_seen_at
		FROM platform.resolve_auth_session_http($1)
	`, tokenHash).Scan(
		&result.ID, &result.UserAccountID, &result.PersonID, &result.SessionEpoch,
		&result.ContextVersion, &result.ActiveCompanyID, &result.ActiveOperationID,
		&policy, &result.AuthenticationLevel, &result.StepUpExpiresAt, &result.StepUpCredentialID,
		&result.CSRFSecretHash, &result.Status, &result.IdleExpiresAt,
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

func (r *OnlineAuthRepository) BeginWebAuthnChallenge(ctx context.Context, tokenHash []byte, request auth.WebAuthnChallengeRequest) error {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || request.ID == uuid.Nil || request.AuditID == uuid.Nil {
		return auth.ErrInvalidInput
	}
	if len(request.ChallengeHash) != 32 || len(request.BindingHash) != 32 || len(request.SessionData) == 0 || request.ExpiresAt.IsZero() {
		return auth.ErrInvalidInput
	}
	var id *uuid.UUID
	err := r.authPool.QueryRow(ctx, `
		SELECT platform.begin_webauthn_challenge($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, tokenHash, request.ID, string(request.Purpose), request.ChallengeHash, request.SessionData,
		request.SessionDataFormat, request.BindingHash, request.ExpiresAt.UTC(), nullableUUID(request.DeviceRequestID), request.AuditID).Scan(&id)
	if err != nil {
		return err
	}
	if id == nil || *id != request.ID {
		return auth.ErrInvalidSession
	}
	return nil
}

func (r *OnlineAuthRepository) GetWebAuthnChallenge(ctx context.Context, tokenHash []byte, id uuid.UUID, purpose auth.ChallengePurpose) (auth.WebAuthnChallenge, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || id == uuid.Nil {
		return auth.WebAuthnChallenge{}, auth.ErrInvalidInput
	}
	var result auth.WebAuthnChallenge
	result.ID = id
	var deviceID *uuid.UUID
	err := r.authPool.QueryRow(ctx, `
		SELECT challenge_hash,session_data,session_data_format,binding_hash,user_account_id,device_request_id
		FROM platform.get_webauthn_challenge($1,$2,$3)
	`, tokenHash, id, string(purpose)).Scan(&result.ChallengeHash, &result.SessionData,
		&result.SessionDataFormat, &result.BindingHash, &result.UserAccountID, &deviceID)
	if err != nil {
		return auth.WebAuthnChallenge{}, err
	}
	result.DeviceRequestID = deviceID
	return result, nil
}

func (r *OnlineAuthRepository) CompleteWebAuthnRegistration(ctx context.Context, tokenHash []byte, request auth.CompleteRegistrationRequest) (uuid.UUID, *uuid.UUID, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || request.ChallengeID == uuid.Nil || request.CredentialID == uuid.Nil || request.AuditID == uuid.Nil {
		return uuid.Nil, nil, auth.ErrInvalidInput
	}
	var credentialID uuid.UUID
	var deviceID *uuid.UUID
	err := r.authPool.QueryRow(ctx, `
		SELECT credential_id,device_authorization_id
		FROM platform.complete_webauthn_registration($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
	`, tokenHash, request.ChallengeID, request.ChallengeHash, request.BindingHash,
		request.CredentialID, request.CredentialKey, request.PublicKey, request.SignCount,
		request.Transports, request.BackupEligible, nullableString(request.BackupState), request.UserVerified,
		request.DisplayName, request.Metadata, request.DeviceAuthorizationID, request.AuditID).Scan(&credentialID, &deviceID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	return credentialID, deviceID, nil
}

func (r *OnlineAuthRepository) CompleteWebAuthnStepUp(ctx context.Context, tokenHash []byte, request auth.CompleteStepUpRequest) (time.Time, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || request.ChallengeID == uuid.Nil || request.AuditID == uuid.Nil {
		return time.Time{}, auth.ErrInvalidInput
	}
	var expires time.Time
	err := r.authPool.QueryRow(ctx, `
		SELECT platform.complete_webauthn_step_up($1,$2,$3,$4,$5,$6)
	`, tokenHash, request.ChallengeID, request.ChallengeHash, request.BindingHash, request.CredentialKey, request.AuditID).Scan(&expires)
	return expires, err
}

func (r *OnlineAuthRepository) ListAuthenticatorCredentials(ctx context.Context, tokenHash []byte) ([]auth.AuthenticatorCredentialRecord, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 {
		return nil, auth.ErrInvalidInput
	}
	rows, err := r.authPool.Query(ctx, `
		SELECT id,credential_id,public_key,sign_count,transports,display_name,status,backup_eligible,backup_state,user_verified_last,
		       created_at,last_used_at,revoked_at
		FROM platform.list_authenticator_credentials($1)
	`, tokenHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []auth.AuthenticatorCredentialRecord
	for rows.Next() {
		var item auth.AuthenticatorCredentialRecord
		var displayName *string
		if err := rows.Scan(&item.ID, &item.CredentialID, &item.PublicKey, &item.SignCount, &item.Transports,
			&displayName, &item.Status, &item.BackupEligible, &item.BackupState, &item.UserVerifiedLast,
			&item.CreatedAt, &item.LastUsedAt, &item.RevokedAt); err != nil {
			return nil, err
		}
		if displayName != nil {
			item.DisplayName = *displayName
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *OnlineAuthRepository) RevokeOwnAuthenticator(ctx context.Context, tokenHash []byte, credentialID uuid.UUID, reason string) error {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || credentialID == uuid.Nil {
		return auth.ErrInvalidInput
	}
	var changed bool
	err := r.authPool.QueryRow(ctx, `SELECT platform.revoke_own_authenticator($1,$2,$3,$4)`, tokenHash, credentialID, reason, uuid.New()).Scan(&changed)
	if err == nil && !changed {
		return pgx.ErrNoRows
	}
	return err
}

func (r *OnlineAuthRepository) CreateDeviceEnrollmentRequest(ctx context.Context, tokenHash []byte, input auth.DeviceEnrollmentRequestInput) (auth.DeviceEnrollmentRequest, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || input.ID == uuid.Nil || input.AuditID == uuid.Nil {
		return auth.DeviceEnrollmentRequest{}, auth.ErrInvalidInput
	}
	var result auth.DeviceEnrollmentRequest
	err := r.authPool.QueryRow(ctx, `
		SELECT request_id,correlation_code,company_id,company_operation_id,installation_id,status,expires_at
		FROM platform.create_device_enrollment_request($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, tokenHash, input.ID, input.CorrelationCode, input.CompanyID, nullableUUID(input.CompanyOperationID), input.InstallationID,
		input.InstallationPublicKey, input.KeyAlgorithm, input.BindingHash, input.ExpiresAt.UTC(), input.AuditID).Scan(
		&result.ID, &result.CorrelationCode, &result.CompanyID, &result.CompanyOperationID, &result.InstallationID, &result.Status, &result.ExpiresAt)
	return result, err
}

func (r *OnlineAuthRepository) ListDeviceEnrollmentRequests(ctx context.Context, tokenHash []byte) ([]auth.DeviceEnrollmentRequestView, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 {
		return nil, auth.ErrInvalidInput
	}
	rows, err := r.authPool.Query(ctx, `
		SELECT request_id, correlation_code, user_account_id, identifier, legal_name,
		       company_id, company_operation_id, installation_id, status, requested_at, expires_at
		  FROM platform.list_device_enrollment_requests($1)
		 ORDER BY requested_at DESC
	`, tokenHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]auth.DeviceEnrollmentRequestView, 0)
	for rows.Next() {
		var item auth.DeviceEnrollmentRequestView
		if err := rows.Scan(&item.ID, &item.CorrelationCode, &item.UserAccountID, &item.Identifier, &item.LegalName,
			&item.CompanyID, &item.CompanyOperationID, &item.InstallationID, &item.Status, &item.RequestedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *OnlineAuthRepository) ListDeviceAuthorizations(ctx context.Context, tokenHash []byte) ([]auth.DeviceAuthorizationView, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 {
		return nil, auth.ErrInvalidInput
	}
	rows, err := r.authPool.Query(ctx, `
		SELECT authorization_id, user_account_id, identifier, legal_name, company_id,
		       company_operation_id, installation_id, key_algorithm, status,
		       requested_at, approved_at, expires_at
		  FROM platform.list_device_authorizations($1)
		 ORDER BY requested_at DESC
	`, tokenHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]auth.DeviceAuthorizationView, 0)
	for rows.Next() {
		var item auth.DeviceAuthorizationView
		if err := rows.Scan(&item.ID, &item.UserAccountID, &item.Identifier, &item.LegalName, &item.CompanyID,
			&item.CompanyOperationID, &item.InstallationID, &item.KeyAlgorithm, &item.Status,
			&item.RequestedAt, &item.ApprovedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *OnlineAuthRepository) ApproveDeviceEnrollmentRequest(ctx context.Context, tokenHash []byte, requestID uuid.UUID, reason string) error {
	return r.changeDeviceRequest(ctx, "approve_device_enrollment_request", tokenHash, requestID, reason)
}

func (r *OnlineAuthRepository) RejectDeviceEnrollmentRequest(ctx context.Context, tokenHash []byte, requestID uuid.UUID, reason string) error {
	return r.changeDeviceRequest(ctx, "reject_device_enrollment_request", tokenHash, requestID, reason)
}

func (r *OnlineAuthRepository) RevokeDeviceAuthorization(ctx context.Context, tokenHash []byte, authorizationID uuid.UUID, reason string) error {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || authorizationID == uuid.Nil {
		return auth.ErrInvalidInput
	}
	var changed bool
	err := r.authPool.QueryRow(ctx, `SELECT platform.revoke_device_authorization($1,$2,$3,$4)`, tokenHash, authorizationID, reason, uuid.New()).Scan(&changed)
	if err == nil && !changed {
		return pgx.ErrNoRows
	}
	return err
}

func (r *OnlineAuthRepository) ListDriverLoginCredentials(ctx context.Context, identifier string, installationID uuid.UUID) ([]auth.DriverLoginCredential, error) {
	if r == nil || r.authPool == nil || strings.TrimSpace(identifier) == "" || installationID == uuid.Nil {
		return nil, auth.ErrInvalidInput
	}
	rows, err := r.authPool.Query(ctx, `
		SELECT user_account_id, device_authorization_id, company_id, company_operation_id,
		       installation_public_key, key_algorithm, credential_id, public_key,
		       sign_count, transports, backup_eligible, backup_state, user_verified_last
		FROM platform.list_driver_login_credentials($1, $2)
	`, strings.ToLower(strings.TrimSpace(identifier)), installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []auth.DriverLoginCredential
	for rows.Next() {
		var item auth.DriverLoginCredential
		var opID *uuid.UUID
		var backupState *string
		if err := rows.Scan(&item.UserAccountID, &item.DeviceAuthorizationID, &item.CompanyID, &opID,
			&item.InstallationPublicKey, &item.KeyAlgorithm, &item.Credential.CredentialID,
			&item.Credential.PublicKey, &item.Credential.SignCount, &item.Credential.Transports,
			&item.Credential.BackupEligible, &backupState, &item.Credential.UserVerifiedLast); err != nil {
			return nil, err
		}
		item.CompanyOperationID = opID
		item.Credential.BackupState = backupState
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *OnlineAuthRepository) ListDriverLoginCredentialsForChallenge(ctx context.Context, challengeID uuid.UUID) ([]auth.DriverLoginCredential, error) {
	if r == nil || r.authPool == nil || challengeID == uuid.Nil {
		return nil, auth.ErrInvalidInput
	}
	rows, err := r.authPool.Query(ctx, `
		SELECT user_account_id, device_authorization_id, company_id, company_operation_id,
		       installation_public_key, key_algorithm, credential_id, public_key,
		       sign_count, transports, backup_eligible, backup_state, user_verified_last
		FROM platform.list_driver_login_credentials_for_challenge($1)
	`, challengeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []auth.DriverLoginCredential
	for rows.Next() {
		var item auth.DriverLoginCredential
		var opID *uuid.UUID
		var backupState *string
		if err := rows.Scan(&item.UserAccountID, &item.DeviceAuthorizationID, &item.CompanyID, &opID,
			&item.InstallationPublicKey, &item.KeyAlgorithm, &item.Credential.CredentialID,
			&item.Credential.PublicKey, &item.Credential.SignCount, &item.Credential.Transports,
			&item.Credential.BackupEligible, &backupState, &item.Credential.UserVerifiedLast); err != nil {
			return nil, err
		}
		item.CompanyOperationID = opID
		item.Credential.BackupState = backupState
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *OnlineAuthRepository) ListDriverAuthorizedContexts(ctx context.Context, tokenHash []byte) ([]auth.DriverAuthorizedContext, error) {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 {
		return nil, auth.ErrInvalidInput
	}
	rows, err := r.authPool.Query(ctx, `
		SELECT company_id, company_operation_id, device_authorization_id, company_name, operation_status
		FROM platform.list_driver_authorized_contexts($1)
	`, tokenHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []auth.DriverAuthorizedContext
	for rows.Next() {
		var item auth.DriverAuthorizedContext
		if err := rows.Scan(&item.CompanyID, &item.CompanyOperationID, &item.DeviceAuthorizationID, &item.CompanyName, &item.OperationStatus); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *OnlineAuthRepository) BeginDriverLoginChallenge(ctx context.Context, request auth.DriverLoginChallengeRequest) error {
	if r == nil || r.authPool == nil || request.ChallengeID == uuid.Nil || request.DeviceAuthorizationID == uuid.Nil || request.InstallationID == uuid.Nil || request.AuditID == uuid.Nil {
		return auth.ErrInvalidInput
	}
	var id *uuid.UUID
	err := r.authPool.QueryRow(ctx, `
		SELECT platform.begin_driver_login_challenge($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, strings.ToLower(strings.TrimSpace(request.Identifier)), request.InstallationID, request.ChallengeID,
		request.DeviceAuthorizationID, request.ChallengeHash, request.SessionData, request.SessionDataFormat,
		request.BindingHash, request.ExpiresAt.UTC(), request.AuditID).Scan(&id)
	if err != nil {
		return err
	}
	if id == nil || *id != request.ChallengeID {
		return auth.ErrInvalidSession
	}
	return nil
}

func (r *OnlineAuthRepository) GetDriverLoginChallenge(ctx context.Context, id uuid.UUID) (auth.DriverLoginChallenge, error) {
	if r == nil || r.authPool == nil || id == uuid.Nil {
		return auth.DriverLoginChallenge{}, auth.ErrInvalidInput
	}
	var result auth.DriverLoginChallenge
	result.ID = id
	err := r.authPool.QueryRow(ctx, `
		SELECT challenge_hash, session_data, session_data_format, binding_hash,
		       user_account_id, device_authorization_id, installation_id, company_id, company_operation_id,
		       installation_public_key, key_algorithm
		FROM platform.get_driver_login_challenge($1)
	`, id).Scan(&result.ChallengeHash, &result.SessionData, &result.SessionDataFormat,
		&result.BindingHash, &result.UserAccountID, &result.DeviceAuthorizationID, &result.InstallationID,
		&result.CompanyID, &result.CompanyOperationID, &result.InstallationPublicKey, &result.KeyAlgorithm)
	return result, err
}

func (r *OnlineAuthRepository) CompleteDriverLogin(ctx context.Context, request auth.CompleteDriverLoginRequest) (auth.SessionRecord, error) {
	if r == nil || r.driverAuthPool == nil || request.ChallengeID == uuid.Nil || request.SessionID == uuid.Nil || request.AuditID == uuid.Nil || request.LoginAuditID == uuid.Nil {
		return auth.SessionRecord{}, auth.ErrInvalidInput
	}
	var record auth.SessionRecord
	err := r.driverAuthPool.QueryRow(ctx, `
		SELECT session_id, user_account_id, session_epoch, status,
		       created_at, last_seen_at, idle_expires_at, absolute_expires_at,
		       active_company_id, active_operation_id, context_version
		FROM platform.complete_driver_login($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, request.ChallengeID, request.ChallengeHash, request.BindingHash, request.CredentialKey,
		request.SignCount, request.SessionID, request.TokenHash, request.CSRFSecretHash,
		nullableBytes(request.UserAgentHash), request.IPPrefix, request.AuditID, request.LoginAuditID).Scan(
		&record.ID, &record.UserAccountID, &record.SessionEpoch, &record.Status,
		&record.CreatedAt, &record.LastSeenAt, &record.IdleExpiresAt, &record.AbsoluteExpiresAt,
		&record.ActiveCompanyID, &record.ActiveOperationID, &record.ContextVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.SessionRecord{}, auth.ErrInvalidSession
	}
	return record, err
}

func (r *OnlineAuthRepository) changeDeviceRequest(ctx context.Context, function string, tokenHash []byte, requestID uuid.UUID, reason string) error {
	if r == nil || r.authPool == nil || len(tokenHash) != 32 || requestID == uuid.Nil {
		return auth.ErrInvalidInput
	}
	var changed bool
	switch function {
	case "approve_device_enrollment_request", "reject_device_enrollment_request":
	default:
		return auth.ErrInvalidInput
	}
	err := r.authPool.QueryRow(ctx, `SELECT platform.`+function+`($1,$2,$3,$4)`, tokenHash, requestID, reason, uuid.New()).Scan(&changed)
	if err == nil && !changed {
		return pgx.ErrNoRows
	}
	return err
}

func (r *OnlineAuthRepository) withAuthTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return WithTenantTx(ctx, r.authPool, tenancy.Platform(), fn)
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil || *value == uuid.Nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}
