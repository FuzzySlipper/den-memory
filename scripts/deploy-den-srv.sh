#!/usr/bin/env bash
# Deploy the Go Den Memories service to den-srv.
# Default mode is dry-run after local validation; pass --yes to mutate den-srv.
set -euo pipefail

REMOTE_HOST=${REMOTE_HOST:-den-srv}
SERVICE_NAME=${SERVICE_NAME:-den-memory}
SERVICE_USER=${SERVICE_USER:-den-memory}
SERVICE_ROOT=${SERVICE_ROOT:-/data/services/den-memory}
LISTEN_ADDR=${DEN_MEMORIES_ADDR:-127.0.0.1:8780}
RUN_LOCAL_SMOKE=1
RUN_TESTS=1
YES=0
FORCE_ENV=0
ENABLE_SERVICE=1

usage() {
  cat <<USAGE
Usage: $0 [--yes] [options]

Build and deploy den-memories from this checkout to den-srv.

Options:
  --yes                  Actually install/restart on the remote host. Default is dry-run.
  --remote HOST          SSH target for den-srv (default: ${REMOTE_HOST}).
  --addr HOST:PORT       Service listen address written to env/server.env (default: ${LISTEN_ADDR}).
  --service-root PATH    Remote service root (default: ${SERVICE_ROOT}).
  --service-user USER    Remote service user/group (default: ${SERVICE_USER}).
  --service-name NAME    Systemd service name without .service (default: ${SERVICE_NAME}).
  --force-env            Rewrite env/server.env even if it already exists.
  --skip-tests           Skip go test/vet/validator preflight (not recommended).
  --skip-local-smoke     Skip temporary local HTTP smoke (not recommended).
  --no-enable            Restart/start but do not enable the service at boot.
  -h, --help             Show this help.

Examples:
  $0                     # validate/build and print remote install plan only
  $0 --yes               # deploy/restart den-memory.service on den-srv
  $0 --yes --addr 0.0.0.0:8780 --force-env
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes) YES=1 ;;
    --remote) REMOTE_HOST=${2:?missing value for --remote}; shift ;;
    --addr) LISTEN_ADDR=${2:?missing value for --addr}; shift ;;
    --service-root) SERVICE_ROOT=${2:?missing value for --service-root}; shift ;;
    --service-user) SERVICE_USER=${2:?missing value for --service-user}; shift ;;
    --service-name) SERVICE_NAME=${2:?missing value for --service-name}; shift ;;
    --force-env) FORCE_ENV=1 ;;
    --skip-tests) RUN_TESTS=0 ;;
    --skip-local-smoke) RUN_LOCAL_SMOKE=0 ;;
    --no-enable) ENABLE_SERVICE=0 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 2; }; }
need go
need curl
need tar
need ssh
need scp
need jq

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$REPO_ROOT"

if [[ ! -f go.mod || ! -d contracts/v0 || ! -f cmd/den-memories/main.go ]]; then
  echo "This script must run from the den-memory repository checkout." >&2
  exit 2
fi

GIT_HEAD=$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
GIT_STATUS=$(git status --short 2>/dev/null || true)
if [[ -n "$GIT_STATUS" ]]; then
  echo "Refusing to deploy from a dirty checkout:" >&2
  echo "$GIT_STATUS" >&2
  exit 2
fi

if [[ "$RUN_TESTS" -eq 1 ]]; then
  echo "== preflight: go test ./... -count=1 =="
  go test ./... -count=1
  echo "== preflight: go vet ./... =="
  go vet ./...
  echo "== preflight: contract validator =="
  go run ./cmd/den-memory-validate
else
  echo "WARNING: skipping tests/validator" >&2
fi

STAGE=$(mktemp -d /tmp/den-memory-stage.XXXXXX)
ARTIFACT=$(mktemp /tmp/den-memory-app.XXXXXX.tar.gz)
cleanup() {
  rm -rf "$STAGE" "$ARTIFACT"
}
trap cleanup EXIT

mkdir -p "$STAGE/app"
echo "== build binaries =="
go build -trimpath -o "$STAGE/app/den-memories" ./cmd/den-memories
go build -trimpath -o "$STAGE/app/den-memory-audit" ./cmd/den-memory-audit
cp -a contracts "$STAGE/app/contracts"
cp -a examples "$STAGE/app/examples"
cp -a docs "$STAGE/app/docs"
cp -a README.md "$STAGE/app/README.md"
printf '%s\n' "$GIT_HEAD" > "$STAGE/app/BUILD_GIT_HEAD"
chmod 0755 "$STAGE/app/den-memories" "$STAGE/app/den-memory-audit"

