import type { DriverActivationRequest, DriverInstallation } from "./webauthn";

export type DriverInvitationPreview = {
  invitation_id: string;
  identifier: string;
  legal_name: string;
  company_id: string;
  company_name: string;
  company_operation_id: string | null;
  operation_name: string | null;
  expires_at: string;
};

const readCSRFToken = (): string =>
  document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith("tp_csrf="))?.slice("tp_csrf=".length) ?? "";

const invitationRequest = async <T>(path: string, body: unknown): Promise<T> => {
  const response = await fetch(path, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", Accept: "application/json", "X-CSRF-Token": readCSRFToken() },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error(`Driver invitation request failed (${response.status})`);
  return (await response.json()) as T;
};

export const validateDriverInvitation = async (token: string): Promise<DriverInvitationPreview> =>
  invitationRequest<DriverInvitationPreview>("/api/v1/driver/invitations/validate", { token });

export const consumeDriverInvitation = async (token: string, confirmationCode: string, installation: DriverInstallation): Promise<DriverActivationRequest> => {
  const response = await invitationRequest<{
    request_id: string;
    correlation_code: string;
    status: string;
    expires_at: string;
    installation_id: string;
  }>("/api/v1/driver/invitations/consume", {
    token,
    confirmation_code: confirmationCode,
    installation_id: installation.id,
    installation_public_key: installation.publicKeyEncoded,
    key_algorithm: "ECDSA_P256_SHA256",
  });
  return {
    id: response.request_id,
    correlationCode: response.correlation_code,
    status: response.status,
    expiresAt: response.expires_at,
    installationId: response.installation_id,
  };
};

export const readInvitationTokenFromLocation = (): string | null => {
  const hash = window.location.hash.replace(/^#/, "");
  const prefix = "driver-invite=";
  if (!hash.startsWith(prefix)) return null;
  const token = hash.slice(prefix.length).trim();
  if (!token) return null;
  window.history.replaceState({}, document.title, `${window.location.pathname}${window.location.search}`);
  return token;
};
