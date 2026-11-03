import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import App from "./App";

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
});
