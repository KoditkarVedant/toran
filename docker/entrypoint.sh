#!/bin/sh
# Refresh the page files in the data volume on every start (so a new image updates the page),
# but never touch the user's own data: services.json, backups/, wallpaper/, icons/.
set -e
mkdir -p "$ROOT"
cp /opt/toran/index.html /opt/toran/custom.css "$ROOT"/
cp -r /opt/toran/wallpapers "$ROOT"/
[ -f "$ROOT/services.json" ] || cp /opt/toran/services.sample.json "$ROOT/services.json"
exec toran
