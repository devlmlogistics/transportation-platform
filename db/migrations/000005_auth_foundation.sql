-- +goose Up

ALTER TABLE platform.user_account
    ADD COLUMN session_epoch bigint NOT NULL DEFAULT 0;

ALTER TABLE platform.user_account
    ADD CONSTRAINT user_account_session_epoch_ck CHECK (session_epoch >= 0);

CREATE TABLE platform.password_credential (
    id                  uuid PRIMARY KEY,
    user_account_id     uuid NOT NULL REFERENCES platform.user_account(id),
    purpose             text NOT NULL CHECK (purpose IN ('BOOTSTRAP', 'RECOVERY')),
    password_hash       text NOT NULL,
    kdf_name            text NOT NULL,
    kdf_parameters      jsonb NOT NULL DEFAULT '{}'::jsonb
                        CHECK (jsonb_typeof(kdf_parameters) = 'object'),
    status              text NOT NULL DEFAULT 'ACTIVE'
                        CHECK (status IN ('ACTIVE', 'REVOKED', 'EXPIRED', 'COMPROMISED')),
    created_at          timestamptz NOT NULL DEFAULT now(),
    last_used_at        timestamptz,
    expires_at          timestamptz,
    revoked_at          timestamptz,
    revoked_reason      text,
    CONSTRAINT password_credential_dates_ck
        CHECK (expires_at IS NULL OR expires_at > created_at),
    CONSTRAINT password_credential_status_ck CHECK (
        (status = 'ACTIVE' AND revoked_at IS NULL)
        OR (status <> 'ACTIVE' AND revoked_at IS NOT NULL)
    )
);

CREATE INDEX password_credential_user_status_idx
    ON platform.password_credential (user_account_id, status, purpose);

CREATE TABLE platform.authenticator_credential (
    id                  uuid PRIMARY KEY,
    user_account_id     uuid NOT NULL REFERENCES platform.user_account(id),
    credential_id       bytea NOT NULL UNIQUE
                        CHECK (octet_length(credential_id) BETWEEN 16 AND 1024),
    public_key          bytea NOT NULL CHECK (octet_length(public_key) > 0),
    sign_count          bigint NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    transports          text[] NOT NULL DEFAULT '{}',
    backup_eligible     boolean NOT NULL DEFAULT false,
    backup_state        text,
    user_verified_last  boolean NOT NULL DEFAULT false,
    display_name        text,
    status              text NOT NULL DEFAULT 'ACTIVE'
                        CHECK (status IN ('ACTIVE', 'REVOKED', 'COMPROMISED')),
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb
                        CHECK (jsonb_typeof(metadata) = 'object'),
    created_at          timestamptz NOT NULL DEFAULT now(),
    last_used_at        timestamptz,
    revoked_at          timestamptz,
    revoked_reason      text,
    CONSTRAINT authenticator_credential_status_ck CHECK (
        (status = 'ACTIVE' AND revoked_at IS NULL)
        OR (status <> 'ACTIVE' AND revoked_at IS NOT NULL)
    )
);

CREATE INDEX authenticator_credential_user_status_idx
    ON platform.authenticator_credential (user_account_id, status);

CREATE TABLE platform.device_authorization (
    id                         uuid PRIMARY KEY,
    installation_id            uuid NOT NULL,
    user_account_id            uuid NOT NULL REFERENCES platform.user_account(id),
    company_id                 uuid NOT NULL REFERENCES platform.company(id),
    company_operation_id       uuid,
    installation_public_key    bytea NOT NULL CHECK (octet_length(installation_public_key) > 0),
    key_algorithm              text NOT NULL,
    status                     text NOT NULL DEFAULT 'PENDING'
                               CHECK (status IN ('PENDING', 'ACTIVE', 'REVOKED', 'LOST', 'REPLACED', 'EXPIRED')),
    requested_at               timestamptz NOT NULL DEFAULT now(),
    approved_at                timestamptz,
    expires_at                 timestamptz,
    last_validated_at          timestamptz,
    approved_by_user_account_id uuid REFERENCES platform.user_account(id),
    revoked_by_user_account_id  uuid REFERENCES platform.user_account(id),
    reason                     text,
    device_name                text,
    user_agent_hash            bytea,
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT device_authorization_dates_ck
        CHECK (expires_at IS NULL OR expires_at > requested_at),
    CONSTRAINT device_authorization_scope_ck
        CHECK (company_operation_id IS NULL OR company_id IS NOT NULL)
);

