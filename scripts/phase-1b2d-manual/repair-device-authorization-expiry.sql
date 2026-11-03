-- Manual-only recovery for the single historical authorization whose expiry
-- incorrectly inherited device_enrollment_request.expires_at.
--
-- This is not a Goose migration and must not be copied into production. It
-- expects transportation_platform_phase_1b2d and a tp_migrator session that
-- may SET ROLE tp_owner for this transaction only. It never changes the
-- authorization id, passkeys, enrollment rows, memberships, or sessions.

\set ON_ERROR_STOP on

BEGIN;
SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;
SET ROLE tp_owner;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

CREATE TEMP TABLE pg_temp.device_authorization_expiry_repair_result (
    success boolean NOT NULL,
    authorization_id uuid,
    previous_expires_at timestamptz,
    new_expires_at timestamptz,
    audit_id uuid,
    error_code text
) ON COMMIT PRESERVE ROWS;

-- Fail closed before entering the row-locking block. The exact installation,
-- account, company and operation are intentionally fixed for this manual DB.
-- The company and operation are checked by their exact authorization columns;
-- their existence is additionally guaranteed by the composite FK. This avoids
-- inventing a broad owner read policy for company_operation under FORCE RLS.
SELECT current_database() = 'transportation_platform_phase_1b2d'
   AND session_user IN ('tp_migrator', 'tp_owner')
   AND current_user = 'tp_owner'
   AND (SELECT count(*)
          FROM platform.device_authorization da
         WHERE da.installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337') = 1
   AND (SELECT count(*)
          FROM platform.device_authorization da
         WHERE da.installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337'
           AND da.user_account_id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003'
           AND da.company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
           AND da.company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001'
           AND da.status = 'ACTIVE'
           AND da.revoked_by_user_account_id IS NULL
           AND da.reason IS NULL
           AND da.expires_at <= clock_timestamp()) = 1
   AND (SELECT count(*)
          FROM platform.user_account ua
          JOIN platform.person p ON p.id = ua.person_id
         WHERE ua.id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003'
           AND ua.status = 'ACTIVE'
           AND p.status = 'ACTIVE') = 1
   AND (SELECT count(*)
          FROM platform.device_enrollment_request d
         WHERE d.installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337'
           AND d.user_account_id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003'
           AND d.company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
           AND d.company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001'
           AND d.status = 'CONSUMED'
           AND d.expires_at <= clock_timestamp()) = 1
   AND (SELECT count(*)
          FROM platform.authenticator_credential ac
         WHERE ac.user_account_id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003'
           AND ac.status = 'ACTIVE') >= 1 AS preconditions_ok
\gset device_expiry_repair_

\if :device_expiry_repair_preconditions_ok
\else
ROLLBACK;
\echo 'Device authorization expiry preconditions failed; transaction rolled back.'
DO $$
BEGIN
    RAISE EXCEPTION 'device authorization expiry preconditions failed'
        USING ERRCODE = 'P0001';
END;
$$;
\endif

DO $$
DECLARE
    v_authorization platform.device_authorization%ROWTYPE;
    v_active_ttl interval;
    v_now timestamptz := clock_timestamp();
    v_new_expires_at timestamptz;
    v_audit_id uuid := gen_random_uuid();
    v_passkeys integer;
    v_requests integer;
BEGIN
    SELECT da.* INTO v_authorization
      FROM platform.device_authorization da
     WHERE da.installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337'::uuid
       AND da.user_account_id = 'aaaaaaaa-aaaa-aaaa-aaaa-200000000003'::uuid
       AND da.company_id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'::uuid
       AND da.company_operation_id = 'aaaaaaaa-aaaa-aaaa-aaaa-000000000001'::uuid
       AND da.status = 'ACTIVE'
       AND da.revoked_by_user_account_id IS NULL
       AND da.reason IS NULL
       AND da.expires_at <= v_now
     FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'expired device authorization was not found'
            USING ERRCODE = 'P0001';
    END IF;

    IF (SELECT count(*)
          FROM platform.device_authorization da
         WHERE da.installation_id = v_authorization.installation_id) <> 1 THEN
        RAISE EXCEPTION 'multiple device authorizations match the manual installation'
            USING ERRCODE = 'P0001';
    END IF;

    SELECT count(*) INTO v_passkeys
      FROM platform.authenticator_credential ac
     WHERE ac.user_account_id = v_authorization.user_account_id
       AND ac.status = 'ACTIVE';
    IF v_passkeys < 1 THEN
        RAISE EXCEPTION 'no active WebAuthn credential exists for the driver'
            USING ERRCODE = 'P0001';
    END IF;

    SELECT count(*) INTO v_requests
      FROM platform.device_enrollment_request d
     WHERE d.installation_id = v_authorization.installation_id
       AND d.user_account_id = v_authorization.user_account_id
       AND d.company_id = v_authorization.company_id
       AND d.company_operation_id = v_authorization.company_operation_id
       AND d.status = 'CONSUMED'
       AND d.expires_at <= v_now;
    IF v_requests <> 1 THEN
        RAISE EXCEPTION 'the consumed enrollment request is ambiguous or missing'
            USING ERRCODE = 'P0001';
    END IF;

    SELECT p.active_ttl INTO v_active_ttl
      FROM platform.device_authorization_policy p
     WHERE p.policy_key = 'default'
     FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'device authorization policy is not configured'
            USING ERRCODE = 'P0001';
    END IF;

    v_new_expires_at := v_now + v_active_ttl;

    UPDATE platform.device_authorization
       SET expires_at = v_new_expires_at,
           last_validated_at = v_now
     WHERE id = v_authorization.id
       AND status = 'ACTIVE'
       AND expires_at <= v_now
       AND revoked_by_user_account_id IS NULL
       AND reason IS NULL;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'device authorization changed during recovery'
            USING ERRCODE = 'P0001';
    END IF;

    INSERT INTO platform.security_audit_event(
        id, event_type, outcome, subject_user_account_id, company_id,
        company_operation_id, reason_code, details)
    VALUES (
        v_audit_id, 'DEVICE_AUTHORIZATION_EXPIRY_RECOVERED', 'SUCCESS',
        v_authorization.user_account_id, v_authorization.company_id,
        v_authorization.company_operation_id,
        'manual_expiry_recovery',
        jsonb_build_object(
            'authorization_id', v_authorization.id,
            'installation_id', v_authorization.installation_id,
            'previous_expires_at', v_authorization.expires_at,
            'new_expires_at', v_new_expires_at,
            'policy_key', 'default'));

    INSERT INTO pg_temp.device_authorization_expiry_repair_result(
        success, authorization_id, previous_expires_at, new_expires_at,
        audit_id)
    VALUES (true, v_authorization.id, v_authorization.expires_at,
            v_new_expires_at, v_audit_id);
EXCEPTION WHEN OTHERS THEN
    INSERT INTO pg_temp.device_authorization_expiry_repair_result(
        success, error_code)
    VALUES (false, SQLSTATE);
END;
$$;

SELECT success AS repair_success
  FROM pg_temp.device_authorization_expiry_repair_result
 ORDER BY success DESC
 LIMIT 1
\gset device_expiry_repair_

\if :device_expiry_repair_repair_success
SELECT authorization_id, previous_expires_at, new_expires_at, audit_id
  FROM pg_temp.device_authorization_expiry_repair_result
 WHERE success;
COMMIT;
RESET ROLE;
\else
SELECT error_code AS repair_error_code
  FROM pg_temp.device_authorization_expiry_repair_result
 WHERE NOT success;
ROLLBACK;
RESET ROLE;
\echo 'Device authorization expiry repair failed; transaction rolled back.'
DO $$
BEGIN
    RAISE EXCEPTION 'device authorization expiry repair failed'
        USING ERRCODE = 'P0001';
END;
$$;
\endif
