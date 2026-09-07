#!/usr/bin/env bash
# Isolated MongoDB 8 fixture. Usage: bash scripts/mongotest.sh [--replica-set] [--mongosh]
set +x
set -euo pipefail
umask 077

native=false
replica_set=''
for arg in "$@"; do
  case "$arg" in
    --mongosh) native=true ;;
    --replica-set) replica_set=vaulty-fixture ;;
    *) printf 'Usage: bash scripts/mongotest.sh [--replica-set] [--mongosh]\n' >&2; exit 2 ;;
  esac
done
for tool in docker go openssl; do
  command -v "$tool" >/dev/null || { printf 'Required tool missing: %s\n' "$tool" >&2; exit 1; }
done
docker version >/dev/null 2>&1 || { printf 'Docker daemon is unavailable.\n' >&2; exit 1; }
repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd -- "$repo"

# Fixed patch tag, verified against the official multi-platform image manifest.
image=mongo:8.0.13
id="$(openssl rand -hex 12)"
name="vaulty-mongotest-$id"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/vaulty-mongotest.XXXXXXXX")"
cleanup() {
  status=$?
  trap - EXIT INT TERM
  # Never remove a container unless its ownership label matches this invocation.
  owner="$(docker inspect --format '{{ index .Config.Labels "vaulty.mongotest" }}' "$name" 2>/dev/null || true)"
  if [ "$owner" = "$id" ]; then
    docker rm -fv "$name" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$tmp"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

password="$(openssl rand -hex 24)"
username="vaulty_mongotest_$id"
printf 'MONGO_INITDB_ROOT_USERNAME=%s\nMONGO_INITDB_ROOT_PASSWORD=%s\n' "$username" "$password" >"$tmp/root.env"
printf 'VAULTY_MONGO_TEST_REPLICA_SET=%s\n' "$replica_set" >>"$tmp/root.env"
chmod 600 "$tmp/root.env"
ready_js='try {
  const u = encodeURIComponent(process.env.MONGO_INITDB_ROOT_USERNAME);
  const p = encodeURIComponent(process.env.MONGO_INITDB_ROOT_PASSWORD);
  const c = new Mongo("mongodb://" + u + ":" + p + "@127.0.0.1:27017/admin?directConnection=true&retryWrites=false&serverSelectionTimeoutMS=2000");
  const admin = c.getDB("admin");
  if (admin.runCommand({ping:1}).ok !== 1) quit(1);
  const rs = process.env.VAULTY_MONGO_TEST_REPLICA_SET;
  if (rs) {
    let status;
    try { status = admin.runCommand({replSetGetStatus:1}); }
    catch (e) { if (e.code !== 94) quit(1); status = {code:94}; }
    if (status.code === 94) {
      if (admin.runCommand({replSetInitiate:{_id:rs,members:[{_id:0,host:"localhost:27017"}]}}).ok !== 1) quit(1);
    } else if (status.ok !== 1) quit(1);
    const hello = admin.runCommand({hello:1});
    if (hello.isWritablePrimary !== true || hello.setName !== rs) quit(1);
  }
  quit(0);
} catch (_) { quit(1); }'
extra=(--label "vaulty.mongotest=$id")
mongo_command=(mongod --bind_ip_all --auth)
if [ -n "$replica_set" ]; then
  openssl rand -base64 756 >"$tmp/replica.key"
  extra+=(--entrypoint bash)
  # docker cp creates a root-owned file. Set container-local ownership before
  # the official entrypoint drops privileges and initializes the synthetic root.
  mongo_command=(-c 'chown mongodb:mongodb /data/configdb/fixture.key && chmod 400 /data/configdb/fixture.key && exec /usr/local/bin/docker-entrypoint.sh "$@"' --
    mongod --bind_ip_all --auth --replSet "$replica_set" --keyFile /data/configdb/fixture.key)
fi
if [ "$native" = true ] && [ "$(uname -s)" = Linux ]; then
  extra+=(--add-host host.docker.internal:host-gateway)
fi
printf 'Starting isolated %s fixture (mode: %s).\n' "$image" "${replica_set:-standalone}"
docker create --name "$name" \
  --env-file "$tmp/root.env" -p 127.0.0.1::27017 \
  --health-cmd "mongosh --nodb --quiet --eval '$ready_js' >/dev/null 2>&1" \
  --health-interval 2s --health-timeout 5s --health-retries 60 \
  "${extra[@]}" "$image" "${mongo_command[@]}" >/dev/null
if [ -n "$replica_set" ]; then
  docker cp "$tmp/replica.key" "$name:/data/configdb/fixture.key"
fi
docker start "$name" >/dev/null
deadline=$((SECONDS + 120))
ready=false
while [ "$SECONDS" -lt "$deadline" ]; do
  state="$(docker inspect --format '{{.State.Running}} {{.State.Health.Status}}' "$name")"
  if [ "$state" = 'true healthy' ]; then ready=true; break; fi
  case "$state" in false*) printf 'Fixture exited before readiness.\n' >&2; exit 1 ;; esac
  sleep 1
done
[ "$ready" = true ] || { printf 'Fixture readiness timed out after 120 seconds.\n' >&2; exit 1; }
port="$(docker inspect --format '{{with index .NetworkSettings.Ports "27017/tcp"}}{{(index . 0).HostPort}}{{end}}' "$name")"
case "$port" in ''|*[!0-9]*) printf 'Cannot determine fixture port.\n' >&2; exit 1 ;; esac

container=''
if [ "$native" = true ]; then container="$name"; fi
# Only synthetic fixture credentials enter this test process, never argv or output.
export VAULTY_MONGO_TEST_URI="mongodb://$username:$password@127.0.0.1:$port/admin?authSource=admin&directConnection=true"
export VAULTY_MONGO_TEST_CONTAINER="$container"
export VAULTY_MONGO_TEST_REPLICA_SET="$replica_set"
unset password
printf 'Running MongoDB integration tests (native mongosh: %s).\n' "$native"
go test -mod=readonly -tags=mongointegration ./internal/dbproxy -run '^TestMongoIntegration($|DriverHello$|DriverSASL$)' -count=1 -timeout=180s
