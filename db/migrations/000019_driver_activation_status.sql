-- +goose Up

-- Read-only status lookup for the driver's own activation request. The
-- session, account and installation are all bound inside the SECURITY DEFINER
-- boundary; the HTTP caller cannot choose another user's request by UUID.
-- Expiration is reported without mutating the request row.
-- +goose StatementBegin
CREATE FUNCTION platform.get_driver_activation_request_status(
    p_session_token_hash bytea,
    p_request_id uuid,
    p_installation_id uuid
)
RETURNS TABLE (
    request_id uuid,
    correlation_code text,
    company_id uuid,
    company_operation_id uuid,
    installation_id uuid,
    status text,
    expires_at timestamptz,
    authorization_status text
)
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, platform
AS $$
    SELECT d.id,
           d.correlation_code,
           d.company_id,
           d.company_operation_id,
           d.installation_id,
           CASE
               WHEN da.status = 'REVOKED' THEN 'REVOKED'::text
               WHEN d.status = 'PENDING' AND d.expires_at <= clock_timestamp() THEN 'EXPIRED'::text
               ELSE d.status
           END,
           d.expires_at,
           da.status
      FROM platform.auth_session AS s
      JOIN platform.user_account AS ua ON ua.id = s.user_account_id
      JOIN platform.person AS p ON p.id = ua.person_id
      JOIN platform.device_enrollment_request AS d
        ON d.user_account_id = s.user_account_id
      LEFT JOIN LATERAL (
          SELECT da_row.status
            FROM platform.device_authorization AS da_row
           WHERE da_row.user_account_id = s.user_account_id
             AND da_row.installation_id = d.installation_id
             AND da_row.company_id = d.company_id
             AND da_row.company_operation_id IS NOT DISTINCT FROM d.company_operation_id
           ORDER BY da_row.requested_at DESC
           LIMIT 1
      ) AS da ON true
     WHERE octet_length(p_session_token_hash) = 32
       AND s.token_hash = p_session_token_hash
       AND s.status = 'ACTIVE'
       AND s.session_policy = 'DRIVER_ACTIVATION'
       AND s.idle_expires_at > clock_timestamp()
       AND s.absolute_expires_at > clock_timestamp()
       AND s.session_epoch = ua.session_epoch
       AND ua.status = 'ACTIVE'
       AND p.status = 'ACTIVE'
       AND d.id = p_request_id
       AND d.installation_id = p_installation_id;
$$;
-- +goose StatementEnd

ALTER FUNCTION platform.get_driver_activation_request_status(bytea, uuid, uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.get_driver_activation_request_status(bytea, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.get_driver_activation_request_status(bytea, uuid, uuid) TO tp_auth_runtime;

-- +goose Down

REVOKE EXECUTE ON FUNCTION platform.get_driver_activation_request_status(bytea, uuid, uuid) FROM tp_auth_runtime;
DROP FUNCTION platform.get_driver_activation_request_status(bytea, uuid, uuid);
