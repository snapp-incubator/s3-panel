import i18next, { type TOptions } from 'i18next'
import enTranslations from '@/locales/en.json'

export default i18next.init({
  resources: {
    en: { translation: enTranslations }
  },
  lng: 'en',
  fallbackLng: 'en',
  interpolation: {
    escapeValue: false
  }
})

export const t = (key: string, options?: TOptions) => {
  return i18next.t(key, options).toString()
}
