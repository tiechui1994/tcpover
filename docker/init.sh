#!/usr/bin/env bash

apt-get --quiet update && \
export DEBIAN_FRONTEND=noninteractive && \
export TZ=Asia/Shanghai && \
apt-get --quiet install --yes openssh-server net-tools iputils-ping iproute2 iptables openssl vim


passwd=$(echo abc123_ |openssl passwd -6 -stdin)
sed -i -E "/^root/ s|root:([^:]+?):(.*)|root:$passwd:\2|" /etc/shadow && cat > /etc/ssh/sshd_config <<-'EOF'
Port 22
AddressFamily any
ListenAddress 0.0.0.0

PermitRootLogin yes
PubkeyAuthentication yes
PasswordAuthentication yes
PermitEmptyPasswords no
ChallengeResponseAuthentication no

UsePAM yes

ClientAliveInterval 30
ClientAliveCountMax 10000

X11Forwarding yes
PrintMotd no

AcceptEnv LANG LC_*
Subsystem	sftp	/usr/lib/openssh/sftp-server
EOF

service ssh restart
