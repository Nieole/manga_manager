/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useStickyState } from './useStickyState';
import type { ImageFilter, ReaderImageFormat, ReadDirection, ReadMode, ScaleMode, ReaderTheme } from './types';

export function useReaderPreferences() {
  const [readerTheme, setReaderTheme] = useStickyState<ReaderTheme>('base', 'manga_reader_theme');
  const [readMode, setReadMode] = useStickyState<ReadMode>('webtoon', 'manga_read_mode');
  const [readDirection, setReadDirection] = useStickyState<ReadDirection>('ltr', 'manga_read_direction');
  const [doublePage, setDoublePage] = useStickyState<boolean>(false, 'manga_double_page');
  const [scaleMode, setScaleMode] = useStickyState<ScaleMode>('fit-screen', 'manga_scale_mode');
  const [imageFilter, setImageFilter] = useStickyState<ImageFilter>('bilinear', 'manga_image_filter');
  const [autoCrop, setAutoCrop] = useStickyState<boolean>(false, 'manga_auto_crop');
  const [preloadCount, setPreloadCount] = useStickyState<number>(3, 'manga_preload_count');
  const [readerImageFormat, setReaderImageFormat] = useStickyState<ReaderImageFormat>('original', 'manga_reader_image_format');
  const [readerImageQuality, setReaderImageQuality] = useStickyState<number>(82, 'manga_reader_image_quality');
  const [eyeProtection, setEyeProtection] = useStickyState<boolean>(false, 'manga_eye_protection');
  const [w2xScale, setW2xScale] = useStickyState<number>(2, 'manga_waifu2x_scale');
  const [w2xNoise, setW2xNoise] = useStickyState<number>(0, 'manga_waifu2x_noise');
  const [w2xFormat, setW2xFormat] = useStickyState<string>('webp', 'manga_waifu2x_format');

  return {
    readerTheme,
    setReaderTheme,
    readMode,
    setReadMode,
    readDirection,
    setReadDirection,
    doublePage,
    setDoublePage,
    scaleMode,
    setScaleMode,
    imageFilter,
    setImageFilter,
    autoCrop,
    setAutoCrop,
    preloadCount,
    setPreloadCount,
    readerImageFormat,
    setReaderImageFormat,
    readerImageQuality,
    setReaderImageQuality,
    eyeProtection,
    setEyeProtection,
    w2xScale,
    setW2xScale,
    w2xNoise,
    setW2xNoise,
    w2xFormat,
    setW2xFormat,
  };
}
