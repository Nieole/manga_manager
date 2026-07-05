/**
 * 业务说明：本文件封装“自定义封面”的前端调用——把某一页设为封面、或上传一张图片作为封面。
 * 它对应后端 POST /api/books/{id}/cover（页码）与 /api/books/{id}/cover/upload（multipart）。
 * 维护时应保持与后端字段一致，并把可读错误交回调用方提示。
 */

import { apiClient } from '../api/client';

// setBookCoverFromPage 把书内指定页(1-based)设为封面，返回新的相对封面路径。
export async function setBookCoverFromPage(bookId: number, page: number): Promise<string> {
  const res = await apiClient.post<{ cover_path: string }>(`/api/books/${bookId}/cover`, { page });
  return res.data?.cover_path ?? '';
}

// uploadBookCover 上传一张图片作为封面，返回新的相对封面路径。
export async function uploadBookCover(bookId: number, file: File): Promise<string> {
  const form = new FormData();
  form.append('file', file);
  const res = await apiClient.post<{ cover_path: string }>(`/api/books/${bookId}/cover/upload`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
  return res.data?.cover_path ?? '';
}
