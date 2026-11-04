import { afterEach, describe, expect, it, vi } from "vitest";
import {
  approveSupervisorDeviceRequest,
  listSupervisorDeviceAuthorizations,
  listSupervisorDeviceRequests,
  loginSupervisor,
  revokeSupervisorDevice,
} from "./supervisor";

const jsonResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const requestID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const authorizationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

describe("supervisor device demo API", () => {
  afterEach(() => vi.restoreAllMocks());

  it("logs in without storing credentials and sends CSRF on mutations", async () => {
    document.cookie = "tp_csrf=demo-csrf";
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse([{ ID: requestID, LegalName: "Conductor Demo" }]))
      .mockResolvedValueOnce(jsonResponse({ status: "APPROVED" }));

    await loginSupervisor("supervisor@example.test", "secret");
    await listSupervisorDeviceRequests();
    await approveSupervisorDeviceRequest(requestID, "demo");

    expect(fetchMock.mock.calls[0]?.[1]).toEqual(expect.objectContaining({ credentials: "include" }));
    expect((fetchMock.mock.calls[0]?.[1] as RequestInit).body).toBe(JSON.stringify({ identifier: "supervisor@example.test", password: "secret" }));
    const approveHeaders = new Headers((fetchMock.mock.calls[2]?.[1] as RequestInit).headers);
    expect(approveHeaders.get("X-CSRF-Token")).toBe("demo-csrf");
    expect(fetchMock.mock.calls[2]?.[0]).toContain(`${requestID}/approve`);
  });

  it("normalizes the backend PascalCase contract for requests and authorizations", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse([{
        ID: requestID,
        CorrelationCode: "REQ-DEMO",
        UserAccountID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
        Identifier: "driver@example.test",
        LegalName: "Conductor Demo",
        CompanyID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
        CompanyOperationID: null,
        InstallationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
        Status: "PENDING",
        RequestedAt: "2026-01-01T00:00:00Z",
        ExpiresAt: "2026-01-01T01:00:00Z",
      }]))
      .mockResolvedValueOnce(jsonResponse([{
        ID: authorizationID,
        UserAccountID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
        Identifier: "driver@example.test",
        LegalName: "Conductor Demo",
        CompanyID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
        CompanyOperationID: "ffffffff-ffff-4fff-8fff-ffffffffffff",
        InstallationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
        KeyAlgorithm: "ECDSA_P256_SHA256",
        Status: "ACTIVE",
        RequestedAt: "2026-01-01T00:00:00Z",
        ApprovedAt: "2026-01-01T00:05:00Z",
        ExpiresAt: null,
      }]));

    const requests = await listSupervisorDeviceRequests();
    const authorizations = await listSupervisorDeviceAuthorizations();

    expect(requests[0]).toMatchObject({ request_id: requestID, legal_name: "Conductor Demo", correlation_code: "REQ-DEMO" });
    expect(authorizations[0]).toMatchObject({ authorization_id: authorizationID, company_operation_id: "ffffffff-ffff-4fff-8fff-ffffffffffff" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("uses the existing revoke endpoint and does not expose a token in the payload", async () => {
    document.cookie = "tp_csrf=demo-csrf-2";
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ status: "REVOKED" }));

    await revokeSupervisorDevice(authorizationID, "demo revoke");

    const body = String((fetchMock.mock.calls[0]?.[1] as RequestInit).body);
    expect(body).toBe(JSON.stringify({ reason: "demo revoke" }));
    expect(body).not.toContain("token");
    expect(fetchMock.mock.calls[0]?.[0]).toContain(`${authorizationID}/revoke`);
  });

  it("rejects approval and revocation without a valid UUID before fetch", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");

    await expect(approveSupervisorDeviceRequest("", "demo")).rejects.toThrow("Invalid request id");
    await expect(revokeSupervisorDevice("undefined", "demo")).rejects.toThrow("Invalid authorization id");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("propagates HTTP errors instead of treating a failed mutation as success", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ error: { code: "device_request_rejected" } }, 403));

    await expect(approveSupervisorDeviceRequest(requestID, "demo")).rejects.toThrow("403");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
