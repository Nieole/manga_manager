/** @vitest-environment jsdom */
import {beforeEach,afterEach,it,vi,expect} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup,within} from '@testing-library/react';
import {LocaleProvider} from '../../i18n/LocaleProvider';import {messages} from '../../i18n/locales/zh-CN';import {ToastProvider} from '../../components/ToastProvider';import {SettingsUsersPage} from './SettingsUsersPage';
const mocks=vi.hoisted(()=>({get:vi.fn(),post:vi.fn(),patch:vi.fn(),refresh:vi.fn(),remove:vi.fn()}));
vi.mock('../../api/client',()=>({apiClient:{get:mocks.get,post:mocks.post,patch:mocks.patch,delete:mocks.remove,defaults:{headers:{common:{}}}},getApiErrorMessage:()=> 'conflict'}));
vi.mock('../../auth/AuthProvider',()=>({useAuth:()=>({user:{id:99,role:'admin'},refresh:mocks.refresh})}));
const row={id:1,username:'original',display_name:'',role:'admin',must_change_password:false};
beforeEach(()=>{mocks.get.mockReset().mockResolvedValue({data:[row]});mocks.post.mockReset();mocks.patch.mockReset();mocks.remove.mockReset()});afterEach(cleanup);
function setup(){return render(<LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}><ToastProvider><SettingsUsersPage/></ToastProvider></LocaleProvider>)}
it('creates the exact username entered by the administrator',async()=>{mocks.post.mockResolvedValue({data:{...row,id:2,username:' New ',role:'regular'}});setup();await screen.findByText('@original');fireEvent.change(screen.getByPlaceholderText('用户名'),{target:{value:' New '}});fireEvent.change(screen.getByPlaceholderText('初始密码（至少 8 位）'),{target:{value:'password1'}});fireEvent.click(screen.getByText('创建用户'));await waitFor(()=>expect(mocks.post).toHaveBeenCalledWith('/api/users',expect.objectContaining({username:' New '})))});
it('uses the authoritative role result without treating a later list failure as a failed write',async()=>{mocks.get.mockResolvedValueOnce({data:[row]}).mockRejectedValue(new Error('reload failed'));mocks.patch.mockResolvedValue({data:{...row,role:'regular'}});setup();await screen.findByText('@original');fireEvent.change(screen.getByLabelText('角色'),{target:{value:'regular'}});await waitFor(()=>expect((screen.getByLabelText('角色')as HTMLSelectElement).value).toBe('regular'))});
it('keeps the original role when the command reports a conflict',async()=>{mocks.patch.mockRejectedValue(new Error('last admin'));setup();await screen.findByText('@original');fireEvent.change(screen.getByLabelText('角色'),{target:{value:'regular'}});await screen.findByText('conflict');expect((screen.getByLabelText('角色')as HTMLSelectElement).value).toBe('admin')});

it('removes a deleted account using the committed result without reloading the list', async () => {
  mocks.get.mockResolvedValueOnce({ data: [row] }).mockRejectedValue(new Error('reload failed'));
  mocks.remove.mockResolvedValue({ data: { status: 'ok' } });
  setup();
  await screen.findByText('@original');
  fireEvent.click(screen.getByTitle('删除'));
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '删除', exact: true }));
  await waitFor(() => expect(screen.queryByText('@original')).toBeNull());
  expect(mocks.remove).toHaveBeenCalledWith('/api/users/1');
});

it('keeps the authoritative temporary-password state after reset without reloading the user list', async () => {
  mocks.get.mockResolvedValueOnce({ data: [row] }).mockRejectedValue(new Error('reload failed'));
  mocks.post.mockResolvedValue({ data: { user: { ...row, must_change_password: true }, password_committed: true } });
  setup();
  await screen.findByText('@original');
  fireEvent.click(screen.getByTitle('重置密码'));
  fireEvent.change(screen.getByPlaceholderText('新的初始密码（至少 8 位）'), { target: { value: 'password2' } });
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '重置密码', exact: true }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.getByText(/· 待首次登录改密/)).toBeTruthy();
  expect(mocks.get).toHaveBeenCalledTimes(1);
});

it('reports a session-refresh failure separately after resetting the current account', async () => {
  mocks.get.mockResolvedValue({ data: [{ ...row, id: 99 }] });
  mocks.post.mockResolvedValue({ data: { user: { ...row, id: 99, must_change_password: true }, password_committed: true } });
  mocks.refresh.mockRejectedValueOnce(new Error('session refresh failed'));
  setup();
  await screen.findByText('@original');
  fireEvent.click(screen.getByTitle('重置密码'));
  fireEvent.change(screen.getByPlaceholderText('新的初始密码（至少 8 位）'), { target: { value: 'password2' } });
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '重置密码', exact: true }));
  await screen.findByText('账户已保存，但会话资料刷新失败。请刷新当前账户状态。');
  expect(screen.getByText(/· 待首次登录改密/)).toBeTruthy();
  expect(screen.queryByRole('dialog')).toBeNull();
});
