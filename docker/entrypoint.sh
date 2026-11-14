#!/bin/sh
# Refresh the page files in the data volume on every start (so a new image updates the page),
# but never touch the user's own data: users.json, secret.key, users/, icons/ and, on installs that
# date from before accounts, services.json, backups/ and wallpaper/.
set -e
mkdir -p "$ROOT"
# The server runs as an ordinary user. A volume made by an older image, or a folder made by Docker
# for a bind mount, can belong to root; say so plainly instead of failing on the first copy.
if ! ( : > "$ROOT/.write-test" ) 2>/dev/null; then
  echo "toran: cannot write to $ROOT as user $(id -u):$(id -g)." >&2
  echo "toran: give that user the folder once, e.g.  docker run --rm -v <your volume or folder>:/data --user 0 --entrypoint chown <this image> -R $(id -u):$(id -g) /data" >&2
  echo "toran: or run the container as the folder's owner with  --user <uid>:<gid>  (see docs/deploy.md)." >&2
  exit 1
fi
rm -f "$ROOT/.write-test"
cp /opt/toran/index.html /opt/toran/custom.css "$ROOT"/
cp -r /opt/toran/wallpapers "$ROOT"/
# A new install gets the sample dashboard, which the first admin you create then takes over (see legacy.go).
# Once accounts exist it is never read again, and never overwritten.
[ -f "$ROOT/services.json" ] || [ -f "$ROOT/users.json" ] || cp /opt/toran/services.sample.json "$ROOT/services.json"
exec toran
