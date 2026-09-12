import AxeBuilder from "@axe-core/playwright";
import { expect, type Page, test } from "@playwright/test";

const password = process.env.MOLEJO_E2E_PASSWORD ?? "";
const exactHostname = process.env.MOLEJO_E2E_EXACT_HOST ?? "";
const poolDomain = process.env.MOLEJO_E2E_POOL_DOMAIN ?? "";
const poolLabel = process.env.MOLEJO_E2E_POOL_LABEL ?? "browser";
const releaseImage = process.env.MOLEJO_E2E_IMAGE ?? "";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Usuário").fill("owner");
  await page.getByLabel("Senha").fill(password);
  await page.getByRole("button", { name: "Entrar" }).click();
  await expect(page).toHaveURL(/\/workspaces\/ws-[a-z2-7]{20}\/overview$/);
}

async function expectNoAccessibilityViolations(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  expect(results.violations).toEqual([]);
}

async function openNewApplication(page: Page, suffix: number) {
  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await page.getByLabel("Novo Project").fill(`Browser ${suffix}`);
  await page.getByRole("button", { name: "Criar Project" }).click();
  await page.getByRole("link", { name: `Abrir Project Browser ${suffix}`, exact: true }).click();
  await page.getByRole("link", { name: "Configurar Environments", exact: true }).click();
  await page.getByLabel("Novo Environment").fill("Production");
  await page.getByRole("button", { name: "Criar Environment" }).click();
  await page.getByRole("link", { name: `Browser ${suffix}`, exact: true }).click();
  await page.getByRole("button", { name: "Adicionar App" }).click();
  await page.getByLabel("Nome do novo App").fill(`Browser app ${suffix}`);
  await page.getByRole("button", { name: "Continuar" }).click();
  await expect(page.getByLabel("Cluster de runtime")).not.toHaveValue("");
}

async function finishApplicationSetup(page: Page, suffix: number) {
  await page.getByRole("button", { name: "Continuar" }).click();
  await expect(page.getByRole("heading", { name: "Revise antes de criar" })).toBeVisible();
  await page.getByRole("button", { name: "Criar App no Environment" }).click();
  await expect(page.getByRole("heading", { name: `Browser app ${suffix}` })).toBeVisible();
}

test.describe("control plane browser flow", () => {
  test("logs in and organizes an App inside a Project", async ({ page }) => {
    test.skip(!password, "MOLEJO_E2E_PASSWORD is required");
    await login(page);
    const suffix = Date.now();
    await openNewApplication(page, suffix);
    await finishApplicationSetup(page, suffix);
    const operationalHealth = page.getByRole("region", { name: "Saúde operacional" });
    await expect(operationalHealth).toBeVisible();
    await expectNoAccessibilityViolations(page);
    await expect(page.getByRole("link", { name: "Entrega", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Observabilidade", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Configuração", exact: true })).toBeVisible();
  });

  test("configures, deploys, and partially removes HTTP addresses", async ({ page }) => {
    test.skip(
      !password || !exactHostname || !poolDomain || !releaseImage,
      "password, exact host, pool domain, and immutable image are required",
    );
    await login(page);
    const suffix = Date.now();
    const pooledHostname = `${poolLabel}.${poolDomain}`;
    await openNewApplication(page, suffix);
    await page.getByText("Ajustar rede, escala e recursos").click();

    const domain = page.getByLabel("Domínio concedido");
    await domain.selectOption({ label: new RegExp(`^${exactHostname.replaceAll(".", "\\.")} ·`) });
    const exactListener = page.getByLabel("Listener de destino");
    if (await exactListener.isVisible()) await exactListener.selectOption({ index: 1 });
    await page.getByRole("button", { name: "Adicionar endereço" }).click();

    await domain.selectOption({ label: new RegExp(`^\\*\\.${poolDomain.replaceAll(".", "\\.")} ·`) });
    await page.getByLabel("Label").fill(poolLabel);
    const poolListener = page.getByLabel("Listener de destino");
    if (await poolListener.isVisible()) await poolListener.selectOption({ index: 1 });
    await page.getByRole("button", { name: "Adicionar endereço" }).click();
    await finishApplicationSetup(page, suffix);

    await expect(page.getByText(exactHostname, { exact: true })).toBeVisible();
    await expect(page.getByText(pooledHostname, { exact: true })).toBeVisible();

    const ids = await page.evaluate(async () => {
      const match = window.location.pathname.match(
        /workspaces\/(ws-[a-z2-7]{20})\/projects\/(prj-[a-z2-7]{20})\/environments\/(env-[a-z2-7]{20})\/apps\/(aev-[a-z2-7]{20})/,
      );
      if (!match) throw new Error("runtime route does not contain Molejo identities");
      const [, workspaceId, projectId, environmentId, appEnvironmentId] = match;
      const response = await fetch(
        `/api/v1/workspaces/${workspaceId}/projects/${projectId}/environments/${environmentId}/apps`,
      );
      if (!response.ok) throw new Error("could not resolve App identity");
      const body = (await response.json()) as { items: Array<{ id: string; appId: string }> };
      const target = body.items.find((item) => item.id === appEnvironmentId);
      if (!target) throw new Error("created App Environment was not listed");
      return { workspaceId, projectId, environmentId, appEnvironmentId, appId: target.appId };
    });

    await page.goto(`/workspaces/${ids.workspaceId}/projects/${ids.projectId}/apps/${ids.appId}/releases`);
    await page.getByLabel("Imagem OCI imutável").fill(releaseImage);
    await page.getByRole("button", { name: "Registrar Release" }).click();
    await expect(page.getByText("Imagem registrada como uma Release do App.")).toBeVisible();

    const runtimeBase = `/workspaces/${ids.workspaceId}/projects/${ids.projectId}/environments/${ids.environmentId}/apps/${ids.appEnvironmentId}`;
    await page.goto(`${runtimeBase}/deployments`);
    await expect(page.getByText("Revisão antes de implantar")).toBeVisible();
    await expect(page.getByText(`${exactHostname}, ${pooledHostname}`, { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Confirmar implantação" }).click();
    await expect(page.getByText("Implantação concluída no cluster.")).toBeVisible({ timeout: 120_000 });
    await page.goto(runtimeBase);
    await expect(page.locator("article", { hasText: exactHostname }).getByText(/Rota: pronta/)).toBeVisible({
      timeout: 120_000,
    });
    await expect(page.locator("article", { hasText: pooledHostname }).getByText(/Rota: pronta/)).toBeVisible();

    await page.goto(`${runtimeBase}/settings/network`);
    await page.locator("article", { hasText: pooledHostname }).getByRole("button", { name: "Remover" }).click();
    await page.getByRole("button", { name: "Salvar estado desejado" }).click();
    await expect(page.getByText(/Configuração salva como estado desejado/)).toBeVisible();
    await page.goto(`${runtimeBase}/deployments`);
    await expect(page.getByText(exactHostname, { exact: true })).toBeVisible();
    await expect(page.getByText(pooledHostname, { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Confirmar implantação" }).click();
    await expect(page.getByText("Implantação concluída no cluster.")).toBeVisible({ timeout: 120_000 });
    await page.goto(runtimeBase);
    await expect(page.locator("article", { hasText: pooledHostname })).toHaveCount(0, { timeout: 120_000 });
    await expect(page.locator("article", { hasText: exactHostname }).getByText(/Rota: pronta/)).toBeVisible();
    await expectNoAccessibilityViolations(page);
  });
});
