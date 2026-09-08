import { readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { stdin as input, stdout as output } from 'node:process';
import { createInterface } from 'node:readline/promises';
import { fileURLToPath } from 'node:url';

const rootDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');

const paths = {
  appConfig: 'apps/web-ele/src/config/app.ts',
  appEnv: 'apps/web-ele/.env',
  appEnvExample: 'apps/web-ele/.env.example',
  appEnvDevelopment: 'apps/web-ele/.env.development',
  appHtml: 'apps/web-ele/index.html',
  appPackage: 'apps/web-ele/package.json',
  coreAbout: 'packages/effects/common-ui/src/ui/about/about.vue',
  coreCopyright: 'packages/effects/layouts/src/basic/copyright/copyright.vue',
  corePreferences: 'packages/@core/preferences/src/config.ts',
  corePreferencesSnapshot:
    'packages/@core/preferences/__tests__/__snapshots__/config.test.ts.snap',
  dockerScript: 'scripts/deploy/build-local-docker-image.sh',
  logo: 'apps/web-ele/public/logo.svg',
  mockPackage: 'apps/backend-mock/package.json',
  mockReadme: 'apps/backend-mock/README.md',
  mockRoute: 'apps/backend-mock/routes/[...].ts',
  readme: 'README.md',
  rootPackage: 'package.json',
  viteEnv: 'internal/vite-config/src/utils/env.ts',
};

function absolutePath(relativePath) {
  return resolve(rootDir, relativePath);
}

async function readText(relativePath) {
  return readFile(absolutePath(relativePath), 'utf8');
}

async function writeText(relativePath, content) {
  await writeFile(
    absolutePath(relativePath),
    `${content.replaceAll('\r\n', '\n').trimEnd()}\n`,
    'utf8',
  );
}

function readEnvValue(content, key) {
  return content.match(new RegExp(`^${key}=(.*)$`, 'm'))?.[1] ?? '';
}

function setEnvValue(content, key, value) {
  const pattern = new RegExp(`^${key}=.*$`, 'm');
  if (!pattern.test(content)) {
    throw new Error(`Missing ${key} in environment file`);
  }
  return content.replace(pattern, `${key}=${value}`);
}

function replaceAll(content, currentValue, nextValue) {
  return currentValue === nextValue
    ? content
    : content.split(currentValue).join(nextValue);
}

function assertPackageName(value, label) {
  const packageNamePattern =
    /^(?:@[a-z0-9][a-z0-9._-]*\/)?[a-z0-9][a-z0-9._-]*$/;
  if (!packageNamePattern.test(value)) {
    throw new Error(`${label} is not a valid npm package name: ${value}`);
  }
}

function assertNamespace(value) {
  if (!/^[a-z0-9][a-z0-9-]*$/.test(value)) {
    throw new Error(
      `Namespace must use lowercase letters, numbers and hyphens: ${value}`,
    );
  }
}

function assertPort(value) {
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65_535) {
    throw new Error(`Invalid development port: ${value}`);
  }
}

async function promptValue(readline, label, currentValue) {
  const answer = await readline.question(`${label} (${currentValue}): `);
  return answer.trim() || currentValue;
}

const appEnv = await readText(paths.appEnv);
const developmentEnv = await readText(paths.appEnvDevelopment);
const rootPackage = JSON.parse(await readText(paths.rootPackage));
const appPackage = JSON.parse(await readText(paths.appPackage));
const mockPackage = JSON.parse(await readText(paths.mockPackage));

const current = {
  apiUrl: readEnvValue(developmentEnv, 'VITE_GLOB_API_URL') || '/api',
  appPackageName: appPackage.name,
  displayName: readEnvValue(appEnv, 'VITE_APP_TITLE') || 'Nova Admin',
  namespace: readEnvValue(appEnv, 'VITE_APP_NAMESPACE') || 'nova-admin',
  port: readEnvValue(developmentEnv, 'VITE_PORT') || '5173',
  rootPackageName: rootPackage.name,
};

