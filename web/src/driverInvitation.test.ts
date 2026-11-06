import { afterEach, describe, expect, it, vi } from "vitest";
import { consumeDriverInvitation, readInvitationTokenFromLocation, validateDriverInvitation } from "./driverInvitation";

const installation = {
  id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  publicKey: {} as CryptoKey,
  privateKey: {} as CryptoKey,
  publicKeyEncoded: "public-key",
};

afterEach(() => {
  vi.restoreAllMocks();
  window.history.replaceState({}, document.title, "/");
});

describe("driver invitation client boundary", () => {
  it("reads a fragment token and removes it from the address bar", () => {
    window.history.replaceState({}, document.title, "/#driver-invite=opaque-token");
    expect(readInvitationTokenFromLocation()).toBe("opaque-token");
    expect(window.location.hash).toBe("");
  });

  it("validates metadata without persisting the bearer token", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ company_name: "Acme" }), { status: 200 }));
    await validateDriverInvitation("opaque-token");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/driver/invitations/validate", expect.objectContaining({ body: JSON.stringify({ token: "opaque-token" }) }));
    expect(localStorage.length).toBe(0);
  });

  it("consumes an invitation using the current installation binding", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({
      request_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
      correlation_code: "REQ-1",
      status: "PENDING",
      expires_at: "2099-01-01T00:00:00Z",
      installation_id: installation.id,
    }), { status: 201 }));
    const request = await consumeDriverInvitation("opaque-token", "12345678", installation);
    expect(request.id).toBe("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/driver/invitations/consume", expect.objectContaining({
      body: expect.stringContaining('"installation_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"'),
    }));
  });
});
