import { expect, test, type Page } from "@playwright/test";

const password = process.env.FRUTO_E2E_PASSWORD ?? "";
async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Senha").fill(password);
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/workspaces\/ws-[a-z2-7]{20}\/overview$/);
}

test.describe("control plane browser flow", () => {
  test("logs in and organizes an App inside a Project", async ({ page }) => {
    test.skip(!password, "FRUTO_E2E_PASSWORD is required");
    await login(page);
    const suffix = Date.now();
    await page.getByRole("link", { name: "Projects", exact: true }).click();
    await page.getByLabel("Novo Project").fill(`Browser ${suffix}`);
    await page.getByRole("button", { name: "Criar Project" }).click();
    await page.getByRole("link", { name: `Abrir Project Browser ${suffix}`, exact: true }).click();
    await page.getByRole("link", { name: "Configurar Environments", exact: true }).click();
    await page.getByLabel("Novo Environment").fill("Production");
    await page.getByRole("button", { name: "Criar Environment" }).click();
    await expect(page.getByText("Production", { exact: true })).toBeVisible();
    await page.getByRole("link", { name: `Browser ${suffix}`, exact: true }).click();
    await page.getByRole("button", { name: "Adicionar App" }).click();
    await page.getByLabel("Nome do novo App").fill("Browser app");
    await page.getByLabel("Branch").fill("develop");
    await page.getByRole("button", { name: "Criar e adicionar" }).click();
    await expect(page.getByRole("heading", { name: "Browser app" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Builds", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Deployments", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Configuração", exact: true })).toBeVisible();
    await expect(page.getByText("develop", { exact: true })).toBeVisible();
  });
});
