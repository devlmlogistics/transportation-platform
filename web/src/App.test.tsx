import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "./App";

const jsonResponse = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

const requestID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const authorizationID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

afterEach(() => vi.restoreAllMocks());

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
    fireEvent.click(screen.getByRole("button", { name: "Ingresar supervisor" }));

    expect((await screen.findAllByText("Conductor Demo · driver@example.test")).length).toBe(2);
    expect(screen.getAllByText(/Empresa dddddddd-dddd-4ddd-8ddd-dddddddddddd/)).toHaveLength(2);
    expect(screen.getByText(/Código REQ-DEMO · Instalación eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Aprobar" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith(`/device-enrollment-requests/${requestID}/approve`))).toBe(true));
    expect(screen.getByText("Solicitud aprobada.")).toBeInTheDocument();
  });
});
