#!/usr/bin/env bash

apt-get --quiet update && \
export DEBIAN_FRONTEND=noninteractive && \
export TZ=Asia/Shanghai && \
apt-get --quiet install --yes openssh-server net-tools iputils-ping iproute2 iptables openssl vim curl

wget https://install.speedtest.net/app/cli/ookla-speedtest-1.2.0-linux-x86_64.tgz -O /tmp/ookla.tgz && \
tar xf /tmp/ookla.tgz -C /tmp && mv /tmp/speedtest /root && rm -rf /tmp/ookla.tgz && rm -rf /tmp/speedtest*

wget https://github.com/yt-dlp/yt-dlp/releases/download/2024.12.13/yt-dlp -O /tmp/yt-dlp && \
chmod a+x /tmp/yt-dlp && mv /tmp/yt-dlp /root

wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz -O /tmp/go.tgz && \
tar xf /tmp/go.tgz -C /tmp && mv /tmp/go /opt && ln -sf /opt/go/bin/go /usr/local/bin && rm -rf /tmp/go.tgz

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

