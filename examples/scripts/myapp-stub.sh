#!/bin/bash
# DynaStub 用户自定义脚本示例
# 这个脚本将替换原始的可执行文件

# 获取当前时间
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')

# 记录日志
LOG_FILE="/var/log/dynastub/myapp.log"
echo "[$TIMESTAMP] DynaStub: Intercepted call to myapp" >> "$LOG_FILE"
echo "[$TIMESTAMP] Arguments: $*" >> "$LOG_FILE"
echo "[$TIMESTAMP] Environment: $(env)" >> "$LOG_FILE"

# 这里可以添加自定义逻辑
# 例如：
# - 修改参数
# - 添加延迟
# - 返回模拟数据
# - 调用其他服务

# 注意：由于 subPath 挂载会覆盖原命令，且容器文件系统通常只读，
# 无法自动备份原命令。如需透传调用原命令，请在构建镜像时预先将
# 原命令备份为 .original 版本，例如：
#   RUN cp /app/myapp /app/myapp.original
#
# 然后在脚本中调用：
# if [ -f "/app/myapp.original" ]; then
#     echo "[$TIMESTAMP] Calling original binary..." >> "$LOG_FILE"
#     /app/myapp.original "$@"
#     exit $?
# fi

# 默认行为：输出拦截信息
echo "DynaStub: This call was intercepted by DynaStub"
echo "Original command: /app/myapp"
echo "Arguments: $*"

# 返回成功
exit 0
