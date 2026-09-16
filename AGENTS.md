# Soha Operator 仓库入口

- 本仓负责独立 Kubernetes Operator、CRD、controller 和 RBAC；不得 import `soha/internal/**`，也不得依赖控制平面才能在集群内协调。
- 在 OpenSoha 多仓工作区中读取 `../AGENTS.md` 一次；独立克隆时使用本仓规则，不要求初始化相邻仓库或规划工具。
- Operator 实现或审查按需使用 [soha-operator](.agents/skills/soha-operator/SKILL.md)，保留 reconcile 幂等性、权限和状态边界。
- Go 改动先验证受影响包；完整入口为 `make verify`，API/RBAC 变化通过 `make generate manifests` 更新生成物。命令、工具版本和镜像门禁以 [CI](.github/workflows/ci.yml) 为准。
- 文档和技能改动只检查内容、链接与差异；相关代码和环境未变化时复用成功验证，保留用户未提交改动。
