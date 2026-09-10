import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import enUS from './locales/en-US.json';
import zhCN from './locales/zh-CN.json';

function buildResources(locale: Record<string, any>) {
  const ns: Record<string, any> = {};
  for (const [key, value] of Object.entries(locale)) {
    if (typeof value === 'object' && value !== null) {
      ns[key] = value;
    }
  }
  ns.translation = locale;
  return ns;
}

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      'en-US': buildResources(enUS),
      'zh-CN': buildResources(zhCN),
    },
    fallbackLng: 'zh-CN',
    debug: false,
    interpolation: {
      escapeValue: false,
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
    },
  });

export default i18n;
