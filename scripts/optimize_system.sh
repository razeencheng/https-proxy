#!/bin/bash
# HTTPS Proxy 系统优化脚本
# 此脚本用于优化Linux系统配置，以提高HTTPS代理服务器的性能

# 检查是否以root用户运行
if [ "$EUID" -ne 0 ]; then
  echo "请以root权限运行此脚本"
  exit 1
fi

echo "开始系统优化..."

# 备份当前的系统设置
BACKUP_FILE="/etc/sysctl.conf.$(date +%Y%m%d%H%M%S).bak"
cp /etc/sysctl.conf "$BACKUP_FILE"
echo "原始sysctl.conf已备份到 $BACKUP_FILE"

# 设置TCP连接参数
cat >> /etc/sysctl.conf << EOF

# HTTPS代理性能优化参数
# 增加TCP的最大并发连接数
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535

# 增加文件描述符限制
fs.file-max = 2097152

# 增加本地端口范围
net.ipv4.ip_local_port_range = 10000 65535

# 增加UDP和TCP缓冲区大小
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.core.rmem_default = 262144
net.core.wmem_default = 262144
net.core.netdev_max_backlog = 65536

# 禁用TCP慢启动，提高初始吞吐量
net.ipv4.tcp_slow_start_after_idle = 0

# 增加TCP缓冲区大小
net.ipv4.tcp_rmem = 4096 262144 16777216
net.ipv4.tcp_wmem = 4096 262144 16777216

# 启用TCP窗口缩放
net.ipv4.tcp_window_scaling = 1

# 启用TIME-WAIT复用
net.ipv4.tcp_tw_reuse = 1

# TCP保活设置
net.ipv4.tcp_keepalive_time = 600
net.ipv4.tcp_keepalive_intvl = 60
net.ipv4.tcp_keepalive_probes = 10

# BBR拥塞控制算法
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr

# 增加连接跟踪表的大小
net.netfilter.nf_conntrack_max = 1048576
EOF

# 应用系统设置
echo "应用优化参数..."
sysctl -p

# 设置用户限制
cat > /etc/security/limits.d/99-proxy-limits.conf << EOF
# 提高进程打开文件数限制
*                soft    nofile          1048576
*                hard    nofile          1048576
# 提高进程可使用的最大线程数
*                soft    nproc           65535
*                hard    nproc           65535
EOF

echo "已设置进程文件描述符限制"

# 检查BBR是否可用
if grep -q "bbr" /proc/sys/net/ipv4/tcp_congestion_control; then
    echo "BBR拥塞控制算法已启用"
else
    echo "警告: BBR拥塞控制算法未启用，这可能需要内核升级"
fi

# 提示重启
echo "请重新登录终端或重启服务器以应用所有更改"
echo "系统优化完成!" 