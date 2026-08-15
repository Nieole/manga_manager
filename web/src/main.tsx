import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App.tsx'
import { ThemeProvider } from './theme/ThemeProvider.tsx'
import { initializeTheme } from './theme/themes.ts'
import { DEFAULT_LOCALE, type AppLocale } from './i18n/core.ts'
import { LocaleProvider, getClientLocale, loadLocaleMessages } from './i18n/LocaleProvider.tsx'
import { installApiAuth } from './utils/apiAuth.ts'
import { AuthProvider } from './auth/AuthProvider.tsx'
import { ToastProvider } from './components/ToastProvider.tsx'
import './index.css'

// Cache buster: 1
initializeTheme()
// 安装多用户会话鉴权拦截器（携带 Cookie + 为改写类请求附加 X-CSRF-Token）。
installApiAuth()

async function bootstrap() {
  const initialLocale = getClientLocale()
  const fallbackLocale = DEFAULT_LOCALE as AppLocale
  const [initialMessages, fallbackMessages] = await Promise.all(
    initialLocale === fallbackLocale
      ? [loadLocaleMessages(initialLocale), loadLocaleMessages(initialLocale)]
      : [loadLocaleMessages(initialLocale), loadLocaleMessages(fallbackLocale)],
  )

  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <LocaleProvider
        initialLocale={initialLocale}
        initialMessages={initialMessages}
        fallbackMessages={fallbackMessages}
      >
        <ThemeProvider>
          <ToastProvider>
            <BrowserRouter>
              <AuthProvider>
                <App />
              </AuthProvider>
            </BrowserRouter>
          </ToastProvider>
        </ThemeProvider>
      </LocaleProvider>
    </StrictMode>,
  )
}

void bootstrap()
