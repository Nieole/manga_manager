/**
 * 业务说明：本文件是前端 API 访问的共享工具层，提供统一的错误消息提取，取代此前散落在多个页面/hook
 * 中逐字复制的实现。让所有调用方对后端错误响应（{ error } 结构）有一致的解析与降级行为。
 * 维护时应保持与后端错误响应契约一致；后续可在此扩展统一的 axios 实例与响应拦截器。
 */

import axios from 'axios'

// getApiErrorMessage 从各类错误中提取用户可读消息：优先后端 { error } 字段，其次 axios/Error 的 message，
// 最后回退到调用方给定的 fallback。此前该逻辑在 9 个文件中各有一份完全等价的副本，现统一至此。
export function getApiErrorMessage(error: unknown, fallback: string): string {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback
  if (error instanceof Error) return error.message
  return fallback
}
