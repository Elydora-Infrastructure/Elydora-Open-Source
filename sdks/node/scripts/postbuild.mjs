import { readFile, writeFile } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const cliPath = resolve(__dirname, '..', 'dist', 'cli.js');

const content = await readFile(cliPath, 'utf-8');
if (!content.startsWith('#!/')) {
  await writeFile(cliPath, '#!/usr/bin/env node\n' + content, 'utf-8');
}
