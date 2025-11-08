#!/bin/bash

# Take input for Tokyo IP
read -p "Enter Tokyo server IP address: " TOKYO_IP

# Download latest XMRig
LATEST=$(curl -s https://api.github.com/repos/xmrig/xmrig/releases/latest | grep browser_download_url | grep linux-static-x64.tar.gz | cut -d '"' -f 4)
wget "$LATEST" -O xmrig-latest.tar.gz
tar -xzf xmrig-latest.tar.gz
sudo mv xmrig-*/xmrig /usr/bin/

# Add tokyo to /etc/hosts
echo "$TOKYO_IP tokyo" | sudo tee -a /etc/hosts > /dev/null

# Enable hugepages
echo "vm.nr_hugepages=1280" | sudo tee /etc/sysctl.d/99-hugepages.conf > /dev/null
sudo sysctl -p /etc/sysctl.d/99-hugepages.conf

# Copy service file
cp grid.service /etc/systemd/system/grid.service
sudo systemctl daemon-reload
sudo systemctl enable grid
sudo systemctl start grid

# Verify it's running
sudo systemctl status grid
