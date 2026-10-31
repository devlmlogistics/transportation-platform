import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import App from "./App";

describe("platform shell", () => {
  it("shows the Phase 1A foundation without claiming operational functionality", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "Fundación técnica Fase 1A" })).toBeInTheDocument();
    expect(screen.getByText(/No hay autenticación, datos operativos ni sesiones reales/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Drayage" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Transportation" })).toBeInTheDocument();
  });
});
