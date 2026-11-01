type JSONValue = Record<string, unknown>;

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
  const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((value.length + 3) % 4);
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  return bytes.buffer;
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

const readCSRFToken = (): string => document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith("tp_csrf="))?.slice("tp_csrf=".length) ?? "";

export async function registerPasskey(displayName = "", deviceRequestID?: string): Promise<void> {
  if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys");
  const started = await requestJSON("/api/v1/auth/webauthn/registration/options", { display_name: displayName, ...(deviceRequestID ? { device_request_id: deviceRequestID } : {}) });
  const options = started.options as JSONValue;
  const publicKey = { ...options, challenge: decode(String(options.challenge)), user: { ...(options.user as JSONValue), id: decode(String((options.user as JSONValue).id)) }, ...(Array.isArray(options.excludeCredentials) ? { excludeCredentials: (options.excludeCredentials as JSONValue[]).map((item) => ({ ...item, id: decode(String(item.id)) })) } : {}) } as PublicKeyCredentialCreationOptions;
  const credential = await navigator.credentials.create({ publicKey });
  if (!(credential instanceof PublicKeyCredential)) throw new Error("No passkey was returned");
  await requestJSON("/api/v1/auth/webauthn/registration/finish", { challenge_id: String(started.challenge_id), display_name: displayName, credential: credentialPayload(credential) as unknown as JSONValue });
}

export async function completeStepUp(): Promise<void> {
  if (!window.PublicKeyCredential) throw new Error("This browser does not support passkeys");
  const started = await requestJSON("/api/v1/auth/webauthn/step-up/options", {});
  const options = started.options as JSONValue;
  const publicKey = { ...options, challenge: decode(String(options.challenge)), ...(Array.isArray(options.allowCredentials) ? { allowCredentials: (options.allowCredentials as JSONValue[]).map((item) => ({ ...item, id: decode(String(item.id)) })) } : {}) } as PublicKeyCredentialRequestOptions;
  const credential = await navigator.credentials.get({ publicKey });
  if (!(credential instanceof PublicKeyCredential)) throw new Error("No passkey was returned");
  await requestJSON("/api/v1/auth/webauthn/step-up/finish", { challenge_id: String(started.challenge_id), credential: credentialPayload(credential) as unknown as JSONValue });
}
