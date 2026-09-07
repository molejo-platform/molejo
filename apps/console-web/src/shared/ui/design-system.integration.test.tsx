import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Alert } from "./Alert";
import { BrandLogo } from "./BrandLogo";
import { Button } from "./Button";
import { Field } from "./Field";

describe("shared design-system primitives", () => {
  it("keeps button variants inside the component contract", () => {
    render(<Button variant="secondary">Cancelar</Button>);
    const button = screen.getByRole("button", { name: "Cancelar" });
    expect(button.className).toContain("button");
    expect(button.getAttribute("data-variant")).toBe("secondary");
  });

  it("renders a checkbox with its label, description, and native semantics", () => {
    render(<Field type="checkbox" label="Entrega automática" helper="Executa após um push." />);
    const checkbox = screen.getByRole("checkbox", { name: "Entrega automática" });
    expect(checkbox.getAttribute("aria-describedby")).not.toBeNull();
    expect(screen.getByText("Executa após um push.").id).toBe(checkbox.getAttribute("aria-describedby"));
  });

  it("announces outcomes without turning persistent guidance into a live region", () => {
    render(
      <>
        <Alert tone="info">Orientação persistente</Alert>
        <Alert tone="warning" live>
          Atualização degradada
        </Alert>
        <Alert tone="success">Alteração concluída</Alert>
        <Alert>Falha ao salvar</Alert>
      </>,
    );
    expect(screen.getByText("Orientação persistente").getAttribute("role")).toBeNull();
    expect(screen.getAllByRole("status").map((element) => element.textContent)).toEqual([
      "Atualização degradada",
      "Alteração concluída",
    ]);
    expect(screen.getByRole("alert").textContent).toContain("Falha ao salvar");
  });

  it("uses an official brand asset with a stable accessible name", () => {
    render(<BrandLogo surface="light" />);
    expect(screen.getByRole("img", { name: "Molejo" }).getAttribute("src")).toBe(
      "/brand/molejo-horizontal-on-light.webp",
    );
  });
});
