import { afterEach, describe, expect, it, vi } from "vitest";
import {
  approveSupervisorDeviceRequest,
  listSupervisorDeviceRequests,
  loginSupervisor,
  revokeSupervisorDevice,
} from "./supervisor";

const jsonResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("supervisor device demo API", () => {
  afterEach(() => vi.restoreAllMocks());

  it("logs in without storing credentials and sends CSRF on mutations", async () => {
    document.cookie = "tp_csrf=demo-csrf";
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse([{ request_id: "request-1" }]))
      .mockResolvedValueOnce(jsonResponse({ status: "APPROVED" }));

    await loginSupervisor("supervisor@example.test", "secret");
    await listSupervisorDeviceRequests();
    await approveSupervisorDeviceRequest("request-1", "demo");

    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ credentials: "include" }));
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).body).toBe(JSON.stringify({ identifier: "supervisor@example.test", password: "secret" }));
    const approveHeaders = new Headers((fetchMock.mock.calls[2]?.[1] as RequestInit).headers);
    expect(approveHeaders.get("X-CSRF-Token")).toBe("demo-csrf");
    expect(fetchMock.mock.calls[2]?.[0]).toContain("request-1/approve");
  });

  it("uses the existing revoke endpoint and does not expose a token in the payload", async () => {
    document.cookie = "tp_csrf=demo-csrf-2";
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ status: "REVOKED" }));

    await revokeSupervisorDevice("authorization-1", "demo revoke");

    const body = String((fetchMock.mock.calls[0]?.[1] as RequestInit).body);
    expect(body).toBe(JSON.stringify({ reason: "demo revoke" }));
    expect(body).not.toContain("token");
  });
});
