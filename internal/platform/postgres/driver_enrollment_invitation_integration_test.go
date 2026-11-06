package postgres

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationauth "transportation-platform/internal/application/auth"
)

func testDriverEnrollmentInvitations(t *testing.T, ctx context.Context, repository *OnlineAuthRepository, authPool *pgxpool.Pool, adminDSN string, fixture foundationFixture) {
	t.Helper()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer adminPool.Close()
	createDriverProfile(t, ctx, adminPool, fixture.PersonADriver)
	service, err := applicationauth.NewDriverEnrollmentInvitationService(repository)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := applicationauth.PolicyFor(applicationauth.AdministrativeSession)
	if err != nil {
		t.Fatal(err)
	}
	supervisorToken := makeHash(0xE1)
	sessionRecord, err := createSyntheticSession(ctx, adminPool, applicationauth.CreateSessionRequest{
		ID: uuid.New(), UserAccountID: fixture.UserASupervisor,
		TokenHash: applicationauth.HashSessionToken(supervisorToken), Policy: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	supervisorSession := applicationauth.AuthenticatedSession{
		ID: sessionRecord.ID, UserAccountID: fixture.UserASupervisor, PersonID: fixture.PersonASupervisor,
		SessionPolicy: applicationauth.AdministrativeSession, Status: "ACTIVE",
		CreatedAt: sessionRecord.CreatedAt, LastSeenAt: sessionRecord.LastSeenAt,
		IdleExpiresAt: sessionRecord.IdleExpiresAt, AbsoluteExpiresAt: sessionRecord.AbsoluteExpiresAt,
	}

	var authCanRead bool
	if err := authPool.QueryRow(ctx, `SELECT has_table_privilege(current_user, 'platform.driver_enrollment_invitation', 'SELECT')`).Scan(&authCanRead); err != nil {
		t.Fatal(err)
	}
	if authCanRead {
		t.Fatal("tp_auth_runtime has direct invitation table read access")
	}
	candidates, err := service.Candidates(ctx, supervisorToken, supervisorSession, fixture.CompanyA, &fixture.OperationA)
	if err != nil || len(candidates) == 0 {
		t.Fatalf("authorized supervisor could not list driver candidates: candidates=%+v err=%v", candidates, err)
	}
	if candidates[0].PersonID != fixture.PersonADriver {
		t.Fatalf("unexpected driver candidate: %+v", candidates[0])
	}

	issued, err := service.Create(ctx, supervisorToken, supervisorSession, applicationauth.DriverEnrollmentInvitationInput{
		PersonID: fixture.PersonADriver, CompanyID: fixture.CompanyA, CompanyOperationID: &fixture.OperationA, ExpiresSeconds: 600,
	})
	if err != nil || len(issued.Token) != 32 || len(issued.Code) != 8 {
		t.Fatalf("valid invitation was not issued: invitation=%+v token_length=%d code_length=%d err=%v", issued.Invitation, len(issued.Token), len(issued.Code), err)
	}
	preview, err := service.Resolve(ctx, issued.Token)
	if err != nil || preview.PersonID != fixture.PersonADriver || preview.CompanyID != fixture.CompanyA || preview.CompanyOperationID == nil || *preview.CompanyOperationID != fixture.OperationA {
		t.Fatalf("invitation preview mismatch: preview=%+v err=%v", preview, err)
	}
	if _, err := service.Create(ctx, supervisorToken, supervisorSession, applicationauth.DriverEnrollmentInvitationInput{
		PersonID: fixture.PersonADriver, CompanyID: fixture.CompanyA, CompanyOperationID: &fixture.OperationA, ExpiresSeconds: 600,
	}); !errors.Is(err, applicationauth.ErrDriverInvitationConflict) {
		t.Fatalf("duplicate active invitation error=%v, want conflict", err)
	}

	installationKey := newInvitationInstallationKey(t)
	consumeInput := func(token []byte, code string, installationID uuid.UUID) applicationauth.DriverEnrollmentConsumeInput {
		return applicationauth.DriverEnrollmentConsumeInput{
			Token: token, ConfirmationCode: code, InstallationID: installationID,
			InstallationPublicKey: elliptic.Marshal(elliptic.P256(), installationKey.PublicKey.X, installationKey.PublicKey.Y),
			KeyAlgorithm:          "ECDSA_P256_SHA256", UserAgent: "invitation-test", RemoteAddr: "192.0.2.80:443",
		}
	}
	if _, err := service.Consume(ctx, consumeInput(issued.Token, "00000000", uuid.New())); !errors.Is(err, applicationauth.ErrDriverInvitationInvalid) {
		t.Fatalf("wrong confirmation code error=%v, want generic rejection", err)
	}
	installationID := uuid.New()
	activation, err := service.Consume(ctx, consumeInput(issued.Token, issued.Code, installationID))
	if err != nil {
		t.Fatalf("valid invitation was not consumed: %v", err)
	}
	resolved, err := repository.ResolveHTTPSession(ctx, applicationauth.HashSessionToken(activation.Token))
	if err != nil || resolved.SessionPolicy != applicationauth.DriverActivationSession {
		t.Fatalf("invitation session was not limited to activation: session=%+v err=%v", resolved, err)
	}
	if activation.Request.Status != "PENDING" || activation.Request.InstallationID != installationID {
		t.Fatalf("unexpected activation request: %+v", activation.Request)
	}
	var invitationStatus string
	var linkedInvitation *uuid.UUID
	if err := adminPool.QueryRow(ctx, `SELECT status FROM platform.driver_enrollment_invitation WHERE id = $1`, issued.Invitation.ID).Scan(&invitationStatus); err != nil {
		t.Fatal(err)
	}
	if invitationStatus != "CONSUMED" {
		t.Fatalf("invitation status=%s, want CONSUMED", invitationStatus)
	}
	if err := adminPool.QueryRow(ctx, `SELECT invitation_id FROM platform.device_enrollment_request WHERE id = $1`, activation.Request.ID).Scan(&linkedInvitation); err != nil {
		t.Fatal(err)
	}
	if linkedInvitation == nil || *linkedInvitation != issued.Invitation.ID {
		t.Fatalf("request was not linked to invitation: %v", linkedInvitation)
	}
	if _, err := service.Consume(ctx, consumeInput(issued.Token, issued.Code, uuid.New())); !errors.Is(err, applicationauth.ErrDriverInvitationInvalid) {
		t.Fatalf("replayed invitation error=%v, want generic rejection", err)
	}

	wrongTenant, err := service.Create(ctx, supervisorToken, supervisorSession, applicationauth.DriverEnrollmentInvitationInput{
		PersonID: fixture.PersonADriver, CompanyID: fixture.CompanyB, CompanyOperationID: &fixture.OperationB, ExpiresSeconds: 600,
	})
	if err == nil || wrongTenant.Invitation.ID != uuid.Nil {
		t.Fatalf("cross-tenant invitation was accepted: invitation=%+v err=%v", wrongTenant.Invitation, err)
	}

	expired, err := service.Create(ctx, supervisorToken, supervisorSession, applicationauth.DriverEnrollmentInvitationInput{
		PersonID: fixture.PersonADriver, CompanyID: fixture.CompanyA, CompanyOperationID: &fixture.OperationA, ExpiresSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `UPDATE platform.driver_enrollment_invitation SET expires_at = clock_timestamp() + interval '1 millisecond' WHERE id = $1`, expired.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `SELECT pg_sleep(0.01)`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Consume(ctx, consumeInput(expired.Token, expired.Code, uuid.New())); !errors.Is(err, applicationauth.ErrDriverInvitationInvalid) {
		t.Fatalf("expired invitation error=%v, want generic rejection", err)
	}

	revoked, err := service.Create(ctx, supervisorToken, supervisorSession, applicationauth.DriverEnrollmentInvitationInput{
		PersonID: fixture.PersonADriver, CompanyID: fixture.CompanyA, CompanyOperationID: &fixture.OperationA, ExpiresSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Revoke(ctx, supervisorToken, supervisorSession, revoked.Invitation.ID, "manual test"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Consume(ctx, consumeInput(revoked.Token, revoked.Code, uuid.New())); !errors.Is(err, applicationauth.ErrDriverInvitationInvalid) {
		t.Fatalf("revoked invitation error=%v, want generic rejection", err)
	}

	concurrent, err := service.Create(ctx, supervisorToken, supervisorSession, applicationauth.DriverEnrollmentInvitationInput{
		PersonID: fixture.PersonADriver, CompanyID: fixture.CompanyA, CompanyOperationID: &fixture.OperationA, ExpiresSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, consumeErr := service.Consume(ctx, consumeInput(concurrent.Token, concurrent.Code, uuid.New()))
			results <- consumeErr
		}()
	}
	group.Wait()
	close(results)
	successes := 0
	for consumeErr := range results {
		if consumeErr == nil {
			successes++
		} else if !errors.Is(consumeErr, applicationauth.ErrDriverInvitationInvalid) {
			t.Fatalf("concurrent consume error=%v", consumeErr)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent invitation consumption successes=%d, want 1", successes)
	}

	// Keep the temporary activation session from affecting later fixture checks.
	if err := repository.RevokeSession(ctx, activation.Session.ID, applicationauth.HashSessionToken(activation.Token), "invitation test cleanup"); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `DELETE FROM platform.driver_profile WHERE person_id = $1`, fixture.PersonADriver); err != nil {
		t.Fatal(err)
	}
}

func newInvitationInstallationKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