tar -C "$STAGE/app" -czf "$ARTIFACT" .

if [[ "$RUN_LOCAL_SMOKE" -eq 1 ]]; then
  echo "== local smoke =="
  smoke_dir=$(mktemp -d /tmp/den-memory-smoke.XXXXXX)
  smoke_port=18780
  smoke_log="$smoke_dir/server.log"
  "$STAGE/app/den-memories" -db "$smoke_dir/den-memories.sqlite" -addr "127.0.0.1:${smoke_port}" -root "$STAGE/app" >"$smoke_log" 2>&1 &
  smoke_pid=$!
  smoke_cleanup() { kill "$smoke_pid" 2>/dev/null || true; wait "$smoke_pid" 2>/dev/null || true; rm -rf "$smoke_dir"; }
  trap 'smoke_cleanup; cleanup' EXIT
  for _ in $(seq 1 30); do
    if curl -fsS "http://127.0.0.1:${smoke_port}/health" >/dev/null; then
      break
    fi
    if ! kill -0 "$smoke_pid" 2>/dev/null; then
      echo "local smoke server exited" >&2
      cat "$smoke_log" >&2
      exit 1
    fi
    sleep 1
  done
  curl -fsS "http://127.0.0.1:${smoke_port}/health" | jq .
  curl -fsS "http://127.0.0.1:${smoke_port}/api/version" | jq .
  curl -fsS "http://127.0.0.1:${smoke_port}/api/doctor/report" >/dev/null
  curl -fsS "http://127.0.0.1:${smoke_port}/api/audit/export?format=jsonl" >/dev/null
  smoke_cleanup
  trap cleanup EXIT
else
  echo "WARNING: skipping local smoke" >&2
fi

cat <<PLAN
== remote deployment plan ==
remote:        ${REMOTE_HOST}
service:       ${SERVICE_NAME}.service
service user:  ${SERVICE_USER}
root:          ${SERVICE_ROOT}
app:           ${SERVICE_ROOT}/app
env:           ${SERVICE_ROOT}/env/server.env
data:          ${SERVICE_ROOT}/data
listen addr:   ${LISTEN_ADDR}
database:      ${SERVICE_ROOT}/data/den-memories.sqlite
git head:      ${GIT_HEAD}
force env:     ${FORCE_ENV}
enable boot:   ${ENABLE_SERVICE}
mutating:      ${YES}
PLAN

if [[ "$YES" -ne 1 ]]; then
  echo "Dry run complete. Re-run with --yes to install/restart on ${REMOTE_HOST}."
  exit 0
fi

REMOTE_ARTIFACT="/tmp/den-memory-app-${GIT_HEAD}-$$.tar.gz"
echo "== upload artifact =="
scp -q "$ARTIFACT" "${REMOTE_HOST}:${REMOTE_ARTIFACT}"

REMOTE_SCRIPT=$(mktemp /tmp/den-memory-remote.XXXXXX.sh)
cat >"$REMOTE_SCRIPT" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail
artifact=$1
service_name=$2
service_user=$3
service_root=$4
listen_addr=$5
force_env=$6
enable_service=$7

app_dir="${service_root}/app"
env_dir="${service_root}/env"
data_dir="${service_root}/data"
env_file="${env_dir}/server.env"
db_path="${data_dir}/den-memories.sqlite"
unit_file="/etc/systemd/system/${service_name}.service"
service_unit="${service_name}.service"
smoke_port="${listen_addr##*:}"
smoke_url="http://127.0.0.1:${smoke_port}"
ts=$(date -u +%Y%m%dT%H%M%SZ)

sudo -n true
if ! getent group "${service_user}" >/dev/null; then
  sudo groupadd --system "${service_user}"
fi
if ! getent passwd "${service_user}" >/dev/null; then
  sudo useradd --system --home-dir "${service_root}" --shell /usr/sbin/nologin --gid "${service_user}" "${service_user}"
fi

sudo install -d -o root -g root -m 0755 "${service_root}"
sudo install -d -o root -g "${service_user}" -m 0750 "${env_dir}"
sudo install -d -o "${service_user}" -g "${service_user}" -m 0750 "${data_dir}"

if [[ ! -f "${env_file}" || "${force_env}" == "1" ]]; then
  tmp_env=$(mktemp)
  cat >"${tmp_env}" <<ENV
DEN_MEMORIES_ADDR=${listen_addr}
DEN_MEMORIES_DB=${db_path}
DEN_MEMORIES_ROOT=${app_dir}
ENV
  if [[ -f "${env_file}" ]]; then
    sudo cp -a "${env_file}" "${env_file}.${ts}.bak"
  fi
  sudo install -o root -g "${service_user}" -m 0640 "${tmp_env}" "${env_file}"
  rm -f "${tmp_env}"
