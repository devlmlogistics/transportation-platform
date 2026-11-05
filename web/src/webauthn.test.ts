import { afterEach, describe, expect, it, vi } from "vitest";
import {
  associateDriverInstallationIdentifier,
  clearDriverInstallationIdentifier,
  createDriverInstallation,
  driverLoginErrorMessage,
  loadDriverInstallation,
  loginDriverWithPasskey,
  queryDriverActivationStatus,
  registerPasskey,
  requestDriverActivation,
  type DriverInstallation,
} from "./webauthn";

const challenge = "AAECAwQFBgcICQoLDA0ODw";
const credentialID = "EBESExQVFhcYGRobHB0eHw";
const challengeID = "11111111-1111-4111-8111-111111111111";

const responseBytes = (size: number): ArrayBuffer => new Uint8Array(size).fill(7).buffer;

class FakePublicKeyCredential {
  readonly id = "credential-id";
  readonly rawId = responseBytes(16);
  readonly type = "public-key";
  readonly response = {
    clientDataJSON: responseBytes(8),
    authenticatorData: responseBytes(37),
    signature: responseBytes(64),
    userHandle: responseBytes(16),
  } as AuthenticatorAssertionResponse;
}

class FakeRegistrationCredential {
  readonly id = "registration-credential";
  readonly rawId = responseBytes(16);
  readonly type = "public-key";
  readonly response = { clientDataJSON: responseBytes(8), attestationObject: responseBytes(64) } as AuthenticatorAttestationResponse;
}

const installCredentialAPI = (credential: PublicKeyCredential | null, error?: Error) => {
  vi.stubGlobal("PublicKeyCredential", FakePublicKeyCredential);
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: { get: vi.fn(async () => (error ? Promise.reject(error) : credential)) },
  });
};

const createInstallation = (): DriverInstallation => ({
  id: "78f5e1f4-4f86-4e5f-8adf-955cb7d23337",
  publicKey: {} as CryptoKey,
  privateKey: {} as CryptoKey,
  publicKeyEncoded: "public-key",
});

const installCryptoAPI = () => {
  const digest = new Uint8Array(32).fill(3).buffer;
  const signature = new Uint8Array(64).fill(4).buffer;
  const sign = vi.fn(async () => signature);
  vi.stubGlobal("crypto", {
    subtle: {
      digest: vi.fn(async () => digest),
      sign,
    },
  });
  return { sign };
};

const response = (body: unknown, ok = true, status?: number): Response =>
  new Response(JSON.stringify(body), { status: status ?? (ok ? 200 : 401), headers: { "Content-Type": "application/json" } });

