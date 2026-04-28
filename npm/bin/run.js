#!/usr/bin/env node
'use strict';

const { spawnSync } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');

const isWindows = os.platform() === 'win32';
const binaryName = isWindows ? 'hira.exe' : 'hira';
const binaryPath = path.join(__dirname, binaryName);

if (!fs.existsSync(binaryPath)) {
  console.error(
    `hira binary not found at ${binaryPath}.\n` +
    `Try reinstalling: npm install -g @hira-vn/cli`
  );
  process.exit(1);
}

const result = spawnSync(binaryPath, process.argv.slice(2), {
  stdio: 'inherit',
  windowsHide: false,
});

if (result.error) {
  console.error('Failed to run hira:', result.error.message);
  process.exit(1);
}

process.exit(result.status ?? 0);
