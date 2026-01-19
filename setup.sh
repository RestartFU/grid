#!/bin/bash
set -euo pipefail

# --- Must run as root ---
if [ "${EUID}" -ne 0 ]; then
  echo "Run with sudo: sudo $0"
  exit 1
fi

# --- Override shutdown with reboot (your original behavior) ---
mv /usr/bin/shutdown /usr/bin/shutdown-force 2>/dev/null || true
cp /usr/bin/reboot /usr/bin/shutdown

# --- Take input for Tokyo IP ---
read -p "Enter Tokyo server IP address: " TOKYO_IP

# --- Detect CPU model (7900X vs 7950X) and tune hugepages ---
CPU_MODEL="$(grep -m1 -E 'model name\s*:' /proc/cpuinfo | cut -d: -f2- | sed 's/^[[:space:]]*//')"

# Defaults if detection fails
HUGEPAGES=1280
CPU_TUNE_LABEL="default"

if echo "$CPU_MODEL" | grep -qiE 'Ryzen 9 7950X'; then
  # 16C/32T: slightly higher hugepages helps keep 1G/2M pools happy
  HUGEPAGES=1536
  CPU_TUNE_LABEL="Ryzen 9 7950X"
elif echo "$CPU_MODEL" | grep -qiE 'Ryzen 9 7900X'; then
  # 12C/24T
  HUGEPAGES=1280
  CPU_TUNE_LABEL="Ryzen 9 7900X"
fi

echo "Detected CPU: $CPU_MODEL"
echo "Tuning profile: $CPU_TUNE_LABEL"
echo "Setting vm.nr_hugepages=$HUGEPAGES"

# --- Install build deps ---
apt update
apt install -y \
  git build-essential cmake automake libtool autoconf pkg-config \
  libssl-dev \
  hwloc libhwloc-dev \
  ca-certificates

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

# --- /etc/hosts: tokyo entry (replace if exists) ---
if grep -qE '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+\s+tokyo(\s|$)' /etc/hosts; then
  sed -i -E "s|^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+\s+tokyo(\s|$)|${TOKYO_IP} tokyo\1|g" /etc/hosts
else
  echo "${TOKYO_IP} tokyo" >> /etc/hosts
fi

# --- Hugepages ---
echo "vm.nr_hugepages=${HUGEPAGES}" > /etc/sysctl.d/99-hugepages.conf
sysctl -p /etc/sysctl.d/99-hugepages.conf

# --- Install systemd service (expects grid.service in current dir when you run script) ---
if [ ! -f ./grid.service ]; then
  echo "ERROR: grid.service not found in current directory: $(pwd)"
  echo "Run the script from the folder containing grid.service, or change the path."
  exit 1
fi

cp ./grid.service /etc/systemd/system/grid.service
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

