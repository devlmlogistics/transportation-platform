-- +goose Up
INSERT INTO platform.permission(code, resource, action, description)
VALUES ('driver.manage','driver','manage','Explicit company driver provisioning and relationship management.');
INSERT INTO platform.permission_scope VALUES ('driver.manage','COMPANY');
-- Deliberately no automatic role_permission or grants to existing supervisors.

CREATE TABLE platform.company_driver_contact (
 company_id uuid NOT NULL REFERENCES platform.company(id),
 person_id uuid NOT NULL REFERENCES platform.person(id),
 given_name text NOT NULL, family_name text NOT NULL, phone text NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
 PRIMARY KEY(company_id,person_id),
 FOREIGN KEY(company_id,person_id) REFERENCES platform.company_membership(company_id,person_id)
);
CREATE TABLE platform.driver_provisioning_request (
 id uuid PRIMARY KEY, company_id uuid NOT NULL REFERENCES platform.company(id),
 requested_by uuid NOT NULL REFERENCES platform.user_account(id),
 identifier text NOT NULL, given_name text NOT NULL, family_name text NOT NULL, phone text NOT NULL,
 operation_ids uuid[] NOT NULL, status text NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','APPROVED')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX driver_link_one_pending ON platform.driver_provisioning_request(company_id, identifier) WHERE status = 'PENDING';
CREATE TABLE platform.driver_management_command (
 company_id uuid NOT NULL REFERENCES platform.company(id), id uuid NOT NULL,
 actor_id uuid NOT NULL REFERENCES platform.user_account(id), payload jsonb NOT NULL,
 result jsonb NOT NULL, PRIMARY KEY(company_id,id)
);
ALTER TABLE platform.company_driver_contact OWNER TO tp_owner;
ALTER TABLE platform.driver_provisioning_request OWNER TO tp_owner;
ALTER TABLE platform.driver_management_command OWNER TO tp_owner;
ALTER TABLE platform.company_driver_contact ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.company_driver_contact FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_provisioning_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_provisioning_request FORCE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_management_command ENABLE ROW LEVEL SECURITY;
ALTER TABLE platform.driver_management_command FORCE ROW LEVEL SECURITY;
CREATE POLICY driver_contact_owner ON platform.company_driver_contact TO tp_owner USING (true) WITH CHECK(true);
CREATE POLICY driver_link_owner ON platform.driver_provisioning_request TO tp_owner USING (true) WITH CHECK(true);
CREATE POLICY driver_command_owner ON platform.driver_management_command TO tp_owner USING (true) WITH CHECK(true);
-- Only the NOLOGIN owner can use these insert/update policies. Runtime has no DML.
CREATE POLICY driver_provision_profile ON platform.driver_profile FOR INSERT TO tp_owner WITH CHECK(current_setting('app.driver_provision',true)='1');
CREATE POLICY driver_provision_company_insert ON platform.company_membership FOR INSERT TO tp_owner WITH CHECK(current_setting('app.driver_provision',true)='1');
CREATE POLICY driver_provision_company_update ON platform.company_membership FOR UPDATE TO tp_owner USING(current_setting('app.driver_provision',true)='1') WITH CHECK(current_setting('app.driver_provision',true)='1');
CREATE POLICY driver_provision_operation_insert ON platform.operation_membership FOR INSERT TO tp_owner WITH CHECK(current_setting('app.driver_provision',true)='1');
CREATE POLICY driver_provision_operation_update ON platform.operation_membership FOR UPDATE TO tp_owner USING(current_setting('app.driver_provision',true)='1') WITH CHECK(current_setting('app.driver_provision',true)='1');

-- Internal helper: no runtime EXECUTE, no global identity updates, exact company only.
-- +goose StatementBegin
CREATE FUNCTION platform.set_driver_company_relationship(p_company uuid,p_person uuid,p_actor uuid,p_ops uuid[],p_status text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_op uuid;
BEGIN
 PERFORM set_config('app.driver_provision','1',true);
 INSERT INTO platform.company_membership(id,company_id,person_id,status,created_by_user_account_id)
 VALUES(gen_random_uuid(),p_company,p_person,p_status,p_actor)
 ON CONFLICT(company_id,person_id) DO UPDATE SET status=p_status, updated_at=clock_timestamp();
 -- Keep history and windows. Re-enabling an expired/revoked relationship requires a separate review.
 UPDATE platform.operation_membership SET status='SUSPENDED',updated_at=clock_timestamp()
 WHERE company_id=p_company AND person_id=p_person AND NOT(company_operation_id=ANY(p_ops));
 FOREACH v_op IN ARRAY p_ops LOOP
   INSERT INTO platform.operation_membership(id,company_id,company_operation_id,person_id,status,created_by_user_account_id)
   VALUES(gen_random_uuid(),p_company,v_op,p_person,p_status,p_actor)
   ON CONFLICT(company_operation_id,person_id) DO UPDATE SET status=p_status,updated_at=clock_timestamp();
 END LOOP;
 -- Scoped sessions must not revive later when a membership is enabled again.
 UPDATE platform.auth_session s SET status='REVOKED',revoked_at=clock_timestamp(),terminal_reason='driver_relationship_changed'
 FROM platform.device_authorization da
 WHERE s.device_authorization_id=da.id AND s.status='ACTIVE' AND da.company_id=p_company
 AND da.user_account_id=(SELECT id FROM platform.user_account WHERE person_id=p_person)
 AND (p_status='SUSPENDED' OR (da.company_operation_id IS NOT NULL AND NOT(da.company_operation_id=ANY(p_ops))));
END $$;
-- +goose StatementEnd

-- One transaction includes authorization, narrow provisioning and successful audit.
-- Denials return a value so their audit commits even though no mutation is applied.
-- +goose StatementBegin
CREATE FUNCTION platform.manage_company_drivers(p_session bytea,p_company uuid,p_action text,p_input jsonb,p_command uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE
 v_actor uuid; v_actor_person uuid; v_person uuid; v_account uuid; v_identifier text;
 v_ops uuid[]; v_op uuid; v_now timestamptz:=clock_timestamp(); v_result jsonb; v_saved record;
 v_contact platform.company_driver_contact%ROWTYPE; v_version bigint; v_request uuid;
BEGIN
 PERFORM set_config('app.auth_internal','1',true);
 PERFORM set_config('app.device_boundary','device-admin-v1',true);
 PERFORM set_config('app.driver_provision','1',true);
 SELECT ua.id,ua.person_id INTO v_actor,v_actor_person
 FROM platform.auth_session s JOIN platform.user_account ua ON ua.id=s.user_account_id
 JOIN platform.person p ON p.id=ua.person_id
 WHERE s.token_hash=p_session AND s.status='ACTIVE' AND s.session_policy='ADMINISTRATIVE'
 AND s.session_epoch=ua.session_epoch AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now
 AND ua.status='ACTIVE' AND p.status='ACTIVE' FOR UPDATE OF ua;
 IF v_actor IS NULL OR NOT platform.device_admin_scope_allowed(v_actor_person,p_company,NULL,'driver.manage',v_now) THEN
   INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,reason_code,details)
   VALUES(gen_random_uuid(),'DRIVER_MANAGEMENT_DENIED','DENIED',v_actor,'NOT_AUTHORIZED','{}');
   RETURN jsonb_build_object('status','FORBIDDEN');
 END IF;
 IF p_action='list' THEN
   SELECT coalesce(jsonb_agg(x ORDER BY x->>'legal_name'),'[]'::jsonb) INTO v_result FROM (
     SELECT jsonb_build_object('person_id',p.id,'identifier',li.normalized_identifier,
       'legal_name',coalesce(nullif(concat_ws(' ',ct.given_name,ct.family_name),''),p.legal_name),
       'phone',coalesce(ct.phone,''),'status',cm.status,'version',coalesce(ct.version,0),
       'operation_ids',coalesce((SELECT jsonb_agg(om.company_operation_id ORDER BY om.company_operation_id)
          FROM platform.operation_membership om WHERE om.company_id=p_company AND om.person_id=p.id
          AND om.status='ACTIVE' AND om.valid_from<=v_now AND (om.valid_until IS NULL OR om.valid_until>v_now)),'[]'::jsonb),
       'enrollment_status',CASE WHEN EXISTS(SELECT 1 FROM platform.device_authorization da WHERE da.user_account_id=ua.id AND da.company_id=p_company AND da.status='ACTIVE' AND (da.expires_at IS NULL OR da.expires_at>v_now)) THEN 'ACTIVE'
          WHEN EXISTS(SELECT 1 FROM platform.device_enrollment_request dr WHERE dr.user_account_id=ua.id AND dr.company_id=p_company AND dr.status IN('PENDING','APPROVED') AND dr.expires_at>v_now) THEN 'PENDING' ELSE 'NOT_ENROLLED' END) x
     FROM platform.person p JOIN platform.user_account ua ON ua.person_id=p.id
     JOIN platform.driver_profile dp ON dp.person_id=p.id
     JOIN platform.company_membership cm ON cm.person_id=p.id AND cm.company_id=p_company
     LEFT JOIN platform.login_identifier li ON li.user_account_id=ua.id AND li.status='ACTIVE'
     LEFT JOIN platform.company_driver_contact ct ON ct.person_id=p.id AND ct.company_id=p_company
     WHERE coalesce(p_input->>'search','')='' OR p.legal_name ILIKE '%'||(p_input->>'search')||'%'
        OR concat_ws(' ',ct.given_name,ct.family_name) ILIKE '%'||(p_input->>'search')||'%'
        OR li.normalized_identifier ILIKE '%'||(p_input->>'search')||'%'
     ORDER BY p.id LIMIT 200
   ) q;
   RETURN jsonb_build_object('status','OK','drivers',v_result);
 END IF;
 IF p_action NOT IN('create','update') OR p_command IS NULL OR p_command='00000000-0000-0000-0000-000000000000'::uuid THEN
   RETURN jsonb_build_object('status','INVALID');
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(p_company::text||p_command::text,24));
 SELECT * INTO v_saved FROM platform.driver_management_command WHERE company_id=p_company AND id=p_command;
 IF FOUND THEN
   IF v_saved.actor_id=v_actor AND v_saved.payload=jsonb_build_object('action',p_action,'input',p_input) THEN RETURN v_saved.result; END IF;
   RETURN jsonb_build_object('status','CONFLICT');
 END IF;
 IF p_action='create' AND NOT platform.consume_auth_rate_limit(p_session,'DRIVER_PROVISION',20,3600) THEN
   INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,company_id,reason_code)
   VALUES(gen_random_uuid(),'DRIVER_MANAGEMENT_DENIED','DENIED',v_actor,p_company,'RATE_LIMIT');
   RETURN jsonb_build_object('status','RATE_LIMITED');
 END IF;
 BEGIN
   SELECT coalesce(array_agg(DISTINCT value::uuid),'{}'::uuid[]) INTO v_ops FROM jsonb_array_elements_text(p_input->'operation_ids');
   IF cardinality(v_ops)>20 THEN RETURN jsonb_build_object('status','INVALID'); END IF;
   FOREACH v_op IN ARRAY v_ops LOOP
     IF NOT platform.device_scope_membership_active(v_actor_person,p_company,v_op,v_now) THEN
       INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,company_id,reason_code)
       VALUES(gen_random_uuid(),'DRIVER_MANAGEMENT_DENIED','DENIED',v_actor,p_company,'OPERATION_NOT_AUTHORIZED');
       RETURN jsonb_build_object('status','FORBIDDEN');
     END IF;
   END LOOP;
   IF p_action='create' THEN
     IF length(btrim(coalesce(p_input->>'given_name',''))) NOT BETWEEN 1 AND 100
       OR length(btrim(coalesce(p_input->>'family_name',''))) NOT BETWEEN 1 AND 100
       OR coalesce(p_input->>'phone','') !~ '^\+[1-9][0-9]{6,14}$'
       OR length(coalesce(p_input->>'identifier','')) NOT BETWEEN 3 AND 320
       OR coalesce(p_input->>'identifier','') !~ '^[a-z0-9][a-z0-9._+@-]+$' THEN RETURN jsonb_build_object('status','INVALID'); END IF;
     v_identifier:=p_input->>'identifier';
     PERFORM pg_advisory_xact_lock(hashtextextended(v_identifier,24));
     SELECT user_account_id INTO v_account FROM platform.login_identifier WHERE normalized_identifier=v_identifier;
     IF v_account IS NOT NULL THEN
       SELECT person_id INTO v_person FROM platform.user_account WHERE id=v_account;
       IF EXISTS(SELECT 1 FROM platform.company_membership WHERE company_id=p_company AND person_id=v_person) THEN
         RETURN jsonb_build_object('status','CONFLICT');
       END IF;
       -- No cross-tenant identity data leaves this boundary, and no automatic attachment.
       INSERT INTO platform.driver_provisioning_request(id,company_id,requested_by,identifier,given_name,family_name,phone,operation_ids)
       VALUES(gen_random_uuid(),p_company,v_actor,v_identifier,p_input->>'given_name',p_input->>'family_name',p_input->>'phone',v_ops)
       ON CONFLICT(company_id,identifier) WHERE status='PENDING' DO UPDATE SET identifier=EXCLUDED.identifier
       RETURNING id INTO v_request;
       v_result:=jsonb_build_object('status','REVIEW_REQUIRED','request_id',v_request);
     ELSE
       v_person:=gen_random_uuid(); v_account:=gen_random_uuid();
       INSERT INTO platform.person(id,legal_name) VALUES(v_person,(p_input->>'given_name')||' '||(p_input->>'family_name'));
       INSERT INTO platform.user_account(id,person_id,status) VALUES(v_account,v_person,'ACTIVE');
       INSERT INTO platform.driver_profile(id,person_id) VALUES(gen_random_uuid(),v_person);
       INSERT INTO platform.login_identifier(id,user_account_id,normalized_identifier) VALUES(gen_random_uuid(),v_account,v_identifier);
       PERFORM platform.set_driver_company_relationship(p_company,v_person,v_actor,v_ops,'ACTIVE');
       INSERT INTO platform.company_driver_contact(company_id,person_id,given_name,family_name,phone)
       VALUES(p_company,v_person,p_input->>'given_name',p_input->>'family_name',p_input->>'phone');
       v_result:=jsonb_build_object('status','CREATED','person_id',v_person);
     END IF;
   ELSE
     v_person:=(p_input->>'person_id')::uuid;
     PERFORM 1 FROM platform.company_membership cm JOIN platform.driver_profile dp ON dp.person_id=cm.person_id
       WHERE cm.company_id=p_company AND cm.person_id=v_person FOR UPDATE OF cm;
     IF NOT FOUND THEN RETURN jsonb_build_object('status','NOT_FOUND'); END IF;
     SELECT * INTO v_contact FROM platform.company_driver_contact WHERE company_id=p_company AND person_id=v_person FOR UPDATE;
     v_version:=coalesce(v_contact.version,0);
     IF v_version IS DISTINCT FROM (p_input->>'version')::bigint THEN RETURN jsonb_build_object('status','CONFLICT'); END IF;
     IF p_input->>'status' NOT IN('ACTIVE','SUSPENDED') OR p_input->>'status' IS NULL THEN RETURN jsonb_build_object('status','INVALID'); END IF;
     -- Revoked/expired relationships cannot be resurrected through this UI.
     IF EXISTS(SELECT 1 FROM platform.company_membership WHERE company_id=p_company AND person_id=v_person AND
         (status='REVOKED' OR valid_from>v_now OR valid_until<=v_now)) THEN RETURN jsonb_build_object('status','REVIEW_REQUIRED'); END IF;
     IF EXISTS(SELECT 1 FROM platform.operation_membership WHERE company_id=p_company AND person_id=v_person AND company_operation_id=ANY(v_ops)
         AND (status='REVOKED' OR valid_from>v_now OR valid_until<=v_now)) THEN RETURN jsonb_build_object('status','REVIEW_REQUIRED'); END IF;
     -- A driver permission cannot reactivate unrelated administrative roles.
     IF EXISTS(SELECT 1 FROM platform.role_assignment WHERE company_id=p_company AND person_id=v_person
         AND role_code<>'DRIVER' AND lifecycle_status='ACTIVE' AND (valid_until IS NULL OR valid_until>v_now))
         THEN RETURN jsonb_build_object('status','REVIEW_REQUIRED'); END IF;
     PERFORM platform.set_driver_company_relationship(p_company,v_person,v_actor,v_ops,p_input->>'status');
     IF v_version=0 THEN
       INSERT INTO platform.company_driver_contact(company_id,person_id,given_name,family_name,phone)
       SELECT p_company,v_person,legal_name,'','' FROM platform.person WHERE id=v_person;
     ELSE
       UPDATE platform.company_driver_contact SET version=version+1 WHERE company_id=p_company AND person_id=v_person;
     END IF;
     v_result:=jsonb_build_object('status','UPDATED');
   END IF;
   INSERT INTO platform.driver_management_command VALUES(p_company,p_command,v_actor,jsonb_build_object('action',p_action,'input',p_input),v_result);
   INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,company_id,reason_code,details)
   VALUES(gen_random_uuid(),'DRIVER_MANAGEMENT_CHANGED','SUCCESS',v_actor,p_company,upper(p_action),jsonb_build_object('result',v_result->>'status'));
   RETURN v_result;
 EXCEPTION WHEN unique_violation THEN RETURN jsonb_build_object('status','CONFLICT');
 WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN jsonb_build_object('status','INVALID');
 END;
