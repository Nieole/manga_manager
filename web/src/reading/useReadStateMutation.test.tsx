// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { apiClient } from '../api/client';
import { useReadStateMutation } from './useReadStateMutation';
vi.mock('../api/client',()=>({apiClient:{post:vi.fn()}}));

it('keeps the same operation and occurrence when a failed mark is retried',async()=>{
 const post=vi.mocked(apiClient.post).mockRejectedValueOnce(new Error('connection lost')).mockResolvedValue({});
 const {result}=renderHook(()=>useReadStateMutation());
 await act(async()=>{await expect(result.current.markBooks([7],false)).rejects.toThrow('connection lost')});
 await act(async()=>{await result.current.markBooks([7],false)});
 expect(post.mock.calls[0][1]).toEqual(post.mock.calls[1][1]);
 expect(post.mock.calls[1][1]).toMatchObject({book_ids:[7],is_read:false,operation_id:expect.any(String),occurred_at:expect.any(String)});
 await act(async()=>{await result.current.markBooks([7],false)});
 expect(post.mock.calls[2][1]).not.toEqual(post.mock.calls[1][1]);
});

it('treats an opposite read-state choice as a new operation',async()=>{
 const post=vi.mocked(apiClient.post).mockReset().mockRejectedValueOnce(new Error('uncertain')).mockResolvedValue({});
 const {result}=renderHook(()=>useReadStateMutation());
 await act(async()=>{await expect(result.current.markSeries([4],false)).rejects.toThrow('uncertain')});
 await act(async()=>{await result.current.markSeries([4],true)});
 await act(async()=>{await result.current.markSeries([4],false)});
 expect(post.mock.calls[2][1]).not.toEqual(post.mock.calls[0][1]);
 expect(post.mock.calls[2][1]).toMatchObject({series_ids:[4],is_read:false});
});
