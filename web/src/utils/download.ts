/**
 * 业务说明：本文件为“下载原始归档文件”提供前端触发助手。
 * 后端 GET /api/books/{id}/file 已带 Content-Disposition: attachment 与正确 MIME（cbz/cbr/zip/rar），
 * 这里只负责触发浏览器下载：会话 Cookie 同源自动携带，无需改写 URL。
 * 维护时应保持不覆盖服务器给出的文件名。
 */

// downloadBookFile 触发指定书籍原始归档的浏览器下载。
// 服务器响应已是 attachment，anchor 导航会直接下载且不离开当前页；
// 文件名（含中文卷名，来自服务器 Content-Disposition）由响应头决定，
// 因此不设置 download 属性以免用空值覆盖服务器文件名。
export function downloadBookFile(bookId: number): void {
  const anchor = document.createElement('a');
  anchor.href = `/api/books/${bookId}/file`;
  anchor.style.display = 'none';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}
