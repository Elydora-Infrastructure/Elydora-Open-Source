import { expect, test, type Page, type Route } from '@playwright/test';

const API_BASE_URL = 'http://localhost:8787';
const ORG_ID = 'org-e2e';
const TOKEN = 'ely_test_registration_token';

interface ObservedRequest {
  readonly method: string;
  readonly path: string;
  readonly body: unknown;
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
}

async function waitForRenderedDialog(page: Page) {
  await page.getByRole('dialog').evaluate(async (dialog) => {
    await Promise.all(dialog.getAnimations().map((animation) => animation.finished));
  });
}

async function mockBackend(
  page: Page,
  observed: ObservedRequest[],
  unexpected: string[],
  registrationIntegrationOverride?: string,
) {
  await page.route(`${API_BASE_URL}/**`, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    const body = request.postDataJSON() as unknown;
    observed.push({ method, path, body });

    if (method === 'OPTIONS') {
      await route.fulfill({ status: 204 });
      return;
    }

    if (method === 'GET' && path === '/api/auth/get-session') {
      await json(route, {
        user: {
          id: 'user-e2e',
          name: 'E2E Owner',
          email: 'owner@example.com',
          emailVerified: true,
          createdAt: new Date(0).toISOString(),
          updatedAt: new Date(0).toISOString(),
          onboarding_completed: true,
          org_id: ORG_ID,
          role: 'org_owner',
        },
        session: {
          id: 'session-e2e',
          userId: 'user-e2e',
          token: 'session-token',
          activeOrganizationId: ORG_ID,
          expiresAt: new Date(Date.now() + 86_400_000).toISOString(),
          createdAt: new Date(0).toISOString(),
          updatedAt: new Date(0).toISOString(),
        },
      });
      return;
    }

    if (method === 'GET' && path === '/v1/agents') {
      await json(route, { agents: [] });
      return;
    }

    if (method === 'POST' && path === '/v1/agents/register') {
      const registration = body as {
        agent_id: string;
        display_name: string;
        responsible_entity?: string;
        integration_type: string;
        keys: Array<{ kid: string; public_key: string; algorithm: string }>;
      };
      await json(route, {
        agent: {
          agent_id: registration.agent_id,
          org_id: ORG_ID,
          display_name: registration.display_name,
          responsible_entity: registration.responsible_entity ?? '',
          integration_type: registrationIntegrationOverride ?? registration.integration_type,
          status: 'active',
          created_at: Date.now(),
          updated_at: Date.now(),
        },
        keys: registration.keys.map((key) => ({
          ...key,
          agent_id: registration.agent_id,
          status: 'active',
          created_at: Date.now(),
          retired_at: null,
        })),
      });
      return;
    }

    if (method === 'POST' && path === '/v1/auth/token') {
      await json(route, { token: TOKEN, expires_at: Date.now() + 86_400_000 });
      return;
    }

    unexpected.push(`${method} ${path}`);
    await json(route, { error: { code: 'UNEXPECTED_E2E_REQUEST' } }, 501);
  });
}

