#!/bin/bash
set -euo pipefail

# --- Must run as root ---
if [ "${EUID}" -ne 0 ]; then
  echo "Run with sudo: sudo $0"
  exit 1
fi

working_dir=$(realpath .)

# --- Take input for Tokyo IP ---
read -p "Enter Tokyo server IP address: " TOKYO_IP

# --- Take input for webhook URL ---
read -p "Enter webhook URL: " WEBHOOK_URL

# --- Detect CPU model (7900X vs 7950X) and tune hugepages ---
CPU_MODEL="$(grep -m1 -E 'model name\s*:' /proc/cpuinfo | cut -d: -f2- | sed 's/^[[:space:]]*//')"

HUGEPAGES=1280
CPU_TUNE_LABEL="default"
if echo "$CPU_MODEL" | grep -qiE 'Ryzen 9 7950X'; then
  HUGEPAGES=1536
  CPU_TUNE_LABEL="Ryzen 9 7950X"
elif echo "$CPU_MODEL" | grep -qiE 'Ryzen 9 7900X'; then
  HUGEPAGES=1280
  CPU_TUNE_LABEL="Ryzen 9 7900X"
fi

echo "Detected CPU: $CPU_MODEL"
echo "Tuning profile: $CPU_TUNE_LABEL"
echo "Setting vm.nr_hugepages=$HUGEPAGES"

# --- Install build deps (includes libuv dev headers) ---
apt update
apt install -y \
  git build-essential cmake automake libtool autoconf pkg-config \
  libssl-dev \
  libuv1-dev \
  hwloc libhwloc-dev \
  ca-certificates \
  golang-go \
  dmidecode

# --- Get XMRig source (fresh) ---
install -d /opt
rm -rf /opt/xmrig
git clone --depth 1 https://github.com/xmrig/xmrig.git /opt/xmrig

# --- Build (dedicated: -march=native) ---
cd /opt/xmrig
rm -rf build
mkdir build
cd build

cmake .. \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_C_FLAGS="-O3 -march=native" \
  -DCMAKE_CXX_FLAGS="-O3 -march=native" \
  -DWITH_HWLOC=ON \
  -DWITH_TLS=ON \
  -DWITH_OPENCL=OFF \
  -DWITH_CUDA=OFF

make -j"$(nproc)"

# --- Install binary ---
install -m 0755 xmrig /usr/bin/xmrig

# --- Build and install grid ---
cd "$working_dir"
go build -o /usr/bin/grid ./cmd/main.go

# --- /etc/hosts: simple + robust (no regex capture groups) ---
# Remove any existing "tokyo" entry (end-of-line match)
sed -i '/[[:space:]]tokyo$/d' /etc/hosts
echo "${TOKYO_IP} tokyo" >> /etc/hosts

# --- Enable hugepages ---
echo "vm.nr_hugepages=${HUGEPAGES}" > /etc/sysctl.d/99-hugepages.conf
sysctl -p /etc/sysctl.d/99-hugepages.conf

sed "s|__WEBHOOK_URL__|${WEBHOOK_URL}|g" "$working_dir/grid.service" > /etc/systemd/system/grid.service
systemctl daemon-reload
systemctl enable grid
systemctl restart grid

# --- Verify ---
systemctl --no-pager status grid || true
echo
/usr/bin/xmrig --version
echo
echo "Hugepages now:"
grep -E 'HugePages_Total|HugePages_Free|Hugepagesize' /proc/meminfo || true
echo "Done."
