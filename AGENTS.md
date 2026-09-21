# Soha Operator 仓库入口

- 本仓负责独立 Kubernetes Operator、CRD、controller 和 RBAC；不得 import `soha/internal/**`，也不得依赖控制平面才能在集群内协调。
- 在 OpenSoha 多仓工作区中读取 `../AGENTS.md` 一次；独立克隆时使用本仓规则，不要求初始化相邻仓库或规划工具。
- Operator 实现或实质审查前读取 [soha-operator](.agents/skills/soha-operator/SKILL.md) 及本次相关参考，不只依赖技能自动匹配。已读且未变化的内容可复用；任务切换或关键上下文丢失时补读必要部分。
- Go 改动先验证受影响包；完整入口为 `make verify`，API/RBAC 变化通过 `make generate manifests` 更新生成物。命令、工具版本和镜像门禁以 [CI](.github/workflows/ci.yml) 为准。
- 文档和技能改动只检查内容、链接与差异；相关代码和环境未变化时复用成功验证，保留用户未提交改动。

## 变更与验收边界

- 修改前明确 CRD/controller 所有权、受影响 source/target、状态转换及验证入口；保留同命名空间引用、幂等协调、删除/失效时安全暂停和不接管外部资源的边界。
- 旧 controller 或清单只证明当前行为，不自动成为新功能样板。规范、API 和实现冲突时说明依据，不通过扩宽 RBAC、忽略失败或重置基线消除问题。
- 普通修复不顺手增加通用操作 CRD、数据库、轮询、webhook 或 finalizer；确有生命周期需求时先界定作用范围和失败恢复验证。
- 共享协调能力变更需要检查调用者及重复事件、缺失/删除 source、外部所有者等相关失败路径；只改一个文件也可能需要扩大回归。
- 报告实际提交、命令、Kubernetes/测试环境及 pass/fail/skip/not-run。生成、编译、fake-client 测试、API-server 集成和真实集群验收分开记录；未执行场景不算通过。
- 跨仓只更新真正受影响的公开契约或 Helm 消费者；规则和历史任务记录不自动授权修改、发布或部署其他仓库。
