import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { InternalAxiosRequestConfig } from 'axios';
import { attachAuth, getCsrfToken, setCsrfToken, withApiToken } from './apiAuth';

// fakeConfig builds a minimal InternalAxiosRequestConfig whose headers.set is a spy,
// so we can assert exactly when/whether the CSRF header is written.
function fakeConfig(method?: string) {
  const set = vi.fn();
  const config = { method, headers: { set } } as unknown as InternalAxiosRequestConfig;
  return { config, set };
}

describe('apiAuth', () => {
  beforeEach(() => {
    // csrfToken is module-level mutable state — reset before each test for isolation.
    setCsrfToken('');
  });

  describe('setCsrfToken / getCsrfToken', () => {
    it('stores and returns the token', () => {
      setCsrfToken('abc123');
      expect(getCsrfToken()).toBe('abc123');
    });

    it('trims surrounding whitespace when storing', () => {
      setCsrfToken('   spaced-token   ');
      expect(getCsrfToken()).toBe('spaced-token');
    });

    it('treats null / undefined as an empty token', () => {
      setCsrfToken('seed');
      setCsrfToken(null);
      expect(getCsrfToken()).toBe('');
      setCsrfToken('seed');
      setCsrfToken(undefined);
      expect(getCsrfToken()).toBe('');
    });

    it('collapses a whitespace-only token to empty', () => {
      setCsrfToken('   ');
      expect(getCsrfToken()).toBe('');
    });
  });

  describe('attachAuth withCredentials', () => {
    it('always sets withCredentials, even for GET and even with no token', () => {
      const { config } = fakeConfig('get');
      const result = attachAuth(config);
      expect(result.withCredentials).toBe(true);
      // returns the same config object it was handed
      expect(result).toBe(config);
    });

    it('sets withCredentials even when method is undefined', () => {
      const { config, set } = fakeConfig(undefined);
      const result = attachAuth(config);
      expect(result.withCredentials).toBe(true);
      // undefined method defaults to "get" -> no CSRF header
      expect(set).not.toHaveBeenCalled();
    });
  });

  describe('attachAuth CSRF header', () => {
    it('adds X-CSRF-Token for mutating methods when a token is set', () => {
      setCsrfToken('tok-42');
      for (const method of ['post', 'put', 'patch', 'delete']) {
        const { config, set } = fakeConfig(method);
        attachAuth(config);
        expect(set).toHaveBeenCalledWith('X-CSRF-Token', 'tok-42');
        expect(set).toHaveBeenCalledTimes(1);
      }
    });

    it('is case-insensitive about the HTTP method', () => {
      setCsrfToken('tok-42');
      const { config, set } = fakeConfig('POST');
      attachAuth(config);
      expect(set).toHaveBeenCalledWith('X-CSRF-Token', 'tok-42');
    });

    it('does NOT add the header for safe methods (get / head)', () => {
      setCsrfToken('tok-42');
      for (const method of ['get', 'head']) {
        const { config, set } = fakeConfig(method);
        attachAuth(config);
        expect(set).not.toHaveBeenCalled();
      }
    });

    it('does NOT add the header for a mutating method when no token is set', () => {
      // beforeEach cleared the token
      const { config, set } = fakeConfig('post');
      attachAuth(config);
      expect(set).not.toHaveBeenCalled();
    });

    it('uses the trimmed token value in the header', () => {
      setCsrfToken('  trimmed-tok  ');
      const { config, set } = fakeConfig('delete');
      attachAuth(config);
      expect(set).toHaveBeenCalledWith('X-CSRF-Token', 'trimmed-tok');
    });
  });

  describe('withApiToken', () => {
    it('returns the URL unchanged (cookie-session no-op passthrough)', () => {
      expect(withApiToken('/api/books/1/file')).toBe('/api/books/1/file');
      expect(withApiToken('/api/x?y=1&z=2')).toBe('/api/x?y=1&z=2');
      expect(withApiToken('')).toBe('');
    });
  });
});
