const { execSync } = require('child_process');
const os = require('os');
const path = require('path');

const repoRoot = path.resolve(__dirname, '..');
const extPublic = path.join(repoRoot, 'ext-web', 'public');
const targetZip = path.join(repoRoot, 'extension.zip');

console.log(`Packaging Twitch Extension from ${extPublic} to ${targetZip}...`);

if (os.platform() === 'win32') {
  execSync(`powershell -Command "Compress-Archive -Path '${extPublic}\\*' -DestinationPath '${targetZip}' -Force"`, {
    stdio: 'inherit',
  });
} else {
  execSync(`cd "${extPublic}" && zip -r "${targetZip}" *`, {
    stdio: 'inherit',
  });
}

console.log(`Extension package created successfully: ${targetZip}`);
