import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createDriverInstallation,
  driverLoginErrorMessage,
  loadDriverInstallation,
  loginDriverWithPasskey,
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

const response = (body: unknown, ok = true): Response =>
  new Response(JSON.stringify(body), { status: ok ? 200 : 401, headers: { "Content-Type": "application/json" } });

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
    expect([...values.values()][0]).not.toHaveProperty("token");
  });
});