CREATE UNIQUE INDEX device_authorization_installation_active_uidx
    ON platform.device_authorization (installation_id, user_account_id, company_id, company_operation_id)
    NULLS NOT DISTINCT
    WHERE status IN ('PENDING', 'ACTIVE');

CREATE INDEX device_authorization_company_status_idx
    ON platform.device_authorization (company_id, status, requested_at DESC);

CREATE TABLE platform.device_enrollment_request (
    id                         uuid PRIMARY KEY,
    correlation_code           text NOT NULL UNIQUE,
    user_account_id            uuid REFERENCES platform.user_account(id),
    company_id                 uuid NOT NULL REFERENCES platform.company(id),
    company_operation_id       uuid,
    installation_id            uuid NOT NULL,
    binding_hash               bytea NOT NULL CHECK (octet_length(binding_hash) = 32),
    status                     text NOT NULL DEFAULT 'PENDING'
                               CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED', 'CONSUMED')),
    requested_at               timestamptz NOT NULL DEFAULT now(),
    expires_at                 timestamptz NOT NULL,
    approved_at                timestamptz,
    consumed_at                timestamptz,
    approved_by_user_account_id uuid REFERENCES platform.user_account(id),
    rejected_by_user_account_id uuid REFERENCES platform.user_account(id),
    reason                     text,
    evidence_reference         text,
    manual_fallback_allowed    boolean NOT NULL DEFAULT false,
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT device_enrollment_request_dates_ck
        CHECK (expires_at > requested_at),
    CONSTRAINT device_enrollment_request_state_ck CHECK (
        (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED') AND consumed_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL)
    )
);

CREATE INDEX device_enrollment_request_scope_status_idx
    ON platform.device_enrollment_request (company_id, company_operation_id, status, expires_at);

CREATE TABLE platform.auth_challenge (
    id                         uuid PRIMARY KEY,
    purpose                    text NOT NULL
                               CHECK (purpose IN ('LOGIN', 'REGISTRATION', 'STEP_UP', 'RECOVERY', 'DEVICE_ENROLLMENT')),
    user_account_id            uuid REFERENCES platform.user_account(id),
    device_enrollment_request_id uuid REFERENCES platform.device_enrollment_request(id),
    challenge_hash             bytea NOT NULL CHECK (octet_length(challenge_hash) = 32),
    session_data               bytea NOT NULL CHECK (octet_length(session_data) BETWEEN 1 AND 65536),
    session_data_format        text NOT NULL CHECK (length(trim(session_data_format)) BETWEEN 1 AND 32),
    binding_hash               bytea NOT NULL CHECK (octet_length(binding_hash) = 32),
    status                     text NOT NULL DEFAULT 'PENDING'
                               CHECK (status IN ('PENDING', 'CONSUMED', 'EXPIRED', 'REJECTED')),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    expires_at                 timestamptz NOT NULL,
    consumed_at                timestamptz,
    attempt_count              integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_attempt_at            timestamptz,
    request_fingerprint        bytea,
    CONSTRAINT auth_challenge_dates_ck CHECK (expires_at > created_at),
    CONSTRAINT auth_challenge_state_ck CHECK (
        (status = 'PENDING' AND consumed_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL)
        OR (status IN ('EXPIRED', 'REJECTED') AND consumed_at IS NULL)
    )
);

CREATE INDEX auth_challenge_pending_idx
    ON platform.auth_challenge (id, purpose)
    WHERE status = 'PENDING' AND consumed_at IS NULL;

CREATE INDEX auth_challenge_expiry_idx
    ON platform.auth_challenge (expires_at)
    WHERE status = 'PENDING' AND consumed_at IS NULL;

CREATE TABLE platform.auth_session (
    id                         uuid PRIMARY KEY,
    user_account_id            uuid NOT NULL REFERENCES platform.user_account(id),
    token_hash                 bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    csrf_secret_hash           bytea CHECK (csrf_secret_hash IS NULL OR octet_length(csrf_secret_hash) = 32),
    status                     text NOT NULL DEFAULT 'ACTIVE'
                               CHECK (status IN ('ACTIVE', 'REVOKED', 'EXPIRED', 'COMPROMISED')),
    created_at                 timestamptz NOT NULL DEFAULT now(),
    last_seen_at               timestamptz NOT NULL DEFAULT now(),
    idle_expires_at            timestamptz NOT NULL,
    absolute_expires_at        timestamptz NOT NULL,
    revoked_at                 timestamptz,
    expired_at                 timestamptz,
    compromised_at             timestamptz,
    terminal_reason            text,
    session_epoch              bigint NOT NULL CHECK (session_epoch >= 0),
    context_version            bigint NOT NULL DEFAULT 0 CHECK (context_version >= 0),
    active_company_id          uuid REFERENCES platform.company(id),
    active_operation_id        uuid,
    authenticator_credential_id uuid REFERENCES platform.authenticator_credential(id),
    device_authorization_id   uuid REFERENCES platform.device_authorization(id),
    user_agent_hash            bytea,
    ip_prefix                  text,
    FOREIGN KEY (active_company_id, active_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT auth_session_dates_ck CHECK (
        idle_expires_at > created_at
        AND absolute_expires_at > created_at
        AND absolute_expires_at >= idle_expires_at
    ),
    CONSTRAINT auth_session_state_ck CHECK (
        (status = 'ACTIVE'
            AND revoked_at IS NULL AND expired_at IS NULL AND compromised_at IS NULL)
        OR (status = 'REVOKED'
            AND revoked_at IS NOT NULL AND expired_at IS NULL AND compromised_at IS NULL)
        OR (status = 'EXPIRED'
            AND revoked_at IS NULL AND expired_at IS NOT NULL AND compromised_at IS NULL)
        OR (status = 'COMPROMISED'
            AND revoked_at IS NULL AND expired_at IS NULL AND compromised_at IS NOT NULL)
    )
);

CREATE INDEX auth_session_user_status_idx
    ON platform.auth_session (user_account_id, status);

CREATE INDEX auth_session_expiry_idx
    ON platform.auth_session (idle_expires_at, absolute_expires_at)
    WHERE status = 'ACTIVE';

CREATE TABLE platform.recovery_request (
    id                         uuid PRIMARY KEY,
    user_account_id            uuid NOT NULL REFERENCES platform.user_account(id),
    kind                       text NOT NULL CHECK (kind IN ('LOST_DEVICE', 'COMPROMISED_CREDENTIAL', 'ACCOUNT_RECOVERY')),
    token_hash                 bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    status                     text NOT NULL DEFAULT 'PENDING'
                               CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED', 'CONSUMED')),
    channel_reference          text,
    evidence_reference         text,
    requested_at               timestamptz NOT NULL DEFAULT now(),
    expires_at                 timestamptz NOT NULL,
    consumed_at                timestamptz,
    approved_by_user_account_id uuid REFERENCES platform.user_account(id),
    reason                     text,
    CONSTRAINT recovery_request_dates_ck CHECK (expires_at > requested_at),
    CONSTRAINT recovery_request_state_ck CHECK (
        (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED') AND consumed_at IS NULL)
        OR (status = 'CONSUMED' AND consumed_at IS NOT NULL)
    )
);

CREATE INDEX recovery_request_user_status_idx
    ON platform.recovery_request (user_account_id, status, expires_at);

CREATE TABLE platform.security_audit_event (
    id                    uuid PRIMARY KEY,
    occurred_at           timestamptz NOT NULL DEFAULT now(),
    event_type            text NOT NULL,
    outcome               text NOT NULL CHECK (outcome IN ('SUCCESS', 'FAILURE', 'DENIED')),
    actor_user_account_id uuid REFERENCES platform.user_account(id),
    subject_user_account_id uuid REFERENCES platform.user_account(id),
    company_id            uuid REFERENCES platform.company(id),
    company_operation_id  uuid,
    reason_code            text,
    correlation_id         text,
    details                jsonb NOT NULL DEFAULT '{}'::jsonb
                          CHECK (jsonb_typeof(details) = 'object'),
    FOREIGN KEY (company_id, company_operation_id)
        REFERENCES platform.company_operation(company_id, id),
    CONSTRAINT security_audit_scope_ck
        CHECK (company_operation_id IS NULL OR company_id IS NOT NULL),
    CONSTRAINT security_audit_details_size_ck
        CHECK (octet_length(details::text) <= 8192)
);

CREATE INDEX security_audit_event_time_idx
    ON platform.security_audit_event (occurred_at DESC);

CREATE INDEX security_audit_event_subject_time_idx
    ON platform.security_audit_event (subject_user_account_id, occurred_at DESC);

CREATE INDEX security_audit_event_company_time_idx
    ON platform.security_audit_event (company_id, occurred_at DESC);

ALTER TABLE platform.password_credential OWNER TO tp_owner;
ALTER TABLE platform.authenticator_credential OWNER TO tp_owner;
ALTER TABLE platform.device_authorization OWNER TO tp_owner;
ALTER TABLE platform.device_enrollment_request OWNER TO tp_owner;
ALTER TABLE platform.auth_challenge OWNER TO tp_owner;
ALTER TABLE platform.auth_session OWNER TO tp_owner;
ALTER TABLE platform.recovery_request OWNER TO tp_owner;
ALTER TABLE platform.security_audit_event OWNER TO tp_owner;

ALTER TABLE platform.password_credential ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.password_credential FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.authenticator_credential ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.authenticator_credential FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.device_authorization ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.device_authorization FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.device_enrollment_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.device_enrollment_request FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_challenge ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_challenge FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_session ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_session FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.recovery_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.recovery_request FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.security_audit_event ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.security_audit_event FORCE ROW LEVEL SECURITY;

-- The owner is a NOLOGIN DDL identity. Explicit owner policies make the
-- SECURITY DEFINER functions work even with FORCE ROW LEVEL SECURITY while
-- keeping the global rows inaccessible to tp_runtime SELECT queries.
CREATE POLICY password_credential_owner_all
    ON platform.password_credential FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY authenticator_credential_owner_all
    ON platform.authenticator_credential FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY auth_challenge_owner_all
    ON platform.auth_challenge FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY auth_session_owner_all
    ON platform.auth_session FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY recovery_request_owner_all
    ON platform.recovery_request FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY security_audit_owner_all
    ON platform.security_audit_event FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);

-- The resolver function joins these global identity rows under tp_owner. The
-- policy is not granted to tp_runtime and the owner role has NOLOGIN.
CREATE POLICY person_auth_resolver_owner
    ON platform.person FOR SELECT TO tp_owner
    USING (true);
CREATE POLICY user_account_auth_resolver_owner
    ON platform.user_account FOR SELECT TO tp_owner
    USING (true);
CREATE POLICY user_account_auth_resolver_owner_update
    ON platform.user_account FOR UPDATE TO tp_owner
    USING (true) WITH CHECK (true);

CREATE POLICY device_authorization_owner_all
    ON platform.device_authorization FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);
CREATE POLICY device_enrollment_owner_all
    ON platform.device_enrollment_request FOR ALL TO tp_owner
    USING (true) WITH CHECK (true);

-- Authentication and device tables have no direct runtime DML policies.
-- Only the narrow SECURITY DEFINER functions below are exposed to the
-- dedicated authentication role. Device administration remains blocked until
-- a separate authorized service contract is introduced.

GRANT SELECT ON platform.operation_type, platform.role, platform.role_scope,
    platform.permission, platform.permission_scope, platform.role_permission
TO tp_runtime;

REVOKE ALL ON platform.auth_session, platform.auth_challenge,
    platform.security_audit_event, platform.device_authorization,
    platform.device_enrollment_request
FROM tp_runtime;

-- A separate login role is the database boundary for the future online
-- authentication service. It receives function EXECUTE only; it does not
-- receive direct table access.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.resolve_auth_session(p_token_hash bytea)
RETURNS TABLE (
    session_id uuid,
    user_account_id uuid,
    person_id uuid,
    session_epoch bigint,
    context_version bigint,
    active_company_id uuid,
    active_operation_id uuid,
    authenticator_credential_id uuid,
    device_authorization_id uuid,
    status text,
    idle_expires_at timestamptz,
    absolute_expires_at timestamptz,
    revoked_at timestamptz,
    expired_at timestamptz,
    compromised_at timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
    SELECT
        s.id,
        s.user_account_id,
        ua.person_id,
        s.session_epoch,
        s.context_version,
        s.active_company_id,
        s.active_operation_id,
        s.authenticator_credential_id,
        s.device_authorization_id,
        s.status,
        s.idle_expires_at,
        s.absolute_expires_at,
        s.revoked_at,
        s.expired_at,
        s.compromised_at
    FROM platform.auth_session AS s
    JOIN platform.user_account AS ua ON ua.id = s.user_account_id
    JOIN platform.person AS p ON p.id = ua.person_id
    WHERE octet_length(p_token_hash) = 32
      AND s.token_hash = p_token_hash
      AND s.status = 'ACTIVE'
      AND ua.status = 'ACTIVE'
      AND p.status = 'ACTIVE'
      AND s.session_epoch = ua.session_epoch
      AND now() < s.idle_expires_at
      AND now() < s.absolute_expires_at
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.resolve_auth_session(bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.resolve_auth_session(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.resolve_auth_session(bytea) TO tp_auth_runtime;

-- A runtime connection must never be able to change the global revocation
-- epoch. The privileged session functions below run as tp_owner and are the
-- only implementation path that may increment it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.prevent_runtime_session_epoch_change()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
BEGIN
    IF session_user = 'tp_runtime'
       AND NEW.session_epoch IS DISTINCT FROM OLD.session_epoch
    THEN
        RAISE EXCEPTION 'runtime cannot change session_epoch'
            USING ERRCODE = '42501';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.prevent_runtime_session_epoch_change() OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.prevent_runtime_session_epoch_change() FROM PUBLIC;
CREATE TRIGGER user_account_session_epoch_guard
    BEFORE UPDATE OF session_epoch ON platform.user_account
    FOR EACH ROW EXECUTE FUNCTION platform.prevent_runtime_session_epoch_change();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.create_auth_session(
    p_session_id uuid,
    p_user_account_id uuid,
    p_token_hash bytea,
    p_csrf_secret_hash bytea,
    p_idle_seconds bigint,
    p_absolute_seconds bigint,
    p_active_company_id uuid,
    p_active_operation_id uuid,
    p_authenticator_credential_id uuid,
    p_device_authorization_id uuid,
    p_user_agent_hash bytea,
    p_ip_prefix text
)
RETURNS TABLE (
    session_id uuid,
    user_account_id uuid,
    session_epoch bigint,
    status text,
    created_at timestamptz,
    last_seen_at timestamptz,
    idle_expires_at timestamptz,
    absolute_expires_at timestamptz,
    active_company_id uuid,
    active_operation_id uuid,
    context_version bigint
)
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    account_epoch bigint;
    issued_at timestamptz;
BEGIN
    IF p_session_id IS NULL
       OR p_user_account_id IS NULL
       OR octet_length(p_token_hash) <> 32
       OR (p_csrf_secret_hash IS NOT NULL AND octet_length(p_csrf_secret_hash) <> 32)
       OR p_idle_seconds <= 0
       OR p_absolute_seconds < p_idle_seconds
    THEN
        RETURN;
    END IF;

    SELECT ua.session_epoch INTO account_epoch
    FROM platform.user_account AS ua
    JOIN platform.person AS p ON p.id = ua.person_id
    WHERE ua.id = p_user_account_id
      AND ua.status = 'ACTIVE'
      AND p.status = 'ACTIVE'
    FOR UPDATE OF ua;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    issued_at := clock_timestamp();

    INSERT INTO platform.auth_session (
        id, user_account_id, token_hash, csrf_secret_hash, status,
        created_at, last_seen_at, idle_expires_at, absolute_expires_at,
        session_epoch, context_version, active_company_id, active_operation_id,
        authenticator_credential_id, device_authorization_id, user_agent_hash,
        ip_prefix
    )
    VALUES (
        p_session_id, p_user_account_id, p_token_hash, p_csrf_secret_hash,
        'ACTIVE', issued_at, issued_at,
        issued_at + make_interval(secs => p_idle_seconds),
        issued_at + make_interval(secs => p_absolute_seconds),
        account_epoch, 0, p_active_company_id, p_active_operation_id,
        p_authenticator_credential_id, p_device_authorization_id,
        p_user_agent_hash, NULLIF(p_ip_prefix, '')
    );

    RETURN QUERY
    SELECT p_session_id, p_user_account_id, account_epoch, 'ACTIVE'::text,
           issued_at, issued_at,
           issued_at + make_interval(secs => p_idle_seconds),
           issued_at + make_interval(secs => p_absolute_seconds),
           p_active_company_id, p_active_operation_id, 0::bigint;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.create_auth_session(uuid, uuid, bytea, bytea, bigint, bigint, uuid, uuid, uuid, uuid, bytea, text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.create_auth_session(uuid, uuid, bytea, bytea, bigint, bigint, uuid, uuid, uuid, uuid, bytea, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.create_auth_session(uuid, uuid, bytea, bytea, bigint, bigint, uuid, uuid, uuid, uuid, bytea, text) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.create_auth_challenge(
    p_id uuid,
    p_purpose text,
    p_user_account_id uuid,
    p_device_enrollment_request_id uuid,
    p_challenge_hash bytea,
    p_session_data bytea,
    p_session_data_format text,
    p_binding_hash bytea,
    p_expires_at timestamptz,
    p_request_fingerprint bytea
)
RETURNS void
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
BEGIN
    IF p_id IS NULL
       OR p_purpose IS NULL
       OR octet_length(p_challenge_hash) <> 32
       OR octet_length(p_binding_hash) <> 32
       OR p_session_data IS NULL
       OR octet_length(p_session_data) = 0
       OR p_expires_at <= clock_timestamp()
    THEN
        RAISE EXCEPTION 'invalid authentication challenge'
            USING ERRCODE = '22023';
    END IF;

    INSERT INTO platform.auth_challenge (
        id, purpose, user_account_id, device_enrollment_request_id,
        challenge_hash, session_data, session_data_format, binding_hash,
        expires_at, request_fingerprint
    )
    VALUES (
        p_id, p_purpose, p_user_account_id, p_device_enrollment_request_id,
        p_challenge_hash, p_session_data, p_session_data_format,
        p_binding_hash, p_expires_at, p_request_fingerprint
    );

END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.create_auth_challenge(uuid, text, uuid, uuid, bytea, bytea, text, bytea, timestamptz, bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.create_auth_challenge(uuid, text, uuid, uuid, bytea, bytea, text, bytea, timestamptz, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.create_auth_challenge(uuid, text, uuid, uuid, bytea, bytea, text, bytea, timestamptz, bytea) TO tp_auth_runtime;

-- A bearer token hash is the only proof accepted by these self-revocation
-- functions. The user_account_id is derived from the matching session; it is
-- never accepted from the caller. Administrative revocation of another user
-- remains blocked until a separately authorized service contract exists.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.revoke_auth_session(
    p_session_id uuid,
    p_token_hash bytea,
    p_reason text
)
RETURNS boolean
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    subject_user_account_id uuid;
    v_revoked_at timestamptz;
BEGIN
    IF p_session_id IS NULL OR octet_length(p_token_hash) <> 32 THEN
        RETURN false;
    END IF;

    SELECT s.user_account_id INTO subject_user_account_id
    FROM platform.auth_session AS s
    WHERE s.id = p_session_id
      AND s.token_hash = p_token_hash
      AND s.status = 'ACTIVE'
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN false;
    END IF;

    v_revoked_at := clock_timestamp();
    UPDATE platform.auth_session AS s
    SET status = 'REVOKED', revoked_at = v_revoked_at, terminal_reason = p_reason
    WHERE id = p_session_id;

    RETURN true;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.revoke_auth_session(uuid, bytea, text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.revoke_auth_session(uuid, bytea, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.revoke_auth_session(uuid, bytea, text) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.revoke_all_auth_sessions(
    p_token_hash bytea,
    p_reason text
)
RETURNS integer
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    subject_user_account_id uuid;
    previous_epoch bigint;
    v_revoked_at timestamptz;
    revoked_count integer;
BEGIN
    IF octet_length(p_token_hash) <> 32 THEN
        RETURN 0;
    END IF;

    SELECT s.user_account_id INTO subject_user_account_id
    FROM platform.auth_session AS s
    WHERE s.token_hash = p_token_hash
      AND s.status = 'ACTIVE'
    LIMIT 1;

    IF NOT FOUND THEN
        RETURN 0;
    END IF;

    SELECT ua.session_epoch INTO previous_epoch
    FROM platform.user_account AS ua
    WHERE ua.id = subject_user_account_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN 0;
    END IF;

    v_revoked_at := clock_timestamp();
    UPDATE platform.user_account
    SET session_epoch = session_epoch + 1, updated_at = v_revoked_at
    WHERE id = subject_user_account_id;

    UPDATE platform.auth_session AS s
    SET status = 'REVOKED', revoked_at = v_revoked_at, terminal_reason = p_reason
    WHERE user_account_id = subject_user_account_id
      AND status = 'ACTIVE';
    GET DIAGNOSTICS revoked_count = ROW_COUNT;

    RETURN revoked_count;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.revoke_all_auth_sessions(bytea, text) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.revoke_all_auth_sessions(bytea, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.revoke_all_auth_sessions(bytea, text) TO tp_auth_runtime;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.consume_auth_challenge(
    p_challenge_id uuid,
    p_purpose text,
    p_challenge_hash bytea,
    p_binding_hash bytea
)
RETURNS TABLE (
    challenge_id uuid,
    user_account_id uuid,
    device_enrollment_request_id uuid,
    session_data bytea,
    session_data_format text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
DECLARE
    challenge_row platform.auth_challenge%ROWTYPE;
    v_consumed_at timestamptz;
BEGIN
    IF p_challenge_id IS NULL
       OR p_purpose IS NULL
       OR octet_length(p_challenge_hash) <> 32
       OR octet_length(p_binding_hash) <> 32
    THEN
        RETURN;
    END IF;

    SELECT * INTO challenge_row
    FROM platform.auth_challenge
    WHERE id = p_challenge_id
      AND purpose = p_purpose
      AND challenge_hash = p_challenge_hash
      AND binding_hash = p_binding_hash
      AND status = 'PENDING'
      AND consumed_at IS NULL
      AND expires_at > clock_timestamp()
    FOR UPDATE;

    IF NOT FOUND THEN
        RETURN;
    END IF;

    v_consumed_at := clock_timestamp();
    UPDATE platform.auth_challenge AS c
    SET status = 'CONSUMED', consumed_at = v_consumed_at, last_attempt_at = v_consumed_at
    WHERE id = challenge_row.id;

    RETURN QUERY
    SELECT challenge_row.id,
           challenge_row.user_account_id,
           challenge_row.device_enrollment_request_id,
           challenge_row.session_data,
           challenge_row.session_data_format;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.consume_auth_challenge(uuid, text, bytea, bytea) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.consume_auth_challenge(uuid, text, bytea, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.consume_auth_challenge(uuid, text, bytea, bytea) TO tp_auth_runtime;

-- This writer is still behind a dedicated auth-role function and does not
-- grant table INSERT to either runtime role.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.record_auth_security_audit(
    p_id uuid,
    p_event_type text,
    p_outcome text,
    p_actor_user_account_id uuid,
    p_subject_user_account_id uuid,
    p_company_id uuid,
    p_company_operation_id uuid,
    p_reason_code text,
    p_correlation_id text,
    p_details jsonb
)
RETURNS void
LANGUAGE plpgsql
VOLATILE
SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
BEGIN
    IF p_id IS NULL OR p_event_type IS NULL OR p_outcome NOT IN ('SUCCESS', 'FAILURE', 'DENIED')
       OR (p_company_operation_id IS NOT NULL AND p_company_id IS NULL)
       OR p_details IS NULL OR jsonb_typeof(p_details) <> 'object'
       OR octet_length(p_details::text) > 8192
    THEN
        RAISE EXCEPTION 'invalid authentication audit event'
            USING ERRCODE = '22023';
    END IF;

    INSERT INTO platform.security_audit_event (
        id, event_type, outcome, actor_user_account_id,
        subject_user_account_id, company_id, company_operation_id,
        reason_code, correlation_id, details
    )
    VALUES (
        p_id, p_event_type, p_outcome, p_actor_user_account_id,
        p_subject_user_account_id, p_company_id, p_company_operation_id,
        p_reason_code, p_correlation_id, p_details
    );
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.record_auth_security_audit(uuid, text, text, uuid, uuid, uuid, uuid, text, text, jsonb) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.record_auth_security_audit(uuid, text, text, uuid, uuid, uuid, uuid, text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.record_auth_security_audit(uuid, text, text, uuid, uuid, uuid, uuid, text, text, jsonb) TO tp_auth_runtime;

-- +goose Down

REVOKE EXECUTE ON FUNCTION platform.record_auth_security_audit(uuid, text, text, uuid, uuid, uuid, uuid, text, text, jsonb) FROM tp_auth_runtime;
REVOKE EXECUTE ON FUNCTION platform.consume_auth_challenge(uuid, text, bytea, bytea) FROM tp_auth_runtime;
REVOKE EXECUTE ON FUNCTION platform.revoke_all_auth_sessions(bytea, text) FROM tp_auth_runtime;
REVOKE EXECUTE ON FUNCTION platform.revoke_auth_session(uuid, bytea, text) FROM tp_auth_runtime;
REVOKE EXECUTE ON FUNCTION platform.create_auth_challenge(uuid, text, uuid, uuid, bytea, bytea, text, bytea, timestamptz, bytea) FROM tp_auth_runtime;
REVOKE EXECUTE ON FUNCTION platform.create_auth_session(uuid, uuid, bytea, bytea, bigint, bigint, uuid, uuid, uuid, uuid, bytea, text) FROM tp_auth_runtime;
REVOKE EXECUTE ON FUNCTION platform.resolve_auth_session(bytea) FROM tp_auth_runtime;
DROP FUNCTION IF EXISTS platform.record_auth_security_audit(uuid, text, text, uuid, uuid, uuid, uuid, text, text, jsonb);
DROP FUNCTION IF EXISTS platform.consume_auth_challenge(uuid, text, bytea, bytea);
DROP FUNCTION IF EXISTS platform.revoke_all_auth_sessions(bytea, text);
DROP FUNCTION IF EXISTS platform.revoke_auth_session(uuid, bytea, text);
DROP FUNCTION IF EXISTS platform.create_auth_challenge(uuid, text, uuid, uuid, bytea, bytea, text, bytea, timestamptz, bytea);
DROP FUNCTION IF EXISTS platform.create_auth_session(uuid, uuid, bytea, bytea, bigint, bigint, uuid, uuid, uuid, uuid, bytea, text);
DROP FUNCTION IF EXISTS platform.resolve_auth_session(bytea);

DROP TRIGGER IF EXISTS user_account_session_epoch_guard ON platform.user_account;
DROP FUNCTION IF EXISTS platform.prevent_runtime_session_epoch_change();

DROP POLICY IF EXISTS device_enrollment_owner_all ON platform.device_enrollment_request;
DROP POLICY IF EXISTS device_authorization_owner_all ON platform.device_authorization;
DROP POLICY IF EXISTS user_account_auth_resolver_owner ON platform.user_account;
DROP POLICY IF EXISTS user_account_auth_resolver_owner_update ON platform.user_account;
DROP POLICY IF EXISTS person_auth_resolver_owner ON platform.person;
DROP POLICY IF EXISTS security_audit_owner_all ON platform.security_audit_event;
DROP POLICY IF EXISTS recovery_request_owner_all ON platform.recovery_request;
DROP POLICY IF EXISTS auth_session_owner_all ON platform.auth_session;
DROP POLICY IF EXISTS auth_challenge_owner_all ON platform.auth_challenge;
DROP POLICY IF EXISTS authenticator_credential_owner_all ON platform.authenticator_credential;
DROP POLICY IF EXISTS password_credential_owner_all ON platform.password_credential;

REVOKE ALL ON platform.auth_session, platform.auth_challenge,
    platform.security_audit_event, platform.device_authorization,
    platform.device_enrollment_request FROM tp_runtime;

ALTER TABLE platform.security_audit_event NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.security_audit_event DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.recovery_request NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.recovery_request DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_session NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_session DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_challenge NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.auth_challenge DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.device_enrollment_request NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.device_enrollment_request DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.device_authorization NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.device_authorization DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.authenticator_credential NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.authenticator_credential DISABLE ROW LEVEL SECURITY;
ALTER TABLE platform.password_credential NO FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.password_credential DISABLE ROW LEVEL SECURITY;

DROP TABLE IF EXISTS platform.security_audit_event;
DROP TABLE IF EXISTS platform.recovery_request;
DROP TABLE IF EXISTS platform.auth_session;
DROP TABLE IF EXISTS platform.auth_challenge;
DROP TABLE IF EXISTS platform.device_enrollment_request;
DROP TABLE IF EXISTS platform.device_authorization;
DROP TABLE IF EXISTS platform.authenticator_credential;
DROP TABLE IF EXISTS platform.password_credential;

ALTER TABLE platform.user_account
    DROP CONSTRAINT IF EXISTS user_account_session_epoch_ck;
ALTER TABLE platform.user_account
    DROP COLUMN IF EXISTS session_epoch;
