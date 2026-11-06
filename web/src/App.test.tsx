import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const jsonResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const requestID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const authorizationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const driverInstallationID = "78f5e1f4-4f86-4e5f-8adf-955cb7d23337";
const driverRequestID = "99999999-9999-4999-8999-999999999999";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
});

const installDriverActivationDB = (status = "PENDING", expiresAt = "2099-01-01T00:00:00Z", identifier?: string) => {
  const values = new Map<string, unknown>([[driverInstallationID, {
    id: driverInstallationID,
    publicKey: {} as CryptoKey,
    privateKey: {} as CryptoKey,
    publicKeyEncoded: "public-key",
    ...(identifier ? { identifier } : {}),
    createdAt: Date.now(),
  }]]);
  const activationRequest = {
    id: driverRequestID,
    correlationCode: "REQ-DRIVER",
    status,
    expiresAt,
    installationId: driverInstallationID,
  };
  values.set(driverInstallationID, { ...(values.get(driverInstallationID) as object), activationRequest });
  const objectStore = (name: string, transaction: { oncomplete?: () => void; onerror?: () => void }) => ({
    get: (id: string) => {
      const request: { result?: unknown; onsuccess?: () => void } = {};
      setTimeout(() => {
        const value = values.get(id) as { activationRequest?: unknown } | undefined;
        request.result = name === "installations" ? value : value?.activationRequest;
        request.onsuccess?.();
      }, 0);
      return request;
    },
    getAll: () => {
      const request: { result?: unknown[]; onsuccess?: () => void } = {};
      setTimeout(() => {
        request.result = [...values.values()].map((value) => {
          const item = value as { id?: string; publicKey?: CryptoKey; privateKey?: CryptoKey; publicKeyEncoded?: string; createdAt?: number };
          return item;
        });
        request.onsuccess?.();
      }, 0);
      return request;
    },
    put: (value: { installationId: string; status?: string }) => {
      const existing = values.get(value.installationId) as { activationRequest?: unknown } | undefined;
      values.set(value.installationId, { ...existing, activationRequest: value });
      setTimeout(() => transaction.oncomplete?.(), 0);
    },
    delete: (id: string) => {
      values.delete(id);
      setTimeout(() => transaction.oncomplete?.(), 0);
    },
  });
  const database = {
    objectStoreNames: { contains: () => true },
    createObjectStore: vi.fn(),
    close: vi.fn(),
    transaction: (name: string) => {
      const transaction: { oncomplete?: () => void; onerror?: () => void; objectStore?: () => ReturnType<typeof objectStore> } = {};
      transaction.objectStore = () => objectStore(name, transaction);
      return transaction;
    },
  };
  vi.stubGlobal("indexedDB", {
    open: vi.fn(() => {
      const request: { result?: typeof database; onsuccess?: () => void; onupgradeneeded?: () => void } = { result: database };
      setTimeout(() => {
        request.onupgradeneeded?.();
        request.onsuccess?.();
      }, 0);
      return request;
    }),
  });
};

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
  ExpiresAt: "2099-01-01T01:00:00Z",
});

