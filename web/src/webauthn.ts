type JSONValue = Record<string, unknown>;

export type DriverActivationRequest = {
  id: string;
  correlationCode: string;
  status: string;
  expiresAt: string;
  installationId: string;
};

export type DriverActivationRequestStatus = DriverActivationRequest & {
  authorizationStatus: string | null;
};

export class DriverActivationStatusError extends Error {
  readonly status: number;

  constructor(status: number) {
    super(`Driver activation status request failed (${status})`);
    this.name = "DriverActivationStatusError";
    this.status = status;
  }
}

type CeremonyCredential = {
  id: string;
  rawId: string;
  type: string;
  response: {
    clientDataJSON: string;
    attestationObject?: string;
    authenticatorData?: string;
    signature?: string;
    userHandle?: string | null;
  };
};

const encode = (value: ArrayBuffer): string => {
  const bytes = new Uint8Array(value);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
};

const decode = (value: string): ArrayBuffer => {
  if (value.length % 4 === 1) throw new Error("Invalid base64url value");
  const padding = (4 - (value.length % 4)) % 4;
  const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "=".repeat(padding);
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  return bytes.buffer;
};

const unwrapPublicKeyOptions = (value: JSONValue): JSONValue => {
  const nested = value.publicKey;
  if (nested && typeof nested === "object" && !Array.isArray(nested)) return nested as JSONValue;
  return value;
};

const driverLoginErrorType = (error: unknown): string => {
  if (error && typeof error === "object" && "name" in error && typeof error.name === "string" && error.name) {
    return error.name;
  }
  if (error instanceof Error && error.name) return error.name;
  return "UnknownError";
};

const reportDriverLoginDiagnostic = (stage: string, error?: unknown): void => {
  if (!import.meta.env.DEV) return;
  console.debug("[driver-login]", { stage, error_type: error ? driverLoginErrorType(error) : undefined });
};

export const driverLoginErrorMessage = (error: unknown): string => {
  if (import.meta.env.DEV) return `El ingreso móvil fue rechazado (${driverLoginErrorType(error)}).`;
  return "El ingreso móvil fue rechazado o la autorización del dispositivo ya no está vigente.";
};

const credentialPayload = (credential: PublicKeyCredential): CeremonyCredential => {
  const response = credential.response as AuthenticatorAttestationResponse | AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: encode(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: encode(response.clientDataJSON),
      ...("attestationObject" in response ? { attestationObject: encode(response.attestationObject) } : {}),
      ...( "authenticatorData" in response ? { authenticatorData: encode(response.authenticatorData) } : {}),
      ...( "signature" in response ? { signature: encode(response.signature) } : {}),
      ...( "userHandle" in response ? { userHandle: response.userHandle ? encode(response.userHandle) : null } : {}),
    },
  };
};

