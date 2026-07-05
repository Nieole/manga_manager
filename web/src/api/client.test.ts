import { describe, expect, it } from 'vitest';
import { getApiErrorMessage, isAxiosError, isCancel } from './client';

// axios.isAxiosError only checks `payload.isAxiosError === true`, so a plain object with
// that brand is a faithful stand-in for a real AxiosError without hitting the network.
function axiosErr(shape: Record<string, unknown>) {
  return { isAxiosError: true, ...shape };
}

describe('getApiErrorMessage', () => {
  it('prefers the backend { error } field on an axios error', () => {
    const err = axiosErr({ response: { data: { error: 'Book not found' } }, message: 'Request failed' });
    expect(getApiErrorMessage(err, 'fallback')).toBe('Book not found');
  });

  it('falls back to the axios message when the error field is an empty string', () => {
    const err = axiosErr({ response: { data: { error: '' } }, message: 'Request failed' });
    expect(getApiErrorMessage(err, 'fallback')).toBe('Request failed');
  });

  it('uses the axios message when there is no response payload (network error)', () => {
    const err = axiosErr({ message: 'Network Error' });
    expect(getApiErrorMessage(err, 'fallback')).toBe('Network Error');
  });

  it('falls back when an axios error has neither an error field nor a message', () => {
    const err = axiosErr({ response: { data: {} }, message: '' });
    expect(getApiErrorMessage(err, 'fallback')).toBe('fallback');
  });

  it('returns the message of a plain Error instance', () => {
    expect(getApiErrorMessage(new Error('boom'), 'fallback')).toBe('boom');
  });

  it('falls back for a thrown string', () => {
    expect(getApiErrorMessage('some string', 'fallback')).toBe('fallback');
  });

  it('falls back for a bare object, null, and undefined', () => {
    expect(getApiErrorMessage({ foo: 1 }, 'fallback')).toBe('fallback');
    expect(getApiErrorMessage(null, 'fallback')).toBe('fallback');
    expect(getApiErrorMessage(undefined, 'fallback')).toBe('fallback');
  });
});

describe('re-exported axios guards', () => {
  it('exposes isAxiosError / isCancel as functions', () => {
    expect(typeof isAxiosError).toBe('function');
    expect(typeof isCancel).toBe('function');
    // sanity: our branded stand-in is recognised, a plain object is not
    expect(isAxiosError({ isAxiosError: true })).toBe(true);
    expect(isAxiosError({})).toBe(false);
  });
});
