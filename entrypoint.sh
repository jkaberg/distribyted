#!/usr/bin/env sh
set -e

# Ensure a valid passwd/group entry exists for PUID/PGID so fusermount can resolve username
PUID="${PUID:-1000}"
PGID="${PGID:-1000}"

# Create group with PGID if missing
if ! grep -q ":${PGID}:" /etc/group 2>/dev/null; then
  addgroup -S -g "$PGID" app 2>/dev/null || true
fi
# Determine group name for the given PGID
GROUP_NAME=$(getent group "$PGID" 2>/dev/null | cut -d: -f1)
[ -z "$GROUP_NAME" ] && GROUP_NAME=app

# Create user with PUID if missing
if ! awk -F: -v id="$PUID" '$3==id{found=1} END{exit !found}' /etc/passwd; then
  adduser -S -D -H -u "$PUID" -G "$GROUP_NAME" app 2>/dev/null || true
fi

# Add user to fuse group if present
if grep -q '^fuse:' /etc/group 2>/dev/null; then
  addgroup app fuse 2>/dev/null || true
fi

# Ensure writable dirs exist (image already chmod 0777, just in case in future)
mkdir -p /data /data/logs /config 2>/dev/null || true

# Ensure ownership of directories is correct
# Avoid chown on FUSE mount path which can error (e.g., "Socket not connected")
MOUNT_PATH="${FUSE_PATH:-/data/mount}"
chown "$PUID:$PGID" /data /config 2>/dev/null || true
if [ -d "/data" ]; then
  for d in /data/*; do
    [ "$d" = "$MOUNT_PATH" ] && continue
    # Skip if glob didn't match
    [ "$d" = "/data/*" ] && break
    chown -R "$PUID:$PGID" "$d" 2>/dev/null || true
  done
fi
chown -R "$PUID:$PGID" /config 2>/dev/null || true

exec su-exec "$PUID:$PGID" "$@"