const readline = createInterface({ input, output });
let next;
try {
  next = {
    displayName: await promptValue(
      readline,
      'Display name',
      current.displayName,
    ),
    namespace: await promptValue(
      readline,
      'Storage namespace',
      current.namespace,
    ),
    rootPackageName: await promptValue(
      readline,
      'Root package name',
      current.rootPackageName,
    ),
    appPackageName: await promptValue(
      readline,
      'App package name',
      current.appPackageName,
    ),
    port: await promptValue(readline, 'Development port', current.port),
    apiUrl: await promptValue(readline, 'Development API URL', current.apiUrl),
  };
} finally {
  readline.close();
}

assertNamespace(next.namespace);
assertPackageName(next.rootPackageName, 'Root package name');
assertPackageName(next.appPackageName, 'App package name');
assertPort(next.port);

rootPackage.name = next.rootPackageName;
for (const [scriptName, script] of Object.entries(rootPackage.scripts ?? {})) {
  rootPackage.scripts[scriptName] = replaceAll(
    script,
    current.appPackageName,
    next.appPackageName,
  );
}
appPackage.name = next.appPackageName;
appPackage.description = `${next.displayName} web application`;
mockPackage.description = `Local mock service for ${next.displayName}`;

await writeText(paths.rootPackage, JSON.stringify(rootPackage, null, 2));
await writeText(paths.appPackage, JSON.stringify(appPackage, null, 2));
await writeText(paths.mockPackage, JSON.stringify(mockPackage, null, 2));

for (const envPath of [paths.appEnv, paths.appEnvExample]) {
  let content = await readText(envPath);
  content = setEnvValue(content, 'VITE_APP_TITLE', next.displayName);
  content = setEnvValue(content, 'VITE_APP_NAMESPACE', next.namespace);
  content = setEnvValue(
    content,
    'VITE_APP_STORE_SECURE_KEY',
    `${next.namespace}-change-this-key`,
  );
  await writeText(envPath, content);
}

let nextDevelopmentEnv = developmentEnv;
nextDevelopmentEnv = setEnvValue(nextDevelopmentEnv, 'VITE_PORT', next.port);
nextDevelopmentEnv = setEnvValue(
  nextDevelopmentEnv,
  'VITE_GLOB_API_URL',
  next.apiUrl,
);
await writeText(paths.appEnvDevelopment, nextDevelopmentEnv);

for (const textPath of [
  paths.appConfig,
  paths.appHtml,
  paths.coreAbout,
  paths.coreCopyright,
  paths.corePreferences,
  paths.corePreferencesSnapshot,
  paths.mockReadme,
  paths.mockRoute,
  paths.readme,
  paths.viteEnv,
]) {
  const content = await readText(textPath);
  await writeText(
    textPath,
    replaceAll(
      replaceAll(content, current.displayName, next.displayName),
      current.namespace,
      next.namespace,
    ),
  );
}

const initial = [...next.displayName.trim()][0]?.toUpperCase() || 'A';
let logo = await readText(paths.logo);
logo = logo.replace(/aria-label="[^"]*"/, `aria-label="${next.displayName}"`);
logo = logo.replaceAll(
  `${current.namespace}-gradient`,
  `${next.namespace}-gradient`,
);
logo = logo.replace(
  /(<text[^>]*data-template-initial=")[^"]*("[^>]*>)[^<]*(<\/text>)/,
  `$1${initial}$2${initial}$3`,
);
await writeText(paths.logo, logo);

let dockerScript = await readText(paths.dockerScript);
dockerScript = dockerScript.replace(
  /^IMAGE_NAME=".*"$/m,
  `IMAGE_NAME="${next.namespace}-local"`,
);
await writeText(paths.dockerScript, dockerScript);

console.log('\nTemplate initialized successfully.');
console.log(
  'Run `pnpm install` to refresh workspace metadata, then `pnpm dev`.',
);
