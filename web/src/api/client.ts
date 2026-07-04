/**
 * 业务说明：本文件是前端 API 访问的共享客户端层，导出统一的 axios 实例（apiClient）与错误消息提取。
 * 所有页面/hook 应通过 apiClient 发请求，而非直接 import axios，以便集中挂载鉴权拦截器、
 * 未来扩展响应错误归一化等横切逻辑（并可用 ESLint no-restricted-imports 约束）。
 * 维护时应保持与后端错误响应契约一致，以及“无令牌即无操作”的鉴权语义。
 */

import axios from 'axios'
import { getApiToken } from '../utils/apiAuth'

// apiClient 是全站统一的 axios 实例。不设 baseURL：沿用调用方现有的 /api/... 绝对路径，行为与
// 直接使用全局 axios 完全一致，仅把横切逻辑收拢到此实例上。
export const apiClient = axios.create()

// 与 installApiAuth 对全局 axios 的处理一致：存在管理令牌时为请求附加 X-API-Token 头（无令牌为无操作）。
// apiClient 是独立实例，不会继承全局 axios 的拦截器，因此需在此单独挂载。
apiClient.interceptors.request.use((config) => {
  const token = getApiToken()
  if (token && config.headers) {
    config.headers.set('X-API-Token', token)
  }
  return config
})

// 从 client 统一再导出 axios 的静态类型守卫，供各处判断错误/取消，避免消费方直接 import axios
// （可用 ESLint no-restricted-imports 收口，只允许 client.ts / apiAuth.ts 直接依赖 axios）。
export const isAxiosError = axios.isAxiosError
export const isCancel = axios.isCancel

// getApiErrorMessage 从各类错误中提取用户可读消息：优先后端 { error } 字段，其次 axios/Error 的 message，
// 最后回退到调用方给定的 fallback。此前该逻辑在 9 个文件中各有一份完全等价的副本，现统一至此。
export function getApiErrorMessage(error: unknown, fallback: string): string {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback
  if (error instanceof Error) return error.message
  return fallback
}
