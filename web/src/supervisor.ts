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
  if (!response.ok) throw new Error(`Supervisor request failed (${response.status})`);
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
};

export async function loginSupervisor(identifier: string, password: string): Promise<void> {
  await request("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ identifier, password }),
  });
}

export const listSupervisorDeviceRequests = (): Promise<DeviceEnrollmentRequestView[]> =>
  request<DeviceEnrollmentRequestView[]>("/api/v1/auth/webauthn/device-enrollment-requests");

export const listSupervisorDeviceAuthorizations = (): Promise<DeviceAuthorizationView[]> =>
  request<DeviceAuthorizationView[]>("/api/v1/auth/webauthn/device-authorizations");

export const approveSupervisorDeviceRequest = (requestID: string, reason: string): Promise<unknown> =>
  request(`/api/v1/auth/webauthn/device-enrollment-requests/${encodeURIComponent(requestID)}/approve`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });

export const revokeSupervisorDevice = (authorizationID: string, reason: string): Promise<unknown> =>
  request(`/api/v1/auth/webauthn/device-authorizations/${encodeURIComponent(authorizationID)}/revoke`, {
    method: "POST",
    body: JSON.stringify({ reason }),
  });