describe("platform shell", () => {
  it.each(["PENDING", "APPROVED"])("recovers expired %s without enabling passkey registration", async (status) => {
    installDriverActivationDB(status, "2020-01-01T00:00:00Z");
    render(<App />);
    expect(await screen.findByText("La aprobación venció. Solicita una nueva activación")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Registrar passkey" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Solicitar nueva activación" })).toBeInTheDocument();
  });

  it("recovers a valid approval and enables passkey registration", async () => {
    installDriverActivationDB("APPROVED");
    render(<App />);
    expect(await screen.findByText("Tu dispositivo fue aprobado")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Registrar passkey" })).toBeEnabled();
  });
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

  it("recognizes an associated installation and makes passkey the primary recurring access", async () => {
    installDriverActivationDB("APPROVED", "2099-01-01T00:00:00Z", "driver@example.test");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "Bienvenido de nuevo" })).toBeInTheDocument();
    expect(screen.getByText(/Esta instalación ya está asociada a tu identidad/)).toBeInTheDocument();
    expect(screen.getByText("(sólo fallback)")).toBeInTheDocument();
    const passkey = screen.getByRole("button", { name: "Ingresar con passkey" });
    const fallback = screen.getByRole("button", { name: "Solicitar nueva activación" });
    expect(passkey).toBeInTheDocument();
    expect(fallback).toBeInTheDocument();
    expect(passkey.compareDocumentPosition(fallback) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
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
          ExpiresAt: "2099-01-01T01:00:00Z",
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

  it("keeps approved requests visible until the driver registers a passkey", async () => {
    document.cookie = "tp_csrf=demo-csrf";
    const approvedID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse([
        enrollmentResponse(requestID, "Conductor pendiente"),
        { ...enrollmentResponse(approvedID, "Conductor aprobado"), Status: "APPROVED", ApprovedAt: "2026-01-01T00:05:00Z" },
      ]))
      .mockResolvedValueOnce(jsonResponse([]));

    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));

    expect(await screen.findByText("Aprobadas — esperando passkey")).toBeInTheDocument();
    expect(screen.getByText("Conductor aprobado")).toBeInTheDocument();
    expect(screen.getByText("Aprobado — esperando passkey")).toBeInTheDocument();
    expect(screen.getByText("Pendientes").parentElement).toHaveTextContent("1");
    expect(screen.getAllByRole("button", { name: "Aprobar instalación" })).toHaveLength(1);
    expect(screen.getByText("Aprobada")).toBeInTheDocument();
  });

  it("does not offer approval for an expired pending request", async () => {
    vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({}))
      .mockResolvedValueOnce(jsonResponse([{ ...enrollmentResponse(requestID, "Conductor vencido"), ExpiresAt: "2020-01-01T00:00:00Z" }]))
      .mockResolvedValueOnce(jsonResponse([]));
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    expect(await screen.findByText("Conductor vencido")).toBeInTheDocument();
    expect(screen.getByText("Pendientes").parentElement).toHaveTextContent("0");
    expect(screen.queryByRole("button", { name: "Aprobar instalación" })).not.toBeInTheDocument();
    expect(screen.getByText("Solicitudes cerradas")).toBeInTheDocument();
    expect(screen.getByText("Expirada")).toBeInTheDocument();
  });

  it("refreshes after approval even when an older poll is still in flight", async () => {
    let calls = 0;
    let release: ((response: Response) => void) | undefined;
    let approved = false;
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith(`/device-enrollment-requests/${requestID}/approve`)) { approved = true; return jsonResponse({}); }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      if (path.endsWith("/device-enrollment-requests")) {
        calls++;
        if (calls === 2) return new Promise<Response>((resolve) => { release = resolve; });
        return jsonResponse([{ ...enrollmentResponse(requestID, "Conductor"), Status: approved ? "APPROVED" : "PENDING" }]);
      }
      throw new Error("Unexpected endpoint");
    });
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await screen.findByRole("button", { name: "Aprobar instalación" });
    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => expect(calls).toBe(2));
    fireEvent.click(screen.getByRole("button", { name: "Aprobar instalación" }));
    await waitFor(() => expect(approved).toBe(true));
    expect(await screen.findByText("Aprobado — esperando passkey")).toBeInTheDocument();
    await act(async () => { release?.(jsonResponse([enrollmentResponse(requestID, "Conductor")])); });
    expect(screen.getByText("Aprobado — esperando passkey")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Aprobar instalación" })).not.toBeInTheDocument();
    expect(calls).toBe(3);
  });

  it("cancels an in-flight poll when revoking and refreshes the result", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let requestCalls = 0;
    let authorizationCalls = 0;
    let aborted = false;
    const activeAuthorization = {
      ID: authorizationID,
      UserAccountID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
      Identifier: "driver@example.test",
      LegalName: "Conductor Activo",
      CompanyID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
      CompanyOperationID: null,
      InstallationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
      KeyAlgorithm: "ECDSA_P256_SHA256",
      Status: "ACTIVE",
      RequestedAt: "2099-01-01T00:00:00Z",
      ApprovedAt: "2099-01-01T00:05:00Z",
      ExpiresAt: null,
    };
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith(`/device-authorizations/${authorizationID}/revoke`)) return jsonResponse({ status: "REVOKED" });
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        if (requestCalls === 2) return new Promise<Response>((_, reject) => {
          init?.signal?.addEventListener("abort", () => { aborted = true; reject(new DOMException("aborted", "AbortError")); }, { once: true });
        });
        return jsonResponse([]);
      }
      if (path.endsWith("/device-authorizations")) {
        authorizationCalls += 1;
        return jsonResponse(authorizationCalls === 1 ? [activeAuthorization] : []);
      }
      throw new Error(`Unexpected path: ${path}`);
    });
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await screen.findByRole("button", { name: "Revocar dispositivo" });
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    await waitFor(() => expect(requestCalls).toBe(2));
    fireEvent.click(screen.getByRole("button", { name: "Revocar dispositivo" }));
    await waitFor(() => expect(aborted).toBe(true));
    await waitFor(() => expect(requestCalls).toBe(3));
    expect(screen.queryByRole("button", { name: "Revocar dispositivo" })).not.toBeInTheDocument();
  });

  it("aborts polling on unmount without surfacing a cancellation error", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let requestCalls = 0;
    let aborted = false;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        if (requestCalls === 2) return new Promise<Response>((_, reject) => init?.signal?.addEventListener("abort", () => { aborted = true; reject(new DOMException("aborted", "AbortError")); }, { once: true }));
        return jsonResponse([]);
      }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      throw new Error(`Unexpected path: ${path}`);
    });
    const view = render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await waitFor(() => expect(requestCalls).toBe(1));
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    await waitFor(() => expect(requestCalls).toBe(2));
    view.unmount();
    await waitFor(() => expect(aborted).toBe(true));
    expect(screen.queryByText("No fue posible actualizar los estados del supervisor.")).not.toBeInTheDocument();
  });

  it("does not overlap manual refresh with an active poll", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let requestCalls = 0;
    let release: (() => void) | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.endsWith("/auth/login")) return jsonResponse({});
      if (path.endsWith("/device-enrollment-requests")) {
        requestCalls += 1;
        if (requestCalls === 2) return new Promise<Response>((resolve) => { release = () => resolve(jsonResponse([])); });
        return jsonResponse([]);
      }
      if (path.endsWith("/device-authorizations")) return jsonResponse([]);
      throw new Error(`Unexpected path: ${path}`);
    });
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    await waitFor(() => expect(requestCalls).toBe(1));
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    await waitFor(() => expect(requestCalls).toBe(2));
    expect(screen.getByRole("button", { name: "Actualizando…" })).toBeDisabled();
    expect(requestCalls).toBe(2);
    release?.();
    await act(async () => { await Promise.resolve(); });
  });

  it("keeps independent counters for pending, approved, closed, active and revoked records", async () => {
    const expired = { ...enrollmentResponse("11111111-1111-4111-8111-111111111111", "Expirado"), ExpiresAt: "2020-01-01T00:00:00Z" };
    const rejected = { ...enrollmentResponse("22222222-2222-4222-8222-222222222222", "Rechazado"), Status: "REJECTED" };
    const approved = { ...enrollmentResponse("33333333-3333-4333-8333-333333333333", "Aprobado"), Status: "APPROVED", ApprovedAt: "2099-01-01T00:05:00Z" };
    const active = { ID: authorizationID, UserAccountID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Identifier: "driver@example.test", LegalName: "Activo", CompanyID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", CompanyOperationID: null, InstallationID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", KeyAlgorithm: "ECDSA_P256_SHA256", Status: "ACTIVE", RequestedAt: "2099-01-01T00:00:00Z", ApprovedAt: "2099-01-01T00:05:00Z", ExpiresAt: null };
    const revoked = { ...active, ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Status: "REVOKED", LegalName: "Revocado" };
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(jsonResponse({})).mockResolvedValueOnce(jsonResponse([enrollmentResponse(requestID, "Pendiente"), approved, expired, rejected])).mockResolvedValueOnce(jsonResponse([active, revoked]));
    render(<App />);
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));
    expect(await screen.findByText("Aprobadas — esperando passkey")).toBeInTheDocument();
    expect(screen.getByText("Pendientes").parentElement).toHaveTextContent("1");
    expect(screen.getByText("Aprobadas esperando passkey").parentElement).toHaveTextContent("1");
    expect(screen.getByText("Registrados activos").parentElement).toHaveTextContent("1");
    expect(screen.getByText("Historial revocado").parentElement).toHaveTextContent("1");
    expect(screen.getAllByText("Expirada").length).toBeGreaterThan(0);
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

  it("polls a pending driver activation, enables registration after approval, and then stops", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    installDriverActivationDB();
    let statusCalls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.includes("/driver/auth/activation/status")) {
        statusCalls += 1;
        return jsonResponse({
          request_id: driverRequestID,
          correlation_code: "REQ-DRIVER",
          installation_id: driverInstallationID,
          status: statusCalls === 1 ? "PENDING" : "APPROVED",
          expires_at: "2099-01-01T00:00:00Z",
          authorization_status: null,
        });
      }
      throw new Error(`Unexpected path: ${path}`);
    });

    const view = render(<App />);
    await waitFor(() => expect(screen.getByText("Esperando aprobación del supervisor")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Registrar passkey" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Consultar estado" })).toBeInTheDocument();
    await waitFor(() => expect(statusCalls).toBe(1));

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    await waitFor(() => expect(screen.getByText("Tu dispositivo fue aprobado")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Registrar passkey" })).toBeEnabled();
    const callsAfterApproval = statusCalls;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(statusCalls).toBe(callsAfterApproval);
    view.unmount();
    render(<App />);
    await waitFor(() => expect(screen.getByText("Tu dispositivo fue aprobado")).toBeInTheDocument());
    expect(statusCalls).toBe(callsAfterApproval);
  });

  it("pauses driver activation polling while hidden and refreshes on visibility or manual request", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    installDriverActivationDB();
    let statusCalls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input);
      if (path.includes("/driver/auth/activation/status")) {
        statusCalls += 1;
        return jsonResponse({ request_id: driverRequestID, correlation_code: "REQ-DRIVER", installation_id: driverInstallationID, status: "PENDING", expires_at: "2099-01-01T00:00:00Z", authorization_status: null });
      }
      throw new Error(`Unexpected path: ${path}`);
    });

    render(<App />);
    await waitFor(() => expect(statusCalls).toBe(1));
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(statusCalls).toBe(1);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => expect(statusCalls).toBe(2));
    fireEvent.click(screen.getByRole("button", { name: "Consultar estado" }));
    await waitFor(() => expect(statusCalls).toBe(3));
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
