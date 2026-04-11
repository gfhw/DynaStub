#!/bin/bash

# DynaStub 测试脚本
# 用于验证 webhook 和 behavior injection 功能

set -e

NAMESPACE="default"
CR_NAME="test-behavior-stub"
POD_NAME="test-app-pod"

echo "=========================================="
echo "DynaStub 测试脚本"
echo "=========================================="
echo ""

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查函数
check_resource() {
    local resource=$1
    local name=$2
    local namespace=$3
    
    if kubectl get $resource $name -n $namespace > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC} $resource/$name 存在"
        return 0
    else
        echo -e "${RED}✗${NC} $resource/$name 不存在"
        return 1
    fi
}

# 步骤 1: 检查 operator 是否运行
echo "步骤 1: 检查 operator 状态"
echo "----------------------------------------"
if kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=k8s-http-fake-operator | grep -q Running; then
    echo -e "${GREEN}✓${NC} Operator 正在运行"
else
    echo -e "${RED}✗${NC} Operator 未运行，请先部署 operator"
    exit 1
fi
echo ""

# 步骤 2: 创建测试 CR
echo "步骤 2: 创建测试 BehaviorStub CR"
echo "----------------------------------------"
kubectl apply -f test/test-cr.yaml
echo -e "${GREEN}✓${NC} 测试 CR 已创建"
echo ""

# 步骤 3: 等待 CR 就绪
echo "步骤 3: 等待 BehaviorStub 就绪"
echo "----------------------------------------"
echo "等待 5 秒..."
sleep 5

# 检查 webhook 是否被创建
echo "检查 MutatingWebhookConfiguration..."
if kubectl get mutatingwebhookconfiguration dynastub-k8s-http-fake-operator-webhook > /dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} MutatingWebhookConfiguration 已创建"
else
    echo -e "${RED}✗${NC} MutatingWebhookConfiguration 未创建"
    exit 1
fi
echo ""

# 步骤 4: 检查 CR 状态
echo "步骤 4: 检查 BehaviorStub 状态"
echo "----------------------------------------"
kubectl get behaviorstub $CR_NAME -n $NAMESPACE -o yaml | grep -A 5 "status:"
echo ""

# 步骤 5: 检查 Pod 是否被注入
echo "步骤 5: 检查 Pod 注入状态"
echo "----------------------------------------"
if kubectl get pod $POD_NAME -n $NAMESPACE > /dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} Pod 存在"
    
    # 检查是否有 sidecar
    if kubectl get pod $POD_NAME -n $NAMESPACE -o jsonpath='{.spec.containers[*].name}' | grep -q "dynastub-sidecar"; then
        echo -e "${GREEN}✓${NC} Sidecar 容器已注入"
    else
        echo -e "${YELLOW}!${NC} Sidecar 容器未注入（可能需要等待 Pod 重新创建）"
    fi
    
    # 显示 Pod 详情
    echo ""
    echo "Pod 容器列表:"
    kubectl get pod $POD_NAME -n $NAMESPACE -o jsonpath='{range .spec.containers[*]}{"  - "}{.name}{"\n"}{end}'
else
    echo -e "${RED}✗${NC} Pod 不存在"
fi
echo ""

# 步骤 6: 清理测试资源
echo "步骤 6: 清理测试资源"
echo "----------------------------------------"
read -p "是否清理测试资源? (y/n): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    kubectl delete -f test/test-cr.yaml --ignore-not-found=true
    echo -e "${GREEN}✓${NC} 测试资源已清理"
    
    # 检查 webhook 是否被删除
    echo ""
    echo "检查 MutatingWebhookConfiguration 是否被删除..."
    sleep 3
    if kubectl get mutatingwebhookconfiguration dynastub-k8s-http-fake-operator-webhook > /dev/null 2>&1; then
        echo -e "${YELLOW}!${NC} MutatingWebhookConfiguration 仍存在（可能有其他 CR）"
    else
        echo -e "${GREEN}✓${NC} MutatingWebhookConfiguration 已删除"
    fi
else
    echo "跳过清理"
fi
echo ""

echo "=========================================="
echo "测试完成"
echo "=========================================="