const requestJSON = async (path: string, body: JSONValue): Promise<JSONValue> => {
  const response = await fetch(path, {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": readCSRFToken() },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw new Error("WebAuthn request was rejected");
  return (await response.json()) as JSONValue;
};

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const activationField = (value: JSONValue, snakeCase: string, pascalCase: string): unknown =>
  value[snakeCase] ?? value[pascalCase];

const normalizeDriverActivationRequest = (value: JSONValue, installationId = ""): DriverActivationRequest => {
  const id = String(value.request_id ?? value.RequestID ?? activationField(value, "id", "ID") ?? "");
  if (!UUID_PATTERN.test(id)) throw new Error("The activation request id was not returned by the server");
  return {
    id,
    correlationCode: String(activationField(value, "correlation_code", "CorrelationCode") ?? ""),
    status: String(activationField(value, "status", "Status") ?? "PENDING"),
    expiresAt: String(activationField(value, "expires_at", "ExpiresAt") ?? ""),
    installationId: String(activationField(value, "installation_id", "InstallationID") ?? installationId),
  };
};

const normalizeDriverActivationStatus = (value: JSONValue): DriverActivationRequestStatus => ({
  ...normalizeDriverActivationRequest(value),
  authorizationStatus: (() => {
    const status = activationField(value, "authorization_status", "AuthorizationStatus");
    return status == null ? null : String(status);
  })(),
});

const readCSRFToken = (): string => document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith("tp_csrf="))?.slice("tp_csrf=".length) ?? "";

export type RegistrationStage = "options_received" | "browser_started" | "browser_completed" | "finished";

export async function registerPasskey(displayName = "", deviceRequestID?: string, onStage?: (stage: RegistrationStage) => void): Promise<void> {
  if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys");
  const started = await requestJSON("/api/v1/auth/webauthn/registration/options", { display_name: displayName, ...(deviceRequestID ? { device_request_id: deviceRequestID } : {}) });
  onStage?.("options_received");
  const options = unwrapPublicKeyOptions(started.options as JSONValue);
  const publicKey = { ...options, challenge: decode(String(options.challenge)), user: { ...(options.user as JSONValue), id: decode(String((options.user as JSONValue).id)) }, ...(Array.isArray(options.excludeCredentials) ? { excludeCredentials: (options.excludeCredentials as JSONValue[]).map((item) => ({ ...item, id: decode(String(item.id)) })) } : {}) } as PublicKeyCredentialCreationOptions;
  onStage?.("browser_started");
  const credential = await navigator.credentials.create({ publicKey });
  onStage?.("browser_completed");
  if (!(credential instanceof PublicKeyCredential)) throw new Error("No passkey was returned");
  await requestJSON("/api/v1/auth/webauthn/registration/finish", { challenge_id: String(started.challenge_id), display_name: displayName, credential: credentialPayload(credential) as unknown as JSONValue });
  onStage?.("finished");
}

export async function completeStepUp(): Promise<void> {
  if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys");
  const started = await requestJSON("/api/v1/auth/webauthn/step-up/options", {});
  const options = unwrapPublicKeyOptions(started.options as JSONValue);
  const publicKey = { ...options, challenge: decode(String(options.challenge)), ...(Array.isArray(options.allowCredentials) ? { allowCredentials: (options.allowCredentials as JSONValue[]).map((item) => ({ ...item, id: decode(String(item.id)) })) } : {}) } as PublicKeyCredentialRequestOptions;
  const credential = await navigator.credentials.get({ publicKey });
  if (!(credential instanceof PublicKeyCredential)) throw new Error("No passkey was returned");
  await requestJSON("/api/v1/auth/webauthn/step-up/finish", { challenge_id: String(started.challenge_id), credential: credentialPayload(credential) as unknown as JSONValue });
}

export type DriverInstallation = {
  id: string;
  publicKey: CryptoKey;
  privateKey: CryptoKey;
  publicKeyEncoded: string;
};

const driverInstallationDBName = "transportation-platform-driver-auth";
const driverInstallationStoreName = "installations";
const driverActivationRequestStoreName = "activation-requests";

type StoredDriverInstallation = DriverInstallation & { createdAt: number };

const openDriverInstallationDB = (): Promise<IDBDatabase> => {
  if (typeof indexedDB === "undefined") throw new Error("IndexedDB is unavailable");
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(driverInstallationDBName, 2);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(driverInstallationStoreName)) {
        request.result.createObjectStore(driverInstallationStoreName, { keyPath: "id" });
      }
      if (!request.result.objectStoreNames.contains(driverActivationRequestStoreName)) {
        request.result.createObjectStore(driverActivationRequestStoreName, { keyPath: "installationId" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("IndexedDB could not be opened"));
  });
};

const persistDriverInstallation = async (installation: DriverInstallation): Promise<void> => {
  const database = await openDriverInstallationDB();
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction(driverInstallationStoreName, "readwrite");
    transaction.objectStore(driverInstallationStoreName).put({ ...installation, createdAt: Date.now() } satisfies StoredDriverInstallation);
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error ?? new Error("Installation could not be persisted"));
  });
  database.close();
};

export async function loadDriverInstallation(id?: string): Promise<DriverInstallation | undefined> {
  const database = await openDriverInstallationDB();
  const stored = await new Promise<StoredDriverInstallation | StoredDriverInstallation[] | undefined>((resolve, reject) => {
    const transaction = database.transaction(driverInstallationStoreName, "readonly");
    const request = id
      ? transaction.objectStore(driverInstallationStoreName).get(id)
      : transaction.objectStore(driverInstallationStoreName).getAll();
    request.onsuccess = () => resolve(request.result as StoredDriverInstallation | StoredDriverInstallation[] | undefined);
    request.onerror = () => reject(request.error ?? new Error("Installation could not be loaded"));
  });
  database.close();
  if (Array.isArray(stored)) {
    return stored.sort((left, right) => right.createdAt - left.createdAt)[0];
  }
  return stored;
}

export async function getOrCreateDriverInstallation(): Promise<DriverInstallation> {
  return (await loadDriverInstallation()) ?? createDriverInstallation();
}

export async function loadDriverActivationRequest(installationId: string): Promise<DriverActivationRequest | undefined> {
  const database = await openDriverInstallationDB();
  const stored = await new Promise<DriverActivationRequest | undefined>((resolve, reject) => {
    const transaction = database.transaction(driverActivationRequestStoreName, "readonly");
    const request = transaction.objectStore(driverActivationRequestStoreName).get(installationId);
    request.onsuccess = () => resolve(request.result as DriverActivationRequest | undefined);
    request.onerror = () => reject(request.error ?? new Error("Activation request could not be loaded"));
  });
  database.close();
  return stored;
}

export async function persistDriverActivationRequest(request: DriverActivationRequest): Promise<void> {
  const database = await openDriverInstallationDB();
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction(driverActivationRequestStoreName, "readwrite");
    transaction.objectStore(driverActivationRequestStoreName).put(request);
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error ?? new Error("Activation request could not be persisted"));
  });
  database.close();
}

export async function clearDriverActivationRequest(installationId: string): Promise<void> {
  const database = await openDriverInstallationDB();
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction(driverActivationRequestStoreName, "readwrite");
    transaction.objectStore(driverActivationRequestStoreName).delete(installationId);
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error ?? new Error("Activation request could not be cleared"));
  });
  database.close();
}

