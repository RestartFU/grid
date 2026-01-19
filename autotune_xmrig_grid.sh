#!/bin/bash
set -euo pipefail

# ========= REQUIREMENTS =========
# - /usr/bin/xmrig exists (compiled/installed already)
# - systemd service name is "grid"
# - dedicated machine (no interactive desktop needs)
#
# This script will:
# 1) Detect 7900X vs 7950X
# 2) Tune hugepages
# 3) Attempt MSR enablement (safe, reversible)
# 4) Benchmark candidate arg sets
# 5) Install a systemd override with best args

if [ "${EUID}" -ne 0 ]; then
  echo "Run as root: sudo $0"
  exit 1
fi

if ! command -v xmrig >/dev/null 2>&1; then
  echo "ERROR: xmrig not found in PATH. Install to /usr/bin/xmrig first."
  exit 1
fi

SERVICE_NAME="grid"
POOL_URL_DEFAULT="tokyo:3333"

CPU_MODEL="$(grep -m1 -E 'model name\s*:' /proc/cpuinfo | cut -d: -f2- | sed 's/^[[:space:]]*//')"

HUGEPAGES=1280
PROFILE="default"
if echo "$CPU_MODEL" | grep -qiE 'Ryzen 9 7950X'; then
  HUGEPAGES=1536
  PROFILE="Ryzen 9 7950X"
elif echo "$CPU_MODEL" | grep -qiE 'Ryzen 9 7900X'; then
  HUGEPAGES=1280
  PROFILE="Ryzen 9 7900X"
fi

echo "Detected CPU: $CPU_MODEL"
echo "Profile: $PROFILE"
echo "Target vm.nr_hugepages=$HUGEPAGES"
echo

# --- Apply hugepages persistently + live ---
echo "vm.nr_hugepages=${HUGEPAGES}" > /etc/sysctl.d/99-hugepages.conf
sysctl -q -p /etc/sysctl.d/99-hugepages.conf || true

# --- Try to enable MSR access (safe). XMRig benefits on Ryzen when MSR is available. ---
# This doesn't “overclock”; it just allows XMRig to apply Ryzen-friendly register tweaks when supported.
modprobe msr 2>/dev/null || true

# Best-effort: allow root to access MSR devices if they exist
if ls /dev/cpu/*/msr >/dev/null 2>&1; then
  chmod 600 /dev/cpu/*/msr 2>/dev/null || true
fi

# --- Detect whether 1GB pages are even possible ---
# If /sys/kernel/mm/hugepages/hugepages-1048576kB exists, kernel supports 1G hugepages.
ONEG_SUPPORTED="no"
if [ -d /sys/kernel/mm/hugepages/hugepages-1048576kB ]; then
  ONEG_SUPPORTED="yes"
fi

echo "1GB hugepages supported by kernel: $ONEG_SUPPORTED"
echo

# ========= BENCHMARK HARNESS =========
# We benchmark using `xmrig --bench=10M` and parse the "speed 60s" or fallback to "speed 10s".
# Note: Bench is CPU-only; it's a stable way to compare configs quickly.

bench_score() {
  local args="$1"
  local out
  out="$(xmrig --bench=10M --algo=rx/0 $args 2>/dev/null || true)"

  # Prefer 60s rate if present
  local s60
  s60="$(echo "$out" | grep -Eo 'speed 60s[^0-9]*[0-9]+(\.[0-9]+)?' | grep -Eo '[0-9]+(\.[0-9]+)?' | head -n1 || true)"
  if [ -n "$s60" ]; then
    echo "$s60"
    return 0
  fi

  # Fallback to 10s
  local s10
  s10="$(echo "$out" | grep -Eo 'speed 10s[^0-9]*[0-9]+(\.[0-9]+)?' | grep -Eo '[0-9]+(\.[0-9]+)?' | head -n1 || true)"
  if [ -n "$s10" ]; then
    echo "$s10"
    return 0
  fi

  # If parsing failed, return 0
  echo "0"
}

# ========= CANDIDATE CONFIGS =========
# Keep these “high-impact, low-risk”.
# - --huge-pages: uses 2MB hugepages (what your sysctl sets up)
# - --randomx-1gb-pages: only if 1G pages exist; otherwise it can hurt or fail
# - MSR: xmrig auto-uses MSR when available; no explicit flag needed in many builds
# - We also test "baseline" (xmrig defaults) because sometimes defaults win.

declare -a CAND_NAMES=()
declare -a CAND_ARGS=()

CAND_NAMES+=("baseline")
CAND_ARGS+=("")

CAND_NAMES+=("hugepages")
CAND_ARGS+=("--huge-pages")

# Add 1GB pages candidate only if kernel supports it
if [ "$ONEG_SUPPORTED" = "yes" ]; then
  CAND_NAMES+=("hugepages+1gbpages")
  CAND_ARGS+=("--huge-pages --randomx-1gb-pages")
fi

# Slightly higher priority can help on dedicated box (you already use cpu-priority=5)
CAND_NAMES+=("hugepages+prio5")
CAND_ARGS+=("--huge-pages --cpu-priority=5")

if [ "$ONEG_SUPPORTED" = "yes" ]; then
  CAND_NAMES+=("hugepages+1gb+prio5")
  CAND_ARGS+=("--huge-pages --randomx-1gb-pages --cpu-priority=5")
fi

# ========= RUN THE TUNER =========
best_name=""
best_args=""
best_score="0"

echo "Running benchmarks (this will take a few minutes)..."
echo

for i in "${!CAND_NAMES[@]}"; do
  name="${CAND_NAMES[$i]}"
  args="${CAND_ARGS[$i]}"

  score="$(bench_score "$args")"
  printf "%-22s  %8s H/s   args: %s\n" "$name" "$score" "${args:-<none>}"

  # Compare as floats using awk
  better="$(awk -v a="$score" -v b="$best_score" 'BEGIN {print (a+0 > b+0) ? 1 : 0}')"
  if [ "$better" = "1" ]; then
    best_score="$score"
    best_name="$name"
    best_args="$args"
  fi
done

echo
echo "Best config: $best_name  (${best_score} H/s)"
echo "Best args:   ${best_args:-<none>}"
echo

# ========= INSTALL SYSTEMD OVERRIDE =========
# We do this cleanly without rewriting your base unit:
# - Create /etc/systemd/system/grid.service.d/override.conf
# - Replace ExecStart so it always uses the chosen args + your pool/user/pass defaults
#
# We’ll keep your pool at tokyo:3333 and user/pass = %H (hostname),
# but you can edit these in the override after.

OVR_DIR="/etc/systemd/system/${SERVICE_NAME}.service.d"
OVR_FILE="${OVR_DIR}/override.conf"

mkdir -p "$OVR_DIR"

cat > "$OVR_FILE" <<EOF
[Service]
# Override ExecStart from the main unit
ExecStart=
ExecStart=/usr/bin/xmrig --url=${POOL_URL_DEFAULT} --user=%H --pass=%H --algo=rx/monero --verbose=0 ${best_args}
Restart=on-failure
RestartSec=10
LimitNOFILE=1048576
Nice=-5
EOF

systemctl daemon-reload
systemctl restart "$SERVICE_NAME" || true

echo "Installed override: $OVR_FILE"
echo "Service restarted: $SERVICE_NAME"
echo
echo "Check status/logs:"
echo "  sudo systemctl --no-pager status $SERVICE_NAME"
echo "  sudo journalctl -u $SERVICE_NAME -n 200 --no-pager"
echo
echo "Hugepages now:"
grep -E 'HugePages_Total|HugePages_Free|Hugepagesize' /proc/meminfo || true

