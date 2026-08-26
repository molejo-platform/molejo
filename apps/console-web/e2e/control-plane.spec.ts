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
  test("logs in, creates, observes Ready, updates, shows history, and deletes", async ({ page }) => {
    test.skip(!password || !image, "FRUTO_E2E_PASSWORD and FRUTO_E2E_IMAGE are required");
    await login(page);
    const suffix = Date.now();
    await page.getByRole("link", { name: "Administração" }).click();
    await page.getByLabel("Novo Project").fill(`Browser ${suffix}`);
    await page.getByRole("button", { name: "Criar Project" }).click();
    await expect(page.getByText(`Browser ${suffix}`, { exact: true })).toBeVisible();
    const environmentForm = page.locator("form").filter({ has: page.getByLabel("Novo Environment") });
    await environmentForm.getByLabel("Novo Environment").fill("Production");
    await environmentForm.getByRole("button", { name: "Criar", exact: true }).click();
    await expect(page.getByText("Production", { exact: true })).toBeVisible();
    const appForm = page.locator("form").filter({ has: page.getByLabel("Novo App") });
    await appForm.getByLabel("Novo App").fill("Browser app");
    await appForm.getByRole("button", { name: "Criar", exact: true }).click();
    await expect(page.getByText("Browser app", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Deployments" }).click();
    await page.getByRole("link", { name: /Novo deployment/i }).click();
    await page.getByLabel("App").selectOption({ label: "Browser app" });
    await page.getByLabel("Environment").selectOption({ label: "Production" });
    await page.getByLabel("Nome").fill(`browser-${suffix}`);
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
