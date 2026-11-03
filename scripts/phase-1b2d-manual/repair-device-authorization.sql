-- Manual-only recovery for the historical zero-UUID authorization.
--
-- This script is intentionally not a Goose migration and must not be copied
-- into a production deployment. It expects one psql session connected to
-- transportation_platform_phase_1b2d as tp_migrator. It switches only for
-- this transaction to the existing tp_owner role; it creates no grants.
--
-- Run with psql's ON_ERROR_STOP. The script explicitly ROLLBACKs failed
-- preconditions and failed repair attempts. It refuses dependent rows because
-- the current foreign keys are immediate and non-deferrable. No credential or
-- public-key column is changed.

\set ON_ERROR_STOP on

BEGIN;
SET ROLE tp_owner;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

CREATE TEMP TABLE pg_temp.device_authorization_repair_result (
    success boolean NOT NULL,
    new_id uuid,
    error_code text,
    session_references integer NOT NULL DEFAULT 0,
    challenge_references integer NOT NULL DEFAULT 0
) ON COMMIT PRESERVE ROWS;

-- Fail closed before locking or changing anything. The row must be the only
-- authorization for the expected installation and the only zero-UUID row.
SELECT current_database() = 'transportation_platform_phase_1b2d'
   AND session_user IN ('tp_migrator', 'tp_owner')
   AND current_user = 'tp_owner'
   AND (SELECT count(*) FROM platform.device_authorization
        WHERE installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337') = 1
   AND (SELECT count(*) FROM platform.device_authorization
        WHERE id = '00000000-0000-0000-0000-000000000000') = 1
   AND (SELECT count(*) FROM platform.device_authorization
        WHERE id = '00000000-0000-0000-0000-000000000000'
          AND installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337'
          AND status = 'ACTIVE') = 1 AS preconditions_ok
\gset device_repair_

\if :device_repair_preconditions_ok
\else
ROLLBACK;
\echo 'Device authorization repair preconditions failed; transaction rolled back.'
DO $$
BEGIN
    RAISE EXCEPTION 'device authorization repair preconditions failed'
        USING ERRCODE = 'P0001';
END;
$$;
\endif

DO $$
DECLARE
    v_authorization platform.device_authorization%ROWTYPE;
    v_new_id uuid;
    v_public_key bytea;
    v_session_references integer;
    v_challenge_references integer;
BEGIN
    SELECT da.* INTO v_authorization
      FROM platform.device_authorization AS da
     WHERE da.id = '00000000-0000-0000-0000-000000000000'::uuid
       AND da.installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337'::uuid
       AND da.status = 'ACTIVE'
     FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001';
    END IF;

    v_public_key := v_authorization.installation_public_key;

    SELECT count(*) INTO v_session_references
      FROM platform.auth_session
     WHERE device_authorization_id = v_authorization.id;
    SELECT count(*) INTO v_challenge_references
      FROM platform.auth_challenge
     WHERE device_authorization_id = v_authorization.id;

    -- The current foreign keys are immediate and use ON UPDATE NO ACTION.
    -- This manual recovery is intentionally valid only when no dependent row
    -- exists. Do not attempt to rewrite child rows or temporarily defer the
    -- constraints.
    IF v_session_references <> 0
       OR v_challenge_references <> 0 THEN
        RAISE EXCEPTION 'Authorization has dependent references; manual review required'
            USING ERRCODE = 'P0001';
    END IF;

    LOOP
        v_new_id := gen_random_uuid();
        EXIT WHEN v_new_id <> '00000000-0000-0000-0000-000000000000'::uuid
              AND NOT EXISTS (
                  SELECT 1 FROM platform.device_authorization
                   WHERE id = v_new_id
              );
    END LOOP;

    UPDATE platform.device_authorization
       SET id = v_new_id
     WHERE id = v_authorization.id
       AND installation_id = '78f5e1f4-4f86-4e5f-8adf-955cb7d23337'::uuid
       AND status = 'ACTIVE';
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001';
    END IF;

    IF (SELECT installation_public_key
          FROM platform.device_authorization
         WHERE id = v_new_id) IS DISTINCT FROM v_public_key THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001';
    END IF;

    INSERT INTO pg_temp.device_authorization_repair_result(
        success, new_id, session_references, challenge_references)
    VALUES (true, v_new_id, v_session_references, v_challenge_references);
EXCEPTION WHEN OTHERS THEN
    -- The exception block rolls back the changes made inside this DO block;
    -- the outer transaction remains available for the explicit ROLLBACK below.
    INSERT INTO pg_temp.device_authorization_repair_result(success, error_code)
    VALUES (false, SQLSTATE);
END;
$$;

SELECT success AS repair_success
  FROM pg_temp.device_authorization_repair_result
 ORDER BY success DESC
 LIMIT 1
\gset device_repair_

\if :device_repair_repair_success
SELECT new_id AS repaired_device_authorization_id,
       session_references,
       challenge_references
  FROM pg_temp.device_authorization_repair_result
 WHERE success;
COMMIT;
RESET ROLE;
\else
SELECT error_code AS repair_error_code
  FROM pg_temp.device_authorization_repair_result
 WHERE NOT success;
ROLLBACK;
RESET ROLE;
\echo 'Device authorization repair failed; transaction rolled back.'
DO $$
BEGIN
    RAISE EXCEPTION 'device authorization repair failed'
        USING ERRCODE = 'P0001';
END;
$$;
\endif
