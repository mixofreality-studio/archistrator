#!/usr/bin/env bash
# Push the archistrator app repo PUBLIC under the mixofreality-studio org.
#
# RUN THIS AFTER archistrator-platform/GO-PUBLIC.sh — the webApp dep
# (@mixofreality-studio/archistrator-platform-framework-web) must be live on
# npmjs.org first so the lockfile can repoint there.
#
# Prereqs: gh authed as a mixofreality-studio org owner (gh auth status).
set -euo pipefail
cd "$(dirname "$0")"

ORG=mixofreality-studio
REPO=archistrator

echo "==> 1. repoint webApp lockfile to the published @$ORG package on npmjs.org"
( cd webApp && npm install )   # re-resolves @$ORG/...-framework-web from npmjs (public)
if grep -q "npm.pkg.github.com" webApp/package-lock.json; then
  echo "   WARNING: lock still references npm.pkg.github.com — is the package published to npmjs.org yet?"
fi
git add webApp/package-lock.json
git commit -q -m "chore(webapp): repoint platform-web dep to public npmjs.org" || echo "   (lock unchanged)"

echo "==> 2. create (if needed) + push the app repo PUBLIC under the org"
if ! gh repo view "$ORG/$REPO" >/dev/null 2>&1; then
  gh repo create "$ORG/$REPO" --public --source=. --remote=origin --push
else
  git remote set-url origin "git@github.com:$ORG/$REPO.git" 2>/dev/null \
    || git remote add origin "git@github.com:$ORG/$REPO.git"
  git push -u origin HEAD:main
fi

echo "==> DONE: $ORG/$REPO is PUBLIC. Add the DOCKER_CONFIG secret in the repo,"
echo "    then watch Actions push images to ghcr.io/$ORG/archistrator-{server,webapp}."
