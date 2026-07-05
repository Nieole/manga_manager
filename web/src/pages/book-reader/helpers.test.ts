import { describe, expect, it } from 'vitest'
import { getPagedImages, getScaleClasses, getFilterStyle } from './helpers'
import type { ImageFilter, Page } from './types'

const page = (n: number): Page => ({ number: n, width: 800, height: 1200 })

describe('getPagedImages', () => {
  const pages = [page(1), page(2), page(3), page(4)]

  it('returns empty when there are no pages', () => {
    expect(getPagedImages([], 0, true, 'ltr')).toEqual([])
  })

  it('single-page mode returns only the current page', () => {
    expect(getPagedImages(pages, 1, false, 'ltr')).toEqual([page(2)])
  })

  it('double-page ltr returns [current, next] in left-to-right order', () => {
    expect(getPagedImages(pages, 0, true, 'ltr')).toEqual([page(1), page(2)])
  })

  it('double-page rtl swaps to [next, current] for right-to-left order', () => {
    expect(getPagedImages(pages, 0, true, 'rtl')).toEqual([page(2), page(1)])
  })

  it('double-page on the last page returns only the current page (no partner)', () => {
    expect(getPagedImages(pages, 3, true, 'ltr')).toEqual([page(4)])
  })

  it('double-page rtl on the last page still returns only the current page', () => {
    expect(getPagedImages(pages, 3, true, 'rtl')).toEqual([page(4)])
  })

  it('double-page rtl in the middle swaps the pair order', () => {
    expect(getPagedImages(pages, 2, true, 'rtl')).toEqual([page(4), page(3)])
  })

  it('single-page mode returns the last page unchanged', () => {
    expect(getPagedImages(pages, 3, false, 'rtl')).toEqual([page(4)])
  })
})

describe('getScaleClasses', () => {
  it('always carries the base classes and the block reset', () => {
    const out = getScaleClasses('fit-height', false, 'BASE')
    expect(out).toContain('BASE')
    expect(out).toContain('block m-0 p-0')
  })

  it('fit-width differs between single and double page', () => {
    expect(getScaleClasses('fit-width', false, '')).toContain('w-full')
    expect(getScaleClasses('fit-width', true, '')).toContain('w-[50vw]')
  })

  it('original mode disables scaling', () => {
    expect(getScaleClasses('original', false, '')).toContain('max-w-none max-h-none')
  })

  it('double-page height-fitting caps width to half the viewport', () => {
    expect(getScaleClasses('fit-height', true, '')).toContain('max-w-[50vw]')
    expect(getScaleClasses('fit-screen', true, '')).toContain('max-w-[50vw]')
  })

  it('single-page fit-screen fills the viewport without the half-width cap', () => {
    const out = getScaleClasses('fit-screen', false, '')
    expect(out).toContain('w-full h-full object-contain')
    expect(out).not.toContain('max-w-[50vw]')
  })

  it('single-page fit-height lets width run free (no half-width cap)', () => {
    const out = getScaleClasses('fit-height', false, '')
    expect(out).toContain('h-full w-auto object-contain max-w-none')
    expect(out).not.toContain('max-w-[50vw]')
  })

  it('original mode ignores double-page and never caps size', () => {
    const out = getScaleClasses('original', true, '')
    expect(out).toContain('w-auto h-auto max-w-none max-h-none')
    expect(out).not.toContain('max-w-[50vw]')
  })
})

describe('getFilterStyle', () => {
  it('nearest maps to pixelated rendering', () => {
    expect(getFilterStyle('nearest')).toEqual({ imageRendering: 'pixelated' })
  })

  it('average/bilinear map to auto rendering', () => {
    expect(getFilterStyle('average')).toEqual({ imageRendering: 'auto' })
    expect(getFilterStyle('bilinear')).toEqual({ imageRendering: 'auto' })
  })

  it('high-quality resamplers map to high-quality rendering', () => {
    const filters: ImageFilter[] = [
      'bicubic', 'lanczos3', 'waifu2x', 'realcugan', 'mitchell', 'lanczos2', 'bspline', 'catmullrom',
    ]
    for (const f of filters) {
      expect(getFilterStyle(f)).toEqual({ imageRendering: 'high-quality' })
    }
  })

  it('none/unknown yields an empty style object', () => {
    expect(getFilterStyle('none')).toEqual({})
    // 未知/越界的滤镜值走 default 分支，返回空样式。
    expect(getFilterStyle('mystery' as ImageFilter)).toEqual({})
  })
})
