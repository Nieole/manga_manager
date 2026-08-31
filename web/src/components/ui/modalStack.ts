/**
 * 全站弹窗的层叠登记表：ModalShell 打开时入栈、关闭时出栈，弹窗之间的三处争抢都按栈判定——
 * 键盘只有栈顶那层响应，页面滚动锁在栈空时才解，全局快捷键在栈非空时整体让位。
 */

// 每个打开中的弹窗一枚身份。用 symbol 而非计数器：不同实例天然不等，出栈只认自己那枚。
export type ModalId = symbol;

const stack: ModalId[] = [];

// 锁滚动前页面自己的 overflow，只在栈从空变满时记一次。
// 每层各记一份是错的：React 重渲染时先跑完所有 cleanup 再跑所有 setup，
// 后装的那层会把已经是 hidden 的值当成「原值」记下来，此后再也还原不回去。
let overflowBeforeLock = '';

export function pushModal(id: ModalId): void {
  if (stack.length === 0) {
    overflowBeforeLock = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
  }
  stack.push(id);
}

export function popModal(id: ModalId): void {
  const index = stack.lastIndexOf(id);
  if (index === -1) return;
  stack.splice(index, 1);
  if (stack.length === 0) {
    document.body.style.overflow = overflowBeforeLock;
  }
}

export function isTopModal(id: ModalId): boolean {
  return stack.length > 0 && stack[stack.length - 1] === id;
}

export function isAnyModalOpen(): boolean {
  return stack.length > 0;
}