END $$;
-- +goose StatementEnd
ALTER FUNCTION platform.set_driver_company_relationship(uuid,uuid,uuid,uuid[],text) OWNER TO tp_owner;
ALTER FUNCTION platform.manage_company_drivers(bytea,uuid,text,jsonb,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.set_driver_company_relationship(uuid,uuid,uuid,uuid[],text) FROM PUBLIC,tp_runtime,tp_auth_runtime,tp_driver_auth_runtime,tp_bootstrap_runtime;
REVOKE ALL ON FUNCTION platform.manage_company_drivers(bytea,uuid,text,jsonb,uuid) FROM PUBLIC,tp_runtime,tp_driver_auth_runtime,tp_bootstrap_runtime;
GRANT EXECUTE ON FUNCTION platform.manage_company_drivers(bytea,uuid,text,jsonb,uuid) TO tp_auth_runtime;
REVOKE ALL ON platform.company_driver_contact,platform.driver_provisioning_request,platform.driver_management_command FROM PUBLIC,tp_runtime,tp_auth_runtime,tp_driver_auth_runtime,tp_bootstrap_runtime;

-- Existing identities require a PLATFORM reviewer with a current step-up session.
-- No public endpoint or self-service approval is exposed in this delivery.
-- +goose StatementBegin
CREATE FUNCTION platform.approve_driver_company_link(p_session bytea,p_request uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,platform AS $$
DECLARE v_actor uuid; v_person uuid; v_account uuid; v_request platform.driver_provisioning_request%ROWTYPE; v_op uuid; v_now timestamptz:=clock_timestamp();
BEGIN
 PERFORM set_config('app.auth_internal','1',true);
 PERFORM set_config('app.device_boundary','device-admin-v1',true);
 PERFORM set_config('app.driver_provision','1',true);
 SELECT ua.id INTO v_actor FROM platform.auth_session s
 JOIN platform.user_account ua ON ua.id=s.user_account_id JOIN platform.person p ON p.id=ua.person_id
 WHERE s.token_hash=p_session AND s.status='ACTIVE' AND s.session_policy='ADMINISTRATIVE'
 AND s.authentication_level='WEBAUTHN_STEP_UP' AND s.step_up_expires_at>v_now
 AND s.idle_expires_at>v_now AND s.absolute_expires_at>v_now AND s.session_epoch=ua.session_epoch
 AND ua.status='ACTIVE' AND p.status='ACTIVE'
 AND EXISTS(SELECT 1 FROM platform.authenticator_credential ac WHERE ac.id=s.step_up_credential_id AND ac.user_account_id=ua.id AND ac.status='ACTIVE')
 AND EXISTS(SELECT 1 FROM platform.role_assignment ra JOIN platform.role r ON r.code=ra.role_code AND r.status='ACTIVE'
 WHERE ra.person_id=ua.person_id AND ra.role_code='GLOBAL_ADMIN' AND ra.scope_type='PLATFORM'
 AND ra.lifecycle_status='ACTIVE' AND ra.valid_from<=v_now AND (ra.valid_until IS NULL OR ra.valid_until>v_now))
 FOR UPDATE OF ua;
 IF v_actor IS NULL THEN RETURN false; END IF;
 SELECT * INTO v_request FROM platform.driver_provisioning_request WHERE id=p_request FOR UPDATE;
 IF NOT FOUND OR v_request.status<>'PENDING' OR v_actor=v_request.requested_by THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM platform.user_account ua JOIN platform.person p ON p.id=ua.person_id
 WHERE ua.id=v_request.requested_by AND ua.status='ACTIVE' AND p.status='ACTIVE'
 AND platform.device_admin_scope_allowed(p.id,v_request.company_id,NULL,'driver.manage',v_now)) THEN RETURN false; END IF;
 FOREACH v_op IN ARRAY v_request.operation_ids LOOP
 IF NOT platform.device_scope_membership_active((SELECT person_id FROM platform.user_account WHERE id=v_request.requested_by),v_request.company_id,v_op,v_now) THEN RETURN false; END IF;
 END LOOP;
 SELECT ua.person_id,ua.id INTO v_person,v_account FROM platform.login_identifier li JOIN platform.user_account ua ON ua.id=li.user_account_id
 JOIN platform.person p ON p.id=ua.person_id WHERE li.normalized_identifier=v_request.identifier AND li.status='ACTIVE' AND ua.status='ACTIVE' AND p.status='ACTIVE' FOR UPDATE OF ua;
 IF NOT FOUND THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM platform.company_membership WHERE company_id=v_request.company_id AND person_id=v_person) THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM platform.driver_profile WHERE person_id=v_person AND status<>'ACTIVE') THEN RETURN false; END IF;
 INSERT INTO platform.driver_profile(id,person_id) VALUES(gen_random_uuid(),v_person) ON CONFLICT(person_id) DO NOTHING;
 PERFORM platform.set_driver_company_relationship(v_request.company_id,v_person,v_actor,v_request.operation_ids,'ACTIVE');
 INSERT INTO platform.company_driver_contact(company_id,person_id,given_name,family_name,phone)
 VALUES(v_request.company_id,v_person,v_request.given_name,v_request.family_name,v_request.phone);
 UPDATE platform.driver_provisioning_request SET status='APPROVED' WHERE id=p_request;
 INSERT INTO platform.security_audit_event(id,event_type,outcome,actor_user_account_id,subject_user_account_id,company_id,reason_code)
 VALUES(gen_random_uuid(),'DRIVER_COMPANY_LINK_APPROVED','SUCCESS',v_actor,v_account,v_request.company_id,'PLATFORM_REVIEW');
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION platform.approve_driver_company_link(bytea,uuid) OWNER TO tp_owner;
REVOKE ALL ON FUNCTION platform.approve_driver_company_link(bytea,uuid) FROM PUBLIC,tp_runtime,tp_driver_auth_runtime,tp_bootstrap_runtime;
GRANT EXECUTE ON FUNCTION platform.approve_driver_company_link(bytea,uuid) TO tp_auth_runtime;

-- +goose Down
DROP FUNCTION platform.approve_driver_company_link(bytea,uuid);
DROP FUNCTION platform.manage_company_drivers(bytea,uuid,text,jsonb,uuid);
DROP FUNCTION platform.set_driver_company_relationship(uuid,uuid,uuid,uuid[],text);
DROP POLICY driver_provision_profile ON platform.driver_profile;
DROP POLICY driver_provision_company_insert ON platform.company_membership;
DROP POLICY driver_provision_company_update ON platform.company_membership;
DROP POLICY driver_provision_operation_insert ON platform.operation_membership;
DROP POLICY driver_provision_operation_update ON platform.operation_membership;
DROP TABLE platform.driver_management_command;
DROP TABLE platform.driver_provisioning_request;
DROP TABLE platform.company_driver_contact;
DELETE FROM platform.permission_scope WHERE permission_code='driver.manage';
DELETE FROM platform.permission WHERE code='driver.manage';
