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

# 默认行为：输出拦截信息
echo "DynaStub: This call was intercepted by DynaStub"
echo "Original command: /app/myapp"
echo "Arguments: $*"

# 返回成功
exit 0
