import { expect, test, type Page } from "@playwright/test";

const password = process.env.FRUTO_E2E_PASSWORD ?? "";
const image = process.env.FRUTO_E2E_IMAGE ?? "";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Senha").fill(password);
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/workspaces\/ws-[a-z2-7]{20}\/overview$/);
}

async function waitForOperation(page: Page, status: "Succeeded" | "Failed" | "Superseded" = "Succeeded") {
  await expect(page.getByText(new RegExp(`Operação .*: ${status}`))).toBeVisible({ timeout: 120_000 });
}

test.describe("control plane browser flow", () => {
  test("logs in, creates, observes Ready, updates, shows history, and deletes", async ({ page }) => {
    test.skip(!password || !image, "FRUTO_E2E_PASSWORD and FRUTO_E2E_IMAGE are required");
    await login(page);
    const suffix = Date.now();
    await page.getByRole("link", { name: "Projects", exact: true }).click();
    await page.getByLabel("Novo Project").fill(`Browser ${suffix}`);
    await page.getByRole("button", { name: "Criar Project" }).click();
    await page.getByRole("link", { name: `Abrir Project Browser ${suffix}`, exact: true }).click();
    await page.getByRole("link", { name: "Environments", exact: true }).click();
    await page.getByLabel("Novo Environment").fill("Production");
    await page.getByRole("button", { name: "Criar Environment" }).click();
    await expect(page.getByText("Production", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Apps", exact: true }).click();
    await page.getByLabel("Novo App").fill("Browser app");
    await page.getByRole("button", { name: "Criar App" }).click();
    await expect(page.getByRole("link", { name: "Browser app", exact: true })).toBeVisible();
    await page.getByRole("link", { name: "Deployments", exact: true }).click();
    await page.getByRole("link", { name: /Novo deployment/i }).click();
    await page.getByLabel("App").selectOption({ label: "Browser app" });
    await page.getByLabel("Environment").selectOption({ label: "Production" });
    await page.getByLabel("Nome").fill(`browser-${suffix}`);
    await page.getByLabel("Imagem OCI por digest").fill(image);
    await page.getByRole("button", { name: "Continuar" }).click();
    await page.getByRole("button", { name: "Continuar" }).click();
    await page.getByRole("button", { name: "Criar deployment" }).click();
    await expect(page).toHaveURL(/\/deployments\/ap-/);
    await waitForOperation(page);
    await expect(page.getByRole("heading", { name: "Pronto" })).toBeVisible({ timeout: 120_000 });
    await expect(page.getByText("CreateDeployment")).toBeVisible();

    await page.getByRole("link", { name: "Editar" }).click();
    await page.getByLabel("Réplicas").fill("2");
    await page.getByRole("button", { name: "Salvar e reconciliar" }).click();
    await waitForOperation(page);
    await expect(page.getByText("UpdateDeployment")).toBeVisible();

    await page.getByRole("button", { name: "Remover" }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.getByRole("button", { name: "Remover deployment" }).click();
    await expect(page).toHaveURL(/\/deployments(?:\?|$)/);
    await waitForOperation(page);
    await expect(page.getByRole("heading", { name: "Deployments" })).toBeVisible({ timeout: 120_000 });
  });
});