test('registers Grok atomically and renders verified adapter commands', async ({ page }) => {
  const observed: ObservedRequest[] = [];
  const unexpected: string[] = [];
  const runtimeErrors: string[] = [];
  page.on('pageerror', (error) => runtimeErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') runtimeErrors.push(message.text());
  });
  await mockBackend(page, observed, unexpected);

  await page.goto('/agents');
  await page.getByRole('button', { name: 'Register Agent' }).click();
  await waitForRenderedDialog(page);
  await expect(page.getByRole('button', { name: 'Augment Code' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'OpenAI Codex' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Kimi CLI' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Qwen Code' })).toBeVisible();
  await page.screenshot({ path: 'test-results/agent-registration-catalog.png', fullPage: true });
  await page.getByRole('button', { name: 'Grok Build' }).click();
  await page.getByLabel('Display Name').fill('Production Grok');
  await page.getByLabel('Responsible Entity (optional)').fill('Platform Security');
  await page.locator('form').getByRole('button', { name: 'Register Agent' }).click();

  await expect(page.getByText('Private Key — Save This Now')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Close' })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect(page.getByText('Private Key — Save This Now')).toBeVisible();
  await page.screenshot({ path: 'test-results/agent-registration-credentials.png', fullPage: true });
  await page.getByLabel('Token Expiration').selectOption('custom');
  await page.getByLabel('days').fill('366');
  await page.getByRole('button', { name: 'Issue API Token' }).click();
  await expect(page.getByText('Enter a whole number from 1 to 365.')).toBeVisible();
  expect(observed.filter(({ path }) => path === '/v1/auth/token')).toHaveLength(0);
  await page.getByLabel('Token Expiration').selectOption('24hours');
  await page.getByRole('button', { name: 'Issue API Token' }).click();
  await expect(page.getByText(TOKEN)).toBeVisible();
  const privateKey = await page.locator('section[aria-labelledby="private-key-label"] code').innerText();
  await page.getByRole('button', { name: 'Continue' }).click();

  const setup = page.locator('pre');
  await expect(setup).toContainText('npx @elydora/sdk install --agent grok');
  await expect(setup).toContainText(`--org_id '${ORG_ID}'`);
  await expect(setup).not.toContainText(TOKEN);
  await expect(setup).not.toContainText(privateKey);
  await expect(setup).not.toContainText('--token');
  await expect(setup).not.toContainText('--private_key');
  await expect(page.getByText('The CLI requests both values with terminal echo disabled.')).toBeVisible();

  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.getByRole('button', { name: 'Copy setup command' }).click();
  await expect(page.getByRole('button', { name: 'Copy setup command' })).toHaveText('Copied');
  const copiedSetup = await page.evaluate(() => navigator.clipboard.readText());
  expect(copiedSetup).not.toContain(TOKEN);
  expect(copiedSetup).not.toContain(privateKey);

  await page.getByRole('tab', { name: 'go' }).click();
  await expect(setup).toContainText('elydora install --agent grok');
  await expect(setup).toContainText(`--org-id '${ORG_ID}'`);
  await expect(setup).toContainText("--agent-id 'agent-");
  await expect(setup).not.toContainText('--private-key');
  await expect(setup).not.toContainText('--token');

  await page.screenshot({ path: 'test-results/agent-registration-grok.png', fullPage: true });

  const registrationRequests = observed.filter(({ path }) => path === '/v1/agents/register');
  expect(registrationRequests).toHaveLength(1);
  expect(registrationRequests[0]?.method).toBe('POST');
  expect(registrationRequests[0]?.body).toMatchObject({
    display_name: 'Production Grok',
    responsible_entity: 'Platform Security',
    integration_type: 'grok',
  });
  expect(observed.filter(({ method }) => method === 'PATCH')).toHaveLength(0);
  expect(observed.filter(({ path }) => path === '/v1/auth/token')).toHaveLength(1);
  expect(unexpected).toEqual([]);
  expect(runtimeErrors).toEqual([]);
});

