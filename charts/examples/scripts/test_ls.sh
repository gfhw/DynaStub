#!/bin/sh
# 示例 ls 脚本，用于测试 DynaStub

# 找到原始 ls 命令
ORIGINAL=$(which ls)

# 输出日志
LOG_FILE="/tmp/dynastub-ls.log"
echo "[$(date '+%Y-%m-%d %H:%M:%S')] DynaStub: Intercepted ls command: $*" >> "$LOG_FILE"

# 检查参数
if [[ "$*" == *"-a"* ]]; then
    # 模拟 ls -a 输出
    echo "total 16"
    echo "drwxr-xr-x  2 root root 4096 Apr 12 00:00 ."
    echo "drwxr-xr-x 21 root root 4096 Apr 12 00:00 .."
    echo "-rw-r--r--  1 root root   23 Apr 12 00:00 .hidden file"
    echo "-rw-r--r--  1 root root  123 Apr 12 00:00 README.md"
    echo "drwxr-xr-x  3 root root 4096 Apr 12 00:00 bin"
    echo "drwxr-xr-x  2 root root 4096 Apr 12 00:00 etc"
    echo "drwxr-xr-x  2 root root 4096 Apr 12 00:00 lib"
elif [[ "$*" == *"-l"* ]]; then
    # 模拟 ls -l 输出
    echo "total 12"
    echo "drwxr-xr-x  3 root root 4096 Apr 12 00:00 bin"
    echo "drwxr-xr-x  2 root root 4096 Apr 12 00:00 etc"
    echo "drwxr-xr-x  2 root root 4096 Apr 12 00:00 lib"
    echo "-rw-r--r--  1 root root  123 Apr 12 00:00 README.md"
else
    # 模拟默认 ls 输出
    echo "bin"
    echo "etc"
    echo "lib"
    echo "README.md"
fi

# 可选：调用原始命令
# exec "$ORIGINAL" "$*"
