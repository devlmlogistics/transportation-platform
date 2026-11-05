export type DeviceEnrollmentRequestView = {
  request_id: string;
  correlation_code: string;
  user_account_id: string;
  identifier: string;
  legal_name: string;
  company_id: string;
  company_operation_id: string | null;
  installation_id: string;
  status: string;
  requested_at: string;
  approved_at: string | null;
  expires_at: string;
};

export type DeviceAuthorizationView = {
  authorization_id: string;
  user_account_id: string;
  identifier: string;
  legal_name: string;
  company_id: string;
  company_operation_id: string | null;
  installation_id: string;
  key_algorithm: string;
  status: string;
  requested_at: string;
  approved_at: string | null;
  expires_at: string | null;
  revoked_at: string | null;
};

type JSONRecord = Record<string, unknown>;

export class SupervisorRequestError extends Error {
  readonly status: number;

  constructor(status: number) {
    super(`Supervisor request failed (${status})`);
    this.name = "SupervisorRequestError";
    this.status = status;
  }
}

const isJSONRecord = (value: unknown): value is JSONRecord =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const field = (record: JSONRecord, snakeCase: string, pascalCase: string): unknown =>
  record[snakeCase] ?? record[pascalCase];

const stringField = (record: JSONRecord, snakeCase: string, pascalCase: string): string => {
  const value = field(record, snakeCase, pascalCase);
  return typeof value === "string" ? value : "";
};

const nullableStringField = (record: JSONRecord, snakeCase: string, pascalCase: string): string | null => {
  const value = field(record, snakeCase, pascalCase);
  return value == null ? null : typeof value === "string" ? value : "";
};

const normalizeDeviceEnrollmentRequest = (value: unknown): DeviceEnrollmentRequestView => {
  if (!isJSONRecord(value)) throw new Error("Invalid device enrollment request response");
  return {
    request_id: stringField(value, "request_id", "ID"),
    correlation_code: stringField(value, "correlation_code", "CorrelationCode"),
    user_account_id: stringField(value, "user_account_id", "UserAccountID"),
    identifier: stringField(value, "identifier", "Identifier"),
    legal_name: stringField(value, "legal_name", "LegalName"),
    company_id: stringField(value, "company_id", "CompanyID"),
    company_operation_id: nullableStringField(value, "company_operation_id", "CompanyOperationID"),
    installation_id: stringField(value, "installation_id", "InstallationID"),
    status: stringField(value, "status", "Status"),
    requested_at: stringField(value, "requested_at", "RequestedAt"),
    approved_at: nullableStringField(value, "approved_at", "ApprovedAt"),
    expires_at: stringField(value, "expires_at", "ExpiresAt"),
  };
};

const normalizeDeviceAuthorization = (value: unknown): DeviceAuthorizationView => {
  if (!isJSONRecord(value)) throw new Error("Invalid device authorization response");
  return {
    authorization_id: stringField(value, "authorization_id", "ID"),
    user_account_id: stringField(value, "user_account_id", "UserAccountID"),
    identifier: stringField(value, "identifier", "Identifier"),
    legal_name: stringField(value, "legal_name", "LegalName"),
    company_id: stringField(value, "company_id", "CompanyID"),
    company_operation_id: nullableStringField(value, "company_operation_id", "CompanyOperationID"),
    installation_id: stringField(value, "installation_id", "InstallationID"),
    key_algorithm: stringField(value, "key_algorithm", "KeyAlgorithm"),
    status: stringField(value, "status", "Status"),
    requested_at: stringField(value, "requested_at", "RequestedAt"),
    approved_at: nullableStringField(value, "approved_at", "ApprovedAt"),
    expires_at: nullableStringField(value, "expires_at", "ExpiresAt"),
    revoked_at: nullableStringField(value, "revoked_at", "RevokedAt"),
  };
};

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const requireUUID = (value: string, resource: string): string => {
  if (!UUID_PATTERN.test(value.trim())) throw new Error(`Invalid ${resource} id`);
  return value.trim();
};

const csrfToken = (): string =>
  document.cookie
    .split(";")
    .map((item) => item.trim())
    .find((item) => item.startsWith("tp_csrf="))
    ?.slice("tp_csrf=".length) ?? "";

const request = async <T>(path: string, init: RequestInit = {}): Promise<T> => {
  const method = (init.method ?? "GET").toUpperCase();
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (method !== "GET" && method !== "HEAD") {
    headers.set("Content-Type", "application/json");
    headers.set("X-CSRF-Token", csrfToken());
  }
  const response = await fetch(path, { ...init, credentials: "include", headers });
  if (!response.ok) throw new SupervisorRequestError(response.status);
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
};

export async function loginSupervisor(identifier: string, password: string): Promise<void> {
  await request("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ identifier, password }),
  });
}

export const listSupervisorDeviceRequests = async (signal?: AbortSignal): Promise<DeviceEnrollmentRequestView[]> =>
  (await request<unknown[]>("/api/v1/auth/webauthn/device-enrollment-requests", { signal })).map(normalizeDeviceEnrollmentRequest);

export const listSupervisorDeviceAuthorizations = async (signal?: AbortSignal): Promise<DeviceAuthorizationView[]> =>
  (await request<unknown[]>("/api/v1/auth/webauthn/device-authorizations", { signal })).map(normalizeDeviceAuthorization);

export const approveSupervisorDeviceRequest = async (requestID: string, reason: string): Promise<unknown> => {
  const validRequestID = requireUUID(requestID, "request");
  return request(`/api/v1/auth/webauthn/device-enrollment-requests/${encodeURIComponent(validRequestID)}/approve`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
};

export const revokeSupervisorDevice = async (authorizationID: string, reason: string): Promise<unknown> => {
  const validAuthorizationID = requireUUID(authorizationID, "authorization");
  return request(`/api/v1/auth/webauthn/device-authorizations/${encodeURIComponent(validAuthorizationID)}/revoke`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
};