describe("driver WebAuthn browser boundary", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("unwraps publicKey options, converts challenge and credential IDs, signs possession, and sends finish", async () => {
    const installation = createInstallation();
    const credential = new FakePublicKeyCredential() as unknown as PublicKeyCredential;
    installCredentialAPI(credential);
    const { sign } = installCryptoAPI();
    document.cookie = "tp_csrf=csrf-token";
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({
      challenge_id: challengeID,
      options: {
        publicKey: {
          challenge,
          rpId: "localhost",
          allowCredentials: [{ type: "public-key", id: credentialID }],
          userVerification: "required",
        },
      },
    })).mockResolvedValueOnce(response({}));

    await loginDriverWithPasskey("driver@example.test", installation);

    const getCall = vi.mocked(navigator.credentials.get).mock.calls[0]?.[0] as CredentialRequestOptions;
    expect(getCall.publicKey).toBeDefined();
    expect(new Uint8Array(getCall.publicKey?.challenge as ArrayBuffer)).toEqual(new Uint8Array(16).map((_, index) => index));
    expect(new Uint8Array(getCall.publicKey?.allowCredentials?.[0].id as ArrayBuffer)).toEqual(new Uint8Array(16).fill(0x10).map((value, index) => value + index));
    expect(sign).toHaveBeenCalledWith({ name: "ECDSA", hash: "SHA-256" }, installation.privateKey, expect.any(Uint8Array));
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/v1/driver/auth/webauthn/finish");
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1] && (fetchMock.mock.calls[1]?.[1] as RequestInit).body))).toEqual(expect.objectContaining({ challenge_id: challengeID, possession_signature: expect.any(String) }));
  });

  it("does not call finish when the browser rejects the WebAuthn ceremony", async () => {
    const installation = createInstallation();
    const error = Object.assign(new Error("browser detail must stay local"), { name: "NotAllowedError" });
    installCredentialAPI(null, error);
    installCryptoAPI();
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({
      challenge_id: challengeID,
      options: { publicKey: { challenge, rpId: "localhost", userVerification: "required" } },
    }));

    await expect(loginDriverWithPasskey("driver@example.test", installation)).rejects.toMatchObject({ name: "NotAllowedError" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(driverLoginErrorMessage(error)).toMatch(/NotAllowedError/);
  });

  it("starts and finishes registration for the approved request returned by the backend", async () => {
    vi.stubGlobal("PublicKeyCredential", FakeRegistrationCredential);
    Object.defineProperty(navigator, "credentials", {
      configurable: true,
      value: { create: vi.fn(async () => new FakeRegistrationCredential()) },
    });
    document.cookie = "tp_csrf=registration-csrf";
    const installation = createInstallation();
    const stages: string[] = [];
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({
      challenge_id: challengeID,
      options: { publicKey: { challenge, rp: { id: "localhost", name: "Transportation Platform" }, user: { id: challenge, name: "driver" }, userVerification: "required" } },
    })).mockResolvedValueOnce(response({ status: "registered" }));

    await registerPasskey("", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", (stage) => stages.push(stage));

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1] && (fetchMock.mock.calls[0]?.[1] as RequestInit).body))).toEqual(expect.objectContaining({ device_request_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" }));
    expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/v1/auth/webauthn/registration/finish");
    expect(stages).toEqual(["options_received", "browser_started", "browser_completed", "finished"]);
  });

  it("normalizes the activation response and rejects a response without an identifier", async () => {
    const installation = createInstallation();
    const values = new Map<string, unknown>();
    let transaction: { oncomplete?: () => void; onerror?: () => void };
    const store = {
      put: (value: { installationId: string }) => { values.set(value.installationId, value); setTimeout(() => transaction.oncomplete?.(), 0); },
      get: (id: string) => ({ result: values.get(id) }),
      delete: (id: string) => { values.delete(id); setTimeout(() => transaction.oncomplete?.(), 0); },
    };
    const database = {
      objectStoreNames: { contains: () => true },
      createObjectStore: vi.fn(),
      close: vi.fn(),
      transaction: () => {
        transaction = { oncomplete: undefined, onerror: undefined };
        return { objectStore: () => store, get oncomplete() { return transaction.oncomplete; }, set oncomplete(value: (() => void) | undefined) { transaction.oncomplete = value; }, get onerror() { return transaction.onerror; }, set onerror(value: (() => void) | undefined) { transaction.onerror = value; } };
      },
    };
    vi.stubGlobal("indexedDB", { open: vi.fn(() => ({ result: database, onsuccess: undefined as (() => void) | undefined, onerror: undefined as (() => void) | undefined, onupgradeneeded: undefined as (() => void) | undefined })) });
    const openRequest = vi.mocked(indexedDB.open);
    openRequest.mockImplementation(() => {
      const request: IDBOpenDBRequest = { result: database as unknown as IDBDatabase } as IDBOpenDBRequest;
      setTimeout(() => request.onsuccess?.(new Event("success") as Event), 0);
      return request;
    });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({
      ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      CorrelationCode: "REQ-DEMO",
      Status: "PENDING",
      ExpiresAt: "2026-11-04T20:01:23Z",
    }));

    await expect(requestDriverActivation("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", undefined, installation, " Driver@Example.Test ")).resolves.toMatchObject({
      id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      correlationCode: "REQ-DEMO",
      status: "PENDING",
      identifier: "driver@example.test",
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);

    fetchMock.mockResolvedValueOnce(response({ Status: "PENDING", ExpiresAt: "2026-11-04T20:01:23Z" }));
    await expect(requestDriverActivation("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", undefined, installation)).rejects.toThrow("request id");
  });

  it("surfaces the stable duplicate activation conflict without accepting a new request", async () => {
    const installation = createInstallation();
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({
      error: { code: "activation_request_exists", message: "Ya existe una solicitud de activación vigente para este dispositivo." },
    }, false, 409));

    await expect(requestDriverActivation("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", undefined, installation))
      .rejects.toMatchObject({ name: "DriverActivationRequestConflictError" });
  });

  it("queries only the current activation request and normalizes its status", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({
      request_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      correlation_code: "REQ-DEMO",
      installation_id: "78f5e1f4-4f86-4e5f-8adf-955cb7d23337",
      status: "APPROVED",
      expires_at: "2026-11-04T20:01:23Z",
      authorization_status: null,
    }));

    await expect(queryDriverActivationStatus("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "78f5e1f4-4f86-4e5f-8adf-955cb7d23337")).resolves.toMatchObject({
      id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
      installationId: "78f5e1f4-4f86-4e5f-8adf-955cb7d23337",
      status: "APPROVED",
      authorizationStatus: null,
    });
    const url = String(fetchMock.mock.calls[0]?.[0]);
    expect(url).toContain("device_request_id=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa");
    expect(url).toContain("installation_id=78f5e1f4-4f86-4e5f-8adf-955cb7d23337");
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ credentials: "include" }));
  });

  it("does not turn a status HTTP failure into approval", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({ error: "expired" }, false));
    await expect(queryDriverActivationStatus("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "78f5e1f4-4f86-4e5f-8adf-955cb7d23337")).rejects.toMatchObject({ status: 401 });
  });

  it("does not call registration finish when Android cancels the ceremony", async () => {
    vi.stubGlobal("PublicKeyCredential", FakeRegistrationCredential);
    Object.defineProperty(navigator, "credentials", {
      configurable: true,
      value: { create: vi.fn(async () => Promise.reject(Object.assign(new Error("cancelled"), { name: "NotAllowedError" }))) },
    });
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(response({
      challenge_id: challengeID,
      options: { publicKey: { challenge, rp: { id: "localhost", name: "Transportation Platform" }, user: { id: challenge, name: "driver" }, userVerification: "required" } },
    }));

    await expect(registerPasskey("", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")).rejects.toMatchObject({ name: "NotAllowedError" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("round-trips an installation and its non-exportable key references through IndexedDB", async () => {
    const values = new Map<string, unknown>();
    let initialized = false;
    const database = {
      objectStoreNames: { contains: (name: string) => name === "installations" },
      createObjectStore: vi.fn(),
      close: vi.fn(),
      transaction: vi.fn(() => {
        const transaction: { oncomplete?: () => void; onerror?: () => void; objectStore: () => typeof store } = {
          objectStore: () => store,
        };
        const store = {
          put: (value: { id: string }) => {
            setTimeout(() => { values.set(value.id, value); transaction.oncomplete?.(); }, 0);
          },
          get: (id: string) => {
            const request: { result?: unknown; onsuccess?: () => void; onerror?: () => void } = {};
            setTimeout(() => { request.result = values.get(id); request.onsuccess?.(); }, 0);
            return request;
          },
          getAll: () => {
            const request: { result?: unknown[]; onsuccess?: () => void; onerror?: () => void } = {};
            setTimeout(() => { request.result = [...values.values()]; request.onsuccess?.(); }, 0);
            return request;
          },
        };
        transaction.objectStore = () => store;
        return transaction;
      }),
    };
    vi.stubGlobal("indexedDB", {
      open: vi.fn(() => {
        const request: { result?: typeof database; onupgradeneeded?: () => void; onsuccess?: () => void } = { result: database };
        setTimeout(() => {
          if (!initialized) { initialized = true; request.onupgradeneeded?.(); }
          request.onsuccess?.();
        }, 0);
        return request;
      }),
    });
    vi.stubGlobal("crypto", {
      randomUUID: vi.fn(() => "78f5e1f4-4f86-4e5f-8adf-955cb7d23337"),
      subtle: {
        generateKey: vi.fn(async () => ({ publicKey: {} as CryptoKey, privateKey: {} as CryptoKey })),
        exportKey: vi.fn(async () => responseBytes(65)),
      },
    });

    const created = await createDriverInstallation();
    const recovered = await loadDriverInstallation(created.id);
    expect(recovered?.id).toBe(created.id);
    expect(recovered?.privateKey).toBe(created.privateKey);
    const associated = await associateDriverInstallationIdentifier(created.id, " Driver@Example.Test ");
    expect(associated.identifier).toBe("driver@example.test");
    expect((await loadDriverInstallation(created.id))?.identifier).toBe("driver@example.test");
    const cleared = await clearDriverInstallationIdentifier(created.id);
    expect(cleared.identifier).toBeUndefined();
    expect([...values.values()][0]).not.toHaveProperty("token");
  });
});
