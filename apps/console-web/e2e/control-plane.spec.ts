import { expect, type Page, test } from "@playwright/test";

const password = process.env.MOLEJO_E2E_PASSWORD ?? "";
async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Usuário").fill("owner");
  await page.getByLabel("Senha").fill(password);
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/workspaces\/ws-[a-z2-7]{20}\/overview$/);
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const response = await fetch("/api/v1/session");
        if (!response.ok) return false;
        const session = (await response.json()) as { workspaceMemberships?: { workspaceId: string; role: string }[] };
        const workspaceId = window.location.pathname.split("/")[2];
        return (
          session.workspaceMemberships?.some(
            (membership) => membership.workspaceId === workspaceId && membership.role === "Owner",
          ) === true
        );
      }),
    )
    .toBe(true);
}

test.describe("control plane browser flow", () => {
  test("logs in and organizes an App inside a Project", async ({ page }) => {
    test.skip(!password, "MOLEJO_E2E_PASSWORD is required");
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
    const operationalHealth = page.getByRole("region", { name: "Saúde operacional" });
    await expect(operationalHealth).toBeVisible();
    await expect(page.getByRole("link", { name: "Entrega", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Observabilidade", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Configuração", exact: true })).toBeVisible();
    await expect(operationalHealth.getByText("develop", { exact: true })).toBeVisible();
  });
});