test('renders custom SDK setup with runtime secret references', async ({ page }) => {
  const observed: ObservedRequest[] = [];
  const unexpected: string[] = [];
  const runtimeErrors: string[] = [];
  page.on('pageerror', (error) => runtimeErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') runtimeErrors.push(message.text());
  });
  await mockBackend(page, observed, unexpected);

  await page.goto('/agents');
  await page.getByRole('button', { name: 'Register Agent' }).click();
  await page.getByRole('button', { name: 'SDK', exact: true }).click();
  await page.getByLabel('Display Name').fill('Production SDK');
  await page.locator('form').getByRole('button', { name: 'Register Agent' }).click();

  const privateKey = await page.locator('section[aria-labelledby="private-key-label"] code').innerText();
  await page.getByRole('button', { name: 'Issue API Token' }).click();
  await expect(page.getByText(TOKEN)).toBeVisible();
  await page.getByRole('button', { name: 'Continue' }).click();

  const setup = page.locator('section[aria-labelledby="setup-command-label"] pre');
  await expect(setup).toContainText('process.env.ELYDORA_PRIVATE_KEY');
  await expect(setup).toContainText('process.env.ELYDORA_API_TOKEN');
  await expect(setup).not.toContainText(privateKey);
  await expect(setup).not.toContainText(TOKEN);
  await expect(page.getByText(
    'Set ELYDORA_PRIVATE_KEY and ELYDORA_API_TOKEN in the runtime environment.',
  )).toBeVisible();

  await page.getByRole('tab', { name: 'python' }).click();
  await expect(setup).toContainText('os.environ.get(name)');
  await expect(setup).not.toContainText(privateKey);
  await expect(setup).not.toContainText(TOKEN);

  await page.getByRole('tab', { name: 'go' }).click();
  await expect(setup).toContainText('os.LookupEnv(name)');
  await expect(setup).not.toContainText(privateKey);
  await expect(setup).not.toContainText(TOKEN);
  await page.screenshot({ path: 'test-results/agent-registration-sdk.png', fullPage: true });
  await page.getByRole('dialog').locator(':scope > div').last().evaluate(
    (content) => content.scrollTo(0, content.scrollHeight),
  );
  await page.screenshot({ path: 'test-results/agent-registration-sdk-lower.png', fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('button', { name: 'Copy private key' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Done' })).toBeVisible();
  const pageOverflows = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth,
  );
  expect(pageOverflows).toBe(false);
  await page.screenshot({ path: 'test-results/agent-registration-sdk-mobile.png', fullPage: true });

  const registrationRequests = observed.filter(({ path }) => path === '/v1/agents/register');
  expect(registrationRequests).toHaveLength(1);
  expect(registrationRequests[0]?.body).toMatchObject({ integration_type: 'sdk' });
  expect(unexpected).toEqual([]);
  expect(runtimeErrors).toEqual([]);
});

test('loads the Chinese registration resources', async ({ page }) => {
  const observed: ObservedRequest[] = [];
  const unexpected: string[] = [];
  const runtimeErrors: string[] = [];
  page.on('pageerror', (error) => runtimeErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') runtimeErrors.push(message.text());
  });
  await page.addInitScript(() => localStorage.setItem('i18nextLng', 'zh'));
  await mockBackend(page, observed, unexpected);

  await page.goto('/agents');
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh');
  await page.getByRole('button', { name: '注册代理' }).click();
  await expect(page.getByText('CLI 适配器')).toBeVisible();
  await expect(page.getByText('自定义 SDK')).toBeVisible();
  await expect(page.getByRole('button', { name: 'OpenAI Codex' })).toBeVisible();

  expect(observed.filter(({ path }) => path === '/v1/agents')).toHaveLength(1);
  expect(unexpected).toEqual([]);
  expect(runtimeErrors).toEqual([]);
});

test('blocks the success state when the backend returns a different integration', async ({ page }) => {
  const observed: ObservedRequest[] = [];
  const unexpected: string[] = [];
  await mockBackend(page, observed, unexpected, 'codex');

  await page.goto('/agents');
  await page.getByRole('button', { name: 'Register Agent' }).click();
  await page.getByRole('button', { name: 'Grok Build' }).click();
  await page.getByLabel('Display Name').fill('Mismatched Grok');
  await page.locator('form').getByRole('button', { name: 'Register Agent' }).click();

  await expect(page.locator('form [role="alert"]')).toContainText('different integration type');
  await expect(page.getByText('Private Key — Save This Now')).toHaveCount(0);
  await expect(page.locator('form').getByRole('button', { name: 'Register Agent' })).toBeDisabled();
  expect(observed.filter(({ path }) => path === '/v1/agents/register')).toHaveLength(1);
  expect(observed.filter(({ method }) => method === 'PATCH')).toHaveLength(0);
  expect(unexpected).toEqual([]);
});
