#!/usr/bin/env node
'use strict';

const https = require('https');
const fs = require('fs');
const path = require('path');
const os = require('os');
const zlib = require('zlib');

function getPlatformInfo() {
  const platform = os.platform();
  const arch = os.arch();

  const goos =
    platform === 'win32' ? 'windows' :
    platform === 'darwin' ? 'darwin' :
    'linux';

  const goarch =
    arch === 'x64' ? 'amd64' :
    arch === 'arm64' ? 'arm64' :
    null;

  if (!goarch) {
    throw new Error(`Unsupported CPU architecture: ${arch}`);
  }
  if (goos === 'windows' && goarch === 'arm64') {
    throw new Error('Windows arm64 is not supported. Install manually: https://github.com/hira-vn/cli/releases');
  }

  return { goos, goarch, isWindows: goos === 'windows' };
}

function getVersion() {
  const pkg = require('../package.json');
  const v = pkg.version;
  if (!v || v === '0.0.0-placeholder') {
    throw new Error('package.json version is a placeholder — cannot resolve download URL.');
  }
  return v;
}

function httpsGet(url, redirectsLeft = 5) {
  return new Promise((resolve, reject) => {
    https.get(url, { headers: { 'User-Agent': 'hira-npm-postinstall' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        if (redirectsLeft === 0) return reject(new Error('Too many redirects'));
        res.resume();
        resolve(httpsGet(res.headers.location, redirectsLeft - 1));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`HTTP ${res.statusCode} fetching ${url}`));
        return;
      }
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('error', reject);
    }).on('error', reject);
  });
}

// GoReleaser wraps the binary in a directory: hira_darwin_amd64/hira
// Match on basename so the directory prefix is irrelevant.
function extractBinaryFromTarGz(buffer, binaryName) {
  const inflated = zlib.gunzipSync(buffer);
  let offset = 0;

  while (offset + 512 <= inflated.length) {
    const header = inflated.slice(offset, offset + 512);
    if (header.every((b) => b === 0)) break;

    const nameRaw = header.slice(0, 100);
    const nameEnd = nameRaw.indexOf(0);
    const name = nameRaw.slice(0, nameEnd === -1 ? 100 : nameEnd).toString('utf8');

    const sizeStr = header.slice(124, 136).toString('ascii').trim().replace(/\0/g, '');
    const size = parseInt(sizeStr, 8) || 0;

    const typeFlag = String.fromCharCode(header[156]);
    const isFile = typeFlag === '0' || typeFlag === '\0';

    offset += 512;

    if (isFile && path.basename(name) === binaryName) {
      return inflated.slice(offset, offset + size);
    }

    offset += Math.ceil(size / 512) * 512;
  }

  throw new Error(`Binary "${binaryName}" not found in archive`);
}

function extractBinaryFromZip(buffer, binaryName) {
  const EOCD_SIG = 0x06054b50;
  let eocdOffset = -1;
  for (let i = buffer.length - 22; i >= 0; i--) {
    if (buffer.readUInt32LE(i) === EOCD_SIG) { eocdOffset = i; break; }
  }
  if (eocdOffset === -1) throw new Error('Invalid ZIP: EOCD not found');

  const cdOffset = buffer.readUInt32LE(eocdOffset + 16);
  const numEntries = buffer.readUInt16LE(eocdOffset + 10);

  let cdPos = cdOffset;
  for (let i = 0; i < numEntries; i++) {
    if (buffer.readUInt32LE(cdPos) !== 0x02014b50) break;

    const compMethod = buffer.readUInt16LE(cdPos + 10);
    const compSize = buffer.readUInt32LE(cdPos + 20);
    const uncompSize = buffer.readUInt32LE(cdPos + 24);
    const fnLen = buffer.readUInt16LE(cdPos + 28);
    const extraLen = buffer.readUInt16LE(cdPos + 30);
    const commentLen = buffer.readUInt16LE(cdPos + 32);
    const localOffset = buffer.readUInt32LE(cdPos + 42);
    const name = buffer.slice(cdPos + 46, cdPos + 46 + fnLen).toString('utf8');

    if (path.basename(name) === binaryName || path.basename(name) === binaryName + '.exe') {
      const localFnLen = buffer.readUInt16LE(localOffset + 26);
      const localExtra = buffer.readUInt16LE(localOffset + 28);
      const dataOffset = localOffset + 30 + localFnLen + localExtra;

      if (compMethod === 0) {
        return Promise.resolve(buffer.slice(dataOffset, dataOffset + uncompSize));
      } else if (compMethod === 8) {
        const compressed = buffer.slice(dataOffset, dataOffset + compSize);
        return new Promise((resolve, reject) => {
          zlib.inflateRaw(compressed, (err, data) => (err ? reject(err) : resolve(data)));
        });
      } else {
        return Promise.reject(new Error(`Unsupported ZIP compression method: ${compMethod}`));
      }
    }

    cdPos += 46 + fnLen + extraLen + commentLen;
  }

  return Promise.reject(new Error(`Binary "${binaryName}" not found in ZIP`));
}

async function main() {
  const { goos, goarch, isWindows } = getPlatformInfo();
  const version = getVersion();

  const binaryName = isWindows ? 'hira.exe' : 'hira';
  const archiveName = isWindows
    ? `hira_${goos}_${goarch}.zip`
    : `hira_${goos}_${goarch}.tar.gz`;

  const url = `https://github.com/hira-vn/cli/releases/download/v${version}/${archiveName}`;
  const binDir = path.join(__dirname, '..', 'bin');
  const binaryPath = path.join(binDir, binaryName);

  console.log(`Downloading hira v${version} for ${goos}/${goarch}...`);
  console.log(`  from: ${url}`);

  if (!fs.existsSync(binDir)) fs.mkdirSync(binDir, { recursive: true });

  const archiveBuffer = await httpsGet(url);
  const binaryBuffer = isWindows
    ? await extractBinaryFromZip(archiveBuffer, 'hira')
    : extractBinaryFromTarGz(archiveBuffer, 'hira');

  fs.writeFileSync(binaryPath, binaryBuffer);
  if (!isWindows) fs.chmodSync(binaryPath, 0o755);

  console.log(`  installed to: ${binaryPath}`);
}

main().catch((err) => {
  console.error('\nFailed to install hira binary:', err.message);
  console.error('Install manually from: https://github.com/hira-vn/cli/releases');
  process.exit(1);
});