else
  echo "preserving existing ${env_file}; pass --force-env to rewrite it"
fi

new_app="${service_root}/app.new.${ts}"
old_app="${service_root}/app.previous"
sudo rm -rf "${new_app}"
sudo install -d -o root -g root -m 0755 "${new_app}"
sudo tar -xzf "${artifact}" -C "${new_app}"
sudo chown -R root:root "${new_app}"
sudo find "${new_app}" -type d -exec chmod 0755 {} +
sudo find "${new_app}" -type f -exec chmod 0644 {} +
sudo chmod 0755 "${new_app}/den-memories" "${new_app}/den-memory-audit"

unit_tmp=$(mktemp)
cat >"${unit_tmp}" <<UNIT
[Unit]
Description=Den Memories service
After=network.target

[Service]
Type=simple
User=${service_user}
Group=${service_user}
WorkingDirectory=${app_dir}
EnvironmentFile=${env_file}
ExecStart=${app_dir}/den-memories -db \${DEN_MEMORIES_DB} -addr \${DEN_MEMORIES_ADDR} -root \${DEN_MEMORIES_ROOT}
Restart=on-failure
RestartSec=5
UMask=0027
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true

[Install]
WantedBy=multi-user.target
UNIT
if [[ -f "${unit_file}" ]]; then
  sudo cp -a "${unit_file}" "${unit_file}.${ts}.bak"
fi
sudo install -o root -g root -m 0644 "${unit_tmp}" "${unit_file}"
rm -f "${unit_tmp}"

if systemctl list-unit-files "${service_unit}" >/dev/null 2>&1; then
  sudo systemctl stop "${service_unit}" || true
fi
if [[ -d "${app_dir}" ]]; then
  sudo rm -rf "${old_app}"
  sudo mv "${app_dir}" "${old_app}"
fi
sudo mv "${new_app}" "${app_dir}"

sudo systemctl daemon-reload
if [[ "${enable_service}" == "1" ]]; then
  sudo systemctl enable "${service_unit}" >/dev/null
fi
sudo systemctl restart "${service_unit}"

for _ in $(seq 1 30); do
  if curl -fsS "${smoke_url}/health" >/dev/null; then
    break
  fi
  sleep 1
done
set +e
health=$(curl -fsS "${smoke_url}/health")
health_rc=$?
version=$(curl -fsS "${smoke_url}/api/version")
version_rc=$?
doctor=$(curl -fsS "${smoke_url}/api/doctor/report")
doctor_rc=$?
audit=$(curl -fsS "${smoke_url}/api/audit/export?format=jsonl" | head -n 2)
audit_rc=${PIPESTATUS[0]}
set -e
if [[ ${health_rc} -ne 0 || ${version_rc} -ne 0 || ${doctor_rc} -ne 0 || ${audit_rc} -ne 0 ]]; then
  echo "deploy smoke failed; rolling back app directory" >&2
  sudo systemctl stop "${service_unit}" || true
  if [[ -d "${old_app}" ]]; then
    sudo rm -rf "${app_dir}"
    sudo mv "${old_app}" "${app_dir}"
    sudo systemctl restart "${service_unit}" || true
  fi
  exit 1
fi

echo "== service status =="
systemctl is-active "${service_unit}"
echo "== health =="
printf '%s\n' "${health}" | jq .
echo "== version =="
printf '%s\n' "${version}" | jq .
echo "== doctor summary =="
printf '%s\n' "${doctor}" | jq '.summary // .issues // .'
echo "== audit head =="
printf '%s\n' "${audit}"
echo "== deployed files =="
stat -c '%A %U:%G %n' "${service_root}" "${app_dir}" "${env_file}" "${data_dir}"
rm -f "${artifact}"
REMOTE
chmod 0755 "$REMOTE_SCRIPT"
scp -q "$REMOTE_SCRIPT" "${REMOTE_HOST}:${REMOTE_SCRIPT}"
rm -f "$REMOTE_SCRIPT"

echo "== remote install/restart =="
ssh "${REMOTE_HOST}" "bash ${REMOTE_SCRIPT} ${REMOTE_ARTIFACT@Q} ${SERVICE_NAME@Q} ${SERVICE_USER@Q} ${SERVICE_ROOT@Q} ${LISTEN_ADDR@Q} ${FORCE_ENV@Q} ${ENABLE_SERVICE@Q}; rm -f ${REMOTE_SCRIPT@Q}"

echo "Deployment complete: ${SERVICE_NAME}.service on ${REMOTE_HOST} (${LISTEN_ADDR})"
