import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const jsonResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const requestID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const authorizationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
});

const enrollmentResponse = (id: string, legalName: string) => ({
  ID: id,
  CorrelationCode: `REQ-${id.slice(0, 4)}`,
  UserAccountID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
  Identifier: "driver@example.test",
  LegalName: legalName,
  CompanyID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
  CompanyOperationID: null,
  InstallationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
  Status: "PENDING",
  RequestedAt: "2026-01-01T00:00:00Z",
  ExpiresAt: "2026-01-01T01:00:00Z",
});

describe("platform shell", () => {
  it("shows the driver mobile authentication boundary without claiming operational functionality", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "Activar o ingresar con passkey" })).toBeInTheDocument();
    expect(screen.getByText(/No hay formularios operativos, GPS ni modo offline/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Drayage" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Transportation" })).toBeInTheDocument();
  });

  it("keeps recurring login closed until an installation is prepared", async () => {
    render(<App />);

    screen.getByRole("button", { name: "Ingresar con passkey" }).click();

    expect(await screen.findByRole("status")).toHaveTextContent("Prepara primero la instalación");
  });

  it("uses the synthetic demo catalog instead of asking the driver for UUIDs", () => {
    render(<App />);

    expect(screen.getByRole("combobox", { name: "Empresa" })).toHaveDisplayValue("Manual Acme Logistics");
    expect(screen.getByRole("option", { name: "Drayage" })).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("UUID autorizado")).not.toBeInTheDocument();
    expect(screen.getByText("Dispositivo sin activar")).toBeInTheDocument();
  });

  it("renders the backend PascalCase response and approves the real request UUID", async () => {
    document.cookie = "tp_csrf=demo-csrf";
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        return jsonResponse([{
          ID: requestID,
          CorrelationCode: "REQ-DEMO",
          UserAccountID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
          Identifier: "driver@example.test",
          LegalName: "Conductor Demo",
          CompanyID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
          CompanyOperationID: "ffffffff-ffff-4fff-8fff-ffffffffffff",
          InstallationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
          Status: "PENDING",
          RequestedAt: "2026-01-01T00:00:00Z",
          ExpiresAt: "2026-01-01T01:00:00Z",
        }]);
      }
      if (path.endsWith("/device-authorizations")) {
        return jsonResponse([{
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
        }]);
      }
      if (path.endsWith(`/device-enrollment-requests/${requestID}/approve`)) return jsonResponse({ status: "APPROVED" });
      throw new Error(`Unexpected path: ${path}`);
    });

    render(<App />);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));

    expect((await screen.findAllByText("Conductor Demo")).length).toBe(2);
    expect(screen.getAllByText("driver@example.test")).toHaveLength(2);
    expect(screen.getAllByText(/dddddddd-dddd-4ddd-8ddd-dddddddddddd/)).toHaveLength(2);
    expect(screen.getByText("REQ-DEMO")).toBeInTheDocument();
    expect(screen.getAllByText("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Aprobar instalación" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith(`/device-enrollment-requests/${requestID}/approve`))).toBe(true));
    expect(screen.getByText(/Solicitud aprobada/)).toBeInTheDocument();
  });

  it("keeps revoked devices visible as history and does not offer a revoke action", async () => {
    document.cookie = "tp_csrf=demo-csrf";
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse([]))
      .mockResolvedValueOnce(jsonResponse([{
        ID: authorizationID,
        UserAccountID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
        Identifier: "driver@example.test",
        LegalName: "Conductor Revocado",
        CompanyID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
        CompanyOperationID: null,
        InstallationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
        KeyAlgorithm: "ECDSA_P256_SHA256",
        Status: "REVOKED",
        RequestedAt: "2026-01-01T00:00:00Z",
        ApprovedAt: "2026-01-01T00:05:00Z",
        ExpiresAt: null,
        RevokedAt: "2026-01-01T00:10:00Z",
      }]));

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));

    expect(await screen.findByText("Conductor Revocado")).toBeInTheDocument();
    expect(screen.getByText("Historial revocado")).toBeInTheDocument();
    expect(screen.getAllByText("Revocado")).toHaveLength(2);
    expect(screen.queryByRole("button", { name: "Revocar dispositivo" })).not.toBeInTheDocument();
  });

  it("polls every five seconds, updates counters, and announces each new request once", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const firstRequestID = "11111111-1111-4111-8111-111111111111";
    const secondRequestID = "22222222-2222-4222-8222-222222222222";
    let pendingRequests = [enrollmentResponse(firstRequestID, "Primer conductor")];
    let requestCalls = 0;
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        return jsonResponse(pendingRequests);
      }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      throw new Error(`Unexpected path: ${path}`);
    });

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Actualizar ahora" })).toBeInTheDocument());
    expect(requestCalls).toBe(1);

    pendingRequests = [
      enrollmentResponse(firstRequestID, "Primer conductor"),
      enrollmentResponse(secondRequestID, "Segundo conductor"),
    ];
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });

    expect(requestCalls).toBe(2);
    expect(screen.getByText("Segundo conductor")).toBeInTheDocument();
    expect(screen.getByText("Nueva solicitud de activación pendiente.")).toBeInTheDocument();
    expect(screen.getByText("Pendientes").parentElement).toHaveTextContent("2");

    document.dispatchEvent(new Event("visibilitychange"));
    await act(async () => {
      await Promise.resolve();
    });
    expect(requestCalls).toBe(3);
    expect(screen.getAllByText("Nueva solicitud de activación pendiente.")).toHaveLength(1);
    expect(fetchMock).toHaveBeenCalled();
  });

  it("does not poll while hidden and refreshes immediately when visible again", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let requestCalls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        return jsonResponse([]);
      }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      throw new Error(`Unexpected path: ${path}`);
    });

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await waitFor(() => expect(requestCalls).toBe(1));

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(requestCalls).toBe(1);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => expect(requestCalls).toBe(2));
  });

  it("stops polling after an expired session", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let requestCalls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        return jsonResponse({ error: "session_expired" }, 401);
      }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      throw new Error(`Unexpected path: ${path}`);
    });

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await waitFor(() => expect(screen.getByText("Tu sesión ha expirado. Vuelve a ingresar.")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Actualizar ahora" })).not.toBeInTheDocument();
    const callsAfterExpiry = requestCalls;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(requestCalls).toBe(callsAfterExpiry);
  });

  it("stops automatic refresh after a permission failure", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let requestCalls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        return jsonResponse({ error: "forbidden" }, 403);
      }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      throw new Error(`Unexpected path: ${path}`);
    });

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await waitFor(() => expect(screen.getByText(/No tienes permisos para consultar esta consola/)).toBeInTheDocument());
    const callsAfterForbidden = requestCalls;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(requestCalls).toBe(callsAfterForbidden);
    expect(screen.queryByRole("button", { name: "Actualizar ahora" })).not.toBeInTheDocument();
  });

  it("does not start a second refresh while the first one is pending and cleans up on unmount", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let requestCalls = 0;
    let releasePending: (() => void) | undefined;
    const { unmount } = render(<App />);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        if (requestCalls === 2) return new Promise<Response>((resolve) => { releasePending = () => resolve(jsonResponse([])); });
        return jsonResponse([]);
      }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      throw new Error(`Unexpected path: ${path}`);
    });

    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await waitFor(() => expect(requestCalls).toBe(1));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(requestCalls).toBe(2);
    releasePending?.();
    await act(async () => {
      await Promise.resolve();
    });
    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(requestCalls).toBe(2);
  });
});
