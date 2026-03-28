#!/usr/bin/env node
// OpenEdge Skills Installer — npx entry point
// Usage:
//   npx github:inferis995/openedge openedge
//   npx github:inferis995/openedge openedge-ops
//   npx github:inferis995/openedge openedge-dashboard
//   npx github:inferis995/openedge              (installa tutte)

const https = require('https');
const fs    = require('fs');
const path  = require('path');

const BASE_URL = 'https://raw.githubusercontent.com/inferis995/openedge/master/.claude/skills';
const ALL_SKILLS = ['openedge', 'openedge-ops', 'openedge-dashboard'];

const skill = process.argv[2] || 'all';
const dest  = process.env.CLAUDE_SKILLS_DIR || path.join(process.cwd(), '.claude', 'skills');

fs.mkdirSync(dest, { recursive: true });

function download(name) {
  return new Promise((resolve, reject) => {
    const url  = `${BASE_URL}/${name}.md`;
    const file = path.join(dest, `${name}.md`);
    process.stdout.write(`  Downloading ${name}.md ... `);

    https.get(url, (res) => {
      if (res.statusCode !== 200) {
        console.log('✗');
        return reject(new Error(`HTTP ${res.statusCode} for ${name}.md`));
      }
      const out = fs.createWriteStream(file);
      res.pipe(out);
      out.on('finish', () => { console.log('✓'); resolve(); });
    }).on('error', (e) => { console.log('✗'); reject(e); });
  });
}

async function main() {
  console.log('\nOpenEdge Skills Installer');
  console.log(`→ destination: ${dest}\n`);

  const toInstall = skill === 'all' ? ALL_SKILLS : [skill];

  if (!ALL_SKILLS.includes(toInstall[0]) && skill !== 'all') {
    console.error(`Unknown skill: ${skill}`);
    console.error(`Available: ${ALL_SKILLS.join(', ')}`);
    process.exit(1);
  }

  for (const s of toInstall) {
    await download(s).catch(e => {
      console.error(`Error: ${e.message}`);
      process.exit(1);
    });
  }

  console.log(`\nDone. Skills installed in: ${dest}`);
}

main();
