---
title: 变更日志
type: docs
description:
author: HUATUO Team
date: 2026-03-29
weight: 50
---

## Unreleased

### 升级须知

- Java 采样要求使用随包提供的 async-profiler 4.5 窗口完成协议版本。已加载旧
  agent 的 JVM 需要重启；仅替换磁盘文件不能更新已加载的库。构建随包 agent 需要 Docker。
- Java 窗口文件和完成标记在确认入队后仍保留于目标 JVM 的 `/tmp`，用于进程崩溃后的恢复。
  请在确认导出或恢复会话后清理；入队确认不等于持久化确认。

### 修复

- Java 总超时和主动停止统一收尾，每个窗口仅交付一次。队列满或已停止时保留未接收窗口，
  显式报告交付不完整，避免静默丢弃。分窗口采样已有的切换间隙不变。
