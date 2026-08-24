import { expect, test, type Page } from "@playwright/test";

const password = process.env.FRUTO_E2E_PASSWORD ?? "";
const image = process.env.FRUTO_E2E_IMAGE ?? "";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Senha").fill(password);
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/deployments$/);
}

async function waitForOperation(page: Page, status: "Succeeded" | "Failed" | "Superseded" = "Succeeded") {
  await expect(page.getByText(new RegExp(`Operação .*: ${status}`))).toBeVisible({ timeout: 120_000 });
}

test.describe("control plane browser flow", () => {
  test.skip(!password || !image, "FRUTO_E2E_PASSWORD and FRUTO_E2E_IMAGE are required");
  test("logs in, creates, observes Ready, updates, shows history, and deletes", async ({ page }) => {
    await login(page);
    await page.getByRole("link", { name: /Novo deployment/i }).click();
    await page.getByLabel("Nome").fill(`browser-${Date.now()}`);
    await page.getByLabel("Imagem OCI por digest").fill(image);
    await page.getByRole("button", { name: "Criar deployment" }).click();
    await expect(page).toHaveURL(/\/deployments\/ap-/);
    await waitForOperation(page);
    await expect(page.getByText("Pronto")).toBeVisible({ timeout: 120_000 });
    await expect(page.getByText("CreateDeployment")).toBeVisible();

    await page.getByRole("link", { name: "Editar" }).click();
    await page.getByLabel("Réplicas").fill("2");
    await page.getByRole("button", { name: "Atualizar deployment" }).click();
    await waitForOperation(page);
    await expect(page.getByText("UpdateDeployment")).toBeVisible();

    await page.getByRole("button", { name: "Remover" }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.getByRole("button", { name: "Confirmar remoção" }).click();
    await expect(page).toHaveURL(/\/deployments(?:\?|$)/);
    await waitForOperation(page);
    await expect(page.getByText("Nenhum deployment ainda")).toBeVisible({ timeout: 120_000 });
  });
});
