import { useStickyState } from './useStickyState';
import type { ImageFilter, ReadDirection, ReadMode, ScaleMode } from './types';

export function useReaderPreferences() {
  const [readMode, setReadMode] = useStickyState<ReadMode>('webtoon', 'manga_read_mode');
  const [readDirection, setReadDirection] = useStickyState<ReadDirection>('ltr', 'manga_read_direction');
  const [doublePage, setDoublePage] = useStickyState<boolean>(false, 'manga_double_page');
  const [scaleMode, setScaleMode] = useStickyState<ScaleMode>('fit-screen', 'manga_scale_mode');
  const [imageFilter, setImageFilter] = useStickyState<ImageFilter>('bilinear', 'manga_image_filter');
  const [autoCrop, setAutoCrop] = useStickyState<boolean>(false, 'manga_auto_crop');
  const [preloadCount, setPreloadCount] = useStickyState<number>(3, 'manga_preload_count');
  const [eyeProtection, setEyeProtection] = useStickyState<boolean>(false, 'manga_eye_protection');
  const [w2xScale, setW2xScale] = useStickyState<number>(2, 'manga_waifu2x_scale');
  const [w2xNoise, setW2xNoise] = useStickyState<number>(0, 'manga_waifu2x_noise');
  const [w2xFormat, setW2xFormat] = useStickyState<string>('webp', 'manga_waifu2x_format');

  return {
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
