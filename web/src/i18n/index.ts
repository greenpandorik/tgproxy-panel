import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

import en from './en.json';
import ru from './ru.json';

export const LANG_STORAGE_KEY = 'lang';
export type Lang = 'ru' | 'en';

function initialLang(): Lang {
  const stored = window.localStorage.getItem(LANG_STORAGE_KEY);
  return stored === 'en' ? 'en' : 'ru';
}

void i18n.use(initReactI18next).init({
  resources: {
    ru: { translation: ru },
    en: { translation: en },
  },
  lng: initialLang(),
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
  returnNull: false,
});

export function setLang(lang: Lang): void {
  window.localStorage.setItem(LANG_STORAGE_KEY, lang);
  void i18n.changeLanguage(lang);
  document.documentElement.lang = lang;
}

document.documentElement.lang = initialLang();

export default i18n;
