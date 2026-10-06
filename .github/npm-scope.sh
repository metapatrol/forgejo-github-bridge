#!/bin/sh
# Points the @metapatrol scope of the given npm projects at GitHub Packages,
# for builds on the GitHub mirror (.github/workflows/publish.yml). Forgejo
# never runs this.
#
# On Forgejo the scope lives in the forge's npm registry (ui/.npmrc), and the
# lockfiles name its tarball URLs. The same packages are published to
# npm.pkg.github.com from the brand repository's mirror, but under other
# tarball URLs, built by another run: so the .npmrc gets the GitHub registry,
# and the lockfile entries of the scope lose `resolved` and `integrity`, which
# makes npm resolve them from the registry by version. Every other package
# keeps both.
#
# The token is read from NPM_TOKEN at install time (GITHUB_TOKEN with
# packages: read, or a token with read:packages).
#
#   .github/npm-scope.sh ui
set -eu

for dir in "$@"; do
  cat > "$dir/.npmrc" <<'EOF'
@metapatrol:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${NPM_TOKEN}
EOF
  [ -f "$dir/package-lock.json" ] || continue
  node - "$dir/package-lock.json" <<'EOF'
const fs = require('fs');
const file = process.argv[2];
const lock = JSON.parse(fs.readFileSync(file, 'utf8'));
let n = 0;
for (const [path, pkg] of Object.entries(lock.packages ?? {})) {
  if (/(^|\/)node_modules\/@metapatrol\//.test(path)) {
    delete pkg.resolved;
    delete pkg.integrity;
    n++;
  }
}
fs.writeFileSync(file, JSON.stringify(lock, null, 2) + '\n');
console.log(`${file}: ${n} @metapatrol entries resolve from GitHub Packages`);
EOF
done