export async function createDriverInstallation(): Promise<DriverInstallation> {
  const id = crypto.randomUUID();
  const pair = (await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, false, ["sign", "verify"])) as CryptoKeyPair;
  const exported = await crypto.subtle.exportKey("raw", pair.publicKey);
  const installation = { id, publicKey: pair.publicKey, privateKey: pair.privateKey, publicKeyEncoded: encode(exported) };
  await persistDriverInstallation(installation);
  return installation;
}

export async function requestDriverActivation(companyID: string, operationID: string | undefined, installation: DriverInstallation): Promise<DriverActivationRequest> {
  const response = await requestJSON("/api/v1/auth/webauthn/device-enrollment-requests", {
    company_id: companyID,
    ...(operationID ? { company_operation_id: operationID } : {}),
    installation_id: installation.id,
    installation_public_key: installation.publicKeyEncoded,
    key_algorithm: "ECDSA_P256_SHA256",
  });
  const request = normalizeDriverActivationRequest(response, installation.id);
  await persistDriverActivationRequest(request);
  return request;
}

export async function queryDriverActivationStatus(requestID: string, installationID: string): Promise<DriverActivationRequestStatus> {
  const query = new URLSearchParams({ device_request_id: requestID, installation_id: installationID });
  let response: Response;
  try {
    response = await fetch(`/api/v1/driver/auth/activation/status?${query.toString()}`, {
      credentials: "include",
      headers: { Accept: "application/json" },
    });
  } catch (error) {
    throw error;
  }
  if (!response.ok) throw new DriverActivationStatusError(response.status);
  return normalizeDriverActivationStatus((await response.json()) as JSONValue);
}

export async function loginDriverWithPassword(identifier: string, password: string): Promise<void> {
  const response = await fetch("/api/v1/driver/auth/activation/login", {
    method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ identifier, password }),
  });
  if (!response.ok) throw new Error("Driver activation was rejected");
}

export async function loginDriverWithPasskey(identifier: string, installation: DriverInstallation): Promise<void> {
  if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys");
  reportDriverLoginDiagnostic("driver.login.installation_loaded");
  let started: JSONValue;
  try {
    started = await requestJSON("/api/v1/driver/auth/webauthn/options", { identifier, installation_id: installation.id });
  } catch (error) {
    reportDriverLoginDiagnostic("driver.login.options_received", error);
    throw error;
  }
  reportDriverLoginDiagnostic("driver.login.options_received");
  const options = unwrapPublicKeyOptions(started.options as JSONValue);
  const rawChallenge = decode(String(options.challenge));
  const publicKey = {
    ...options,
    challenge: rawChallenge,
    ...(Array.isArray(options.allowCredentials)
      ? { allowCredentials: (options.allowCredentials as JSONValue[]).map((item) => ({ ...item, id: decode(String(item.id)) })) }
      : {}),
  } as PublicKeyCredentialRequestOptions;
  reportDriverLoginDiagnostic("driver.login.webauthn_started");
  let credential: Credential | null;
  try {
    credential = await navigator.credentials.get({ publicKey });
  } catch (error) {
    reportDriverLoginDiagnostic("driver.login.webauthn_completed", error);
    throw error;
  }
  reportDriverLoginDiagnostic("driver.login.webauthn_completed");
  if (!(credential instanceof PublicKeyCredential)) throw new Error("No passkey was returned");
  const challengeHash = await crypto.subtle.digest("SHA-256", rawChallenge);
  const proofMessage = new Uint8Array(new TextEncoder().encode("tp-driver-possession\0").length + challengeHash.byteLength + 16);
  proofMessage.set(new TextEncoder().encode("tp-driver-possession\0"), 0);
  proofMessage.set(new Uint8Array(challengeHash), new TextEncoder().encode("tp-driver-possession\0").length);
  proofMessage.set(decodeUUID(installation.id), new TextEncoder().encode("tp-driver-possession\0").length + challengeHash.byteLength);
  let signature: ArrayBuffer;
  try {
    signature = await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, installation.privateKey, proofMessage);
  } catch (error) {
    reportDriverLoginDiagnostic("driver.login.possession_signed", error);
    throw error;
  }
  reportDriverLoginDiagnostic("driver.login.possession_signed");
  reportDriverLoginDiagnostic("driver.login.finish_started");
  try {
    await requestJSON("/api/v1/driver/auth/webauthn/finish", {
      challenge_id: String(started.challenge_id), credential: credentialPayload(credential) as unknown as JSONValue,
      possession_signature: encode(signature),
    });
  } catch (error) {
    reportDriverLoginDiagnostic("driver.login.finish_completed", error);
    throw error;
  }
  reportDriverLoginDiagnostic("driver.login.finish_completed");
}

const decodeUUID = (value: string): Uint8Array => {
  const hex = value.replaceAll("-", "");
  const result = new Uint8Array(16);
  for (let index = 0; index < result.length; index += 1) result[index] = Number.parseInt(hex.slice(index * 2, index * 2 + 2), 16);
  return result;
};
